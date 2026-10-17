//go:build postgres

package growth

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/growth/connectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phoneFake accepts code 22222, asks for a password after code 11111, and accepts password "pw".
type phoneFake struct {
	account   connectors.Account
	phone     string
	cancelled bool
}

func (f *phoneFake) Code(ctx context.Context, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch code {
	case "11111":
		return connectors.ErrPasswordNeeded
	case "22222":
		return nil
	}
	return connectors.ErrBadCode
}

func (f *phoneFake) Password(ctx context.Context, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if password != "pw" {
		return connectors.ErrBadCode
	}
	return nil
}

func (f *phoneFake) Cancel() { f.cancelled = true }

func registerPhoneFake(t *testing.T) *[]*phoneFake {
	t.Helper()
	if real, ok := connectors.LookupPhoneLogin(connectors.PlatformTelegram); ok {
		t.Cleanup(func() { connectors.RegisterPhoneLogin(connectors.PlatformTelegram, real) })
	}
	var started []*phoneFake
	connectors.RegisterPhoneLogin(connectors.PlatformTelegram,
		func(ctx context.Context, _ *config.Config, account connectors.Account, phone string) (connectors.PhoneLoginSession, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			f := &phoneFake{account: account, phone: phone}
			started = append(started, f)
			return f, nil
		})
	return &started
}

// remoteFake is signed in from the second Finish on.
type remoteFake struct {
	finishes  int
	cancelled []int64
	started   []connectors.Account
}

func (r *remoteFake) Start(ctx context.Context, _ *config.Config, a connectors.Account) (connectors.RemoteLoginView, error) {
	r.started = append(r.started, a)
	return connectors.RemoteLoginView{
		ViewURL:     "https://browser.example/login/1?ticket=t",
		ExpiresAt:   time.Date(2026, 10, 16, 23, 0, 0, 0, time.UTC),
		Fingerprint: json.RawMessage(`{"os":"windows","timezone":"Asia/Bangkok","language":"th-TH"}`),
	}, ctx.Err()
}

func (r *remoteFake) Finish(ctx context.Context, _ *config.Config, _ connectors.Account) (bool, string, error) {
	r.finishes++
	if r.finishes < 2 {
		return false, "checkpoint page", ctx.Err()
	}
	return true, "", ctx.Err()
}

func (r *remoteFake) Cancel(ctx context.Context, _ *config.Config, a connectors.Account) error {
	r.cancelled = append(r.cancelled, a.ID)
	return ctx.Err()
}

func TestLogin_Telegram_WalksCodeAndPasswordToAnActiveAccount(t *testing.T) {
	started := registerPhoneFake(t)
	w := newWorld(t)
	ctx := context.Background()
	proxy := "socks5://u:p@10.0.0.1:1080"

	_, err := w.svc.StartTelegramLogin(ctx, w.user, LoginStart{Phone: "66 81"})
	assert.ErrorIs(t, err, ErrInvalidSetting)

	step, err := w.svc.StartTelegramLogin(ctx, w.user, LoginStart{Phone: "+66 81-234-5678", Proxy: &proxy})
	require.NoError(t, err)
	assert.Equal(t, LoginNextCode, step.Next)
	assert.Nil(t, step.Account)
	require.Len(t, *started, 1)
	fake := (*started)[0]
	assert.Equal(t, "+66812345678", fake.phone)
	assert.Equal(t, proxy, fake.account.Proxy, "the code goes out through the account's proxy")

	acc, err := w.svc.userAccount(ctx, w.user, fake.account.ID)
	require.NoError(t, err)
	assert.Equal(t, "+66812345678", acc.Label)
	assert.Equal(t, AccountStatusNeedsLogin, acc.Status)

	other := seedUser(t, w.db, "other@example.test")
	_, err = w.svc.SubmitTelegramCode(ctx, other, step.LoginID, "22222")
	assert.ErrorIs(t, err, ErrLoginNotFound, "another customer cannot use the login id")

	_, err = w.svc.SubmitTelegramCode(ctx, w.user, step.LoginID, "00000")
	assert.ErrorIs(t, err, connectors.ErrBadCode)
	step, err = w.svc.SubmitTelegramCode(ctx, w.user, step.LoginID, " 11111 ")
	require.NoError(t, err, "a wrong code leaves the sign-in open")
	assert.Equal(t, LoginNextPassword, step.Next)

	step, err = w.svc.SubmitTelegramPassword(ctx, w.user, step.LoginID, "pw")
	require.NoError(t, err)
	assert.Equal(t, LoginNextDone, step.Next)
	require.NotNil(t, step.Account)
	assert.Equal(t, AccountStatusActive, step.Account.Status)
	assert.True(t, fake.cancelled, "the finished session is released")

	_, err = w.svc.SubmitTelegramPassword(ctx, w.user, step.LoginID, "pw")
	assert.ErrorIs(t, err, ErrLoginNotFound)
}

func TestLogin_Remote_StoresTheFingerprintAndActivatesOnlyWhenSignedIn(t *testing.T) {
	const platform = "remote_fake"
	fake := &remoteFake{}
	connectors.RegisterRemoteLogin(platform, fake)
	w := newWorld(t)
	ctx := context.Background()

	_, err := w.svc.StartRemoteLogin(ctx, w.user, LoginStart{Platform: "nowhere"})
	assert.ErrorIs(t, err, ErrLoginUnavailable)

	view, err := w.svc.StartRemoteLogin(ctx, w.user, LoginStart{Platform: platform})
	require.NoError(t, err)
	assert.Equal(t, "https://browser.example/login/1?ticket=t", view.ViewURL)
	assert.JSONEq(t, `{"os":"windows","timezone":"Asia/Bangkok","language":"th-TH"}`, string(view.Fingerprint))
	require.Len(t, fake.started, 1)
	acc, err := w.svc.userAccount(ctx, w.user, fake.started[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "remote_fake 1", acc.Label)
	assert.Equal(t, AccountKindBrowserSession, acc.Kind)
	assert.Equal(t, AccountStatusNeedsLogin, acc.Status)
	assert.JSONEq(t, string(view.Fingerprint), string(acc.Fingerprint.RawMessage))

	_, err = w.svc.FinishRemoteLogin(ctx, w.user, view.SessionID)
	assert.ErrorIs(t, err, ErrLoginIncomplete)
	assert.ErrorContains(t, err, "checkpoint page")
	got, err := w.svc.FinishRemoteLogin(ctx, w.user, view.SessionID)
	require.NoError(t, err, "the view stays open after an early finish")
	assert.Equal(t, AccountStatusActive, got.Status)

	again, err := w.svc.StartRemoteLogin(ctx, w.user, LoginStart{Platform: platform})
	require.NoError(t, err)
	require.Len(t, fake.started, 2)
	assert.Equal(t, "remote_fake 2", mustLabel(t, w, fake.started[1].ID))
	require.NoError(t, w.svc.CancelRemoteLogin(ctx, w.user, again.SessionID))
	assert.Equal(t, []int64{fake.started[1].ID}, fake.cancelled)
	_, err = w.svc.userAccount(ctx, w.user, fake.started[1].ID)
	assert.ErrorIs(t, err, ErrAccountNotFound, "cancelling removes the account the sign-in added")

	relogin, err := w.svc.StartRemoteLogin(ctx, w.user, LoginStart{AccountID: &got.ID})
	require.NoError(t, err)
	require.NoError(t, w.svc.CancelRemoteLogin(ctx, w.user, relogin.SessionID))
	kept, err := w.svc.userAccount(ctx, w.user, got.ID)
	require.NoError(t, err, "cancelling a re-login keeps the account")
	assert.Equal(t, AccountStatusActive, kept.Status)
}

func mustLabel(t *testing.T, w *world, id int64) string {
	t.Helper()
	acc, err := w.svc.userAccount(context.Background(), w.user, id)
	require.NoError(t, err)
	return acc.Label
}

func TestLogin_SetAccountEnabled_ReturnsANeverSignedInAccountToNeedsLogin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	fresh, err := w.svc.EnsureAccount(ctx, w.user, AccountSpec{
		Platform: connectors.PlatformTelegram, Label: "fresh", Status: AccountStatusNeedsLogin,
	})
	require.NoError(t, err)

	v, err := w.svc.SetAccountEnabled(ctx, w.user, fresh.ID, false)
	require.NoError(t, err)
	assert.Equal(t, AccountStatusDisabled, v.Status)
	v, err = w.svc.SetAccountEnabled(ctx, w.user, fresh.ID, true)
	require.NoError(t, err)
	assert.Equal(t, AccountStatusNeedsLogin, v.Status)

	worked := w.firstAccount(t)
	_, err = w.db.Exec(`UPDATE channel_accounts SET last_success_at = now() WHERE id = $1`, worked)
	require.NoError(t, err)
	_, err = w.svc.SetAccountEnabled(ctx, w.user, worked, false)
	require.NoError(t, err)
	v, err = w.svc.SetAccountEnabled(ctx, w.user, worked, true)
	require.NoError(t, err)
	assert.Equal(t, AccountStatusActive, v.Status)
}
