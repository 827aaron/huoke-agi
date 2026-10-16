package connectors

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLoginAPI struct {
	sendCode func(phone string) (tg.AuthSentCodeClass, error)
	signIn   func(phone, code, hash string) (*tg.AuthAuthorization, error)
	password func(password string) (*tg.AuthAuthorization, error)
}

func (f *fakeLoginAPI) SendCode(ctx context.Context, phone string, _ auth.SendCodeOptions) (tg.AuthSentCodeClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.sendCode(phone)
}

func (f *fakeLoginAPI) SignIn(ctx context.Context, phone, code, hash string) (*tg.AuthAuthorization, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.signIn(phone, code, hash)
}

func (f *fakeLoginAPI) Password(ctx context.Context, password string) (*tg.AuthAuthorization, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.password(password)
}

func newTestLogin(t *testing.T, api *fakeLoginAPI) (*TelegramLogin, LoginStep) {
	t.Helper()
	l := &TelegramLogin{timeout: 5 * time.Second, phone: "+66812345678", api: api, expired: make(chan struct{})}
	step, err := l.sendCode(context.Background())
	require.NoError(t, err)
	return l, step
}

func sentCode(phone string) (tg.AuthSentCodeClass, error) {
	return &tg.AuthSentCode{PhoneCodeHash: "hash-for-" + phone}, nil
}

func TestTelegramLogin_SubmitCode_SignsIn(t *testing.T) {
	l, step := newTestLogin(t, &fakeLoginAPI{
		sendCode: sentCode,
		signIn: func(phone, code, hash string) (*tg.AuthAuthorization, error) {
			assert.Equal(t, "+66812345678", phone)
			assert.Equal(t, "hash-for-+66812345678", hash)
			if code != "12345" {
				return nil, tgerr.New(400, "PHONE_CODE_INVALID")
			}
			return &tg.AuthAuthorization{}, nil
		},
	})
	require.Equal(t, LoginStepCode, step)
	ctx := context.Background()

	step, err := l.SubmitCode(ctx, "00000")
	assert.ErrorContains(t, err, "code is wrong")
	assert.Equal(t, LoginStepCode, step)

	step, err = l.SubmitCode(ctx, "12345")
	require.NoError(t, err)
	assert.Equal(t, LoginStepDone, step)

	_, err = l.SubmitCode(ctx, "12345")
	assert.ErrorIs(t, err, errLoginOver)
}

func TestTelegramLogin_SubmitPassword_TwoStep(t *testing.T) {
	l, _ := newTestLogin(t, &fakeLoginAPI{
		sendCode: sentCode,
		signIn: func(string, string, string) (*tg.AuthAuthorization, error) {
			return nil, auth.ErrPasswordAuthNeeded
		},
		password: func(password string) (*tg.AuthAuthorization, error) {
			if password != "s3cret" {
				return nil, auth.ErrPasswordInvalid
			}
			return &tg.AuthAuthorization{}, nil
		},
	})
	ctx := context.Background()

	_, err := l.SubmitPassword(ctx, "s3cret")
	assert.ErrorContains(t, err, "waiting for its code")

	step, err := l.SubmitCode(ctx, "12345")
	require.NoError(t, err)
	assert.Equal(t, LoginStepPassword, step)

	step, err = l.SubmitPassword(ctx, "wrong")
	assert.ErrorContains(t, err, "password is wrong")
	assert.Equal(t, LoginStepPassword, step)

	step, err = l.SubmitPassword(ctx, "s3cret")
	require.NoError(t, err)
	assert.Equal(t, LoginStepDone, step)
}

func TestTelegramLogin_SendCode(t *testing.T) {
	t.Run("signed in without a code", func(t *testing.T) {
		_, step := newTestLogin(t, &fakeLoginAPI{sendCode: func(string) (tg.AuthSentCodeClass, error) {
			return &tg.AuthSentCodeSuccess{}, nil
		}})
		assert.Equal(t, LoginStepDone, step)
	})
	t.Run("flood wait", func(t *testing.T) {
		l := &TelegramLogin{timeout: time.Second, api: &fakeLoginAPI{sendCode: func(string) (tg.AuthSentCodeClass, error) {
			return nil, tgerr.New(420, "FLOOD_WAIT_300")
		}}}
		_, err := l.sendCode(context.Background())
		var flood *FloodWaitError
		require.ErrorAs(t, err, &flood)
		assert.Equal(t, 300*time.Second, flood.Wait)
	})
	t.Run("bad number", func(t *testing.T) {
		l := &TelegramLogin{timeout: time.Second, api: &fakeLoginAPI{sendCode: func(string) (tg.AuthSentCodeClass, error) {
			return nil, tgerr.New(400, "PHONE_NUMBER_INVALID")
		}}}
		_, err := l.sendCode(context.Background())
		assert.ErrorContains(t, err, "international format")
	})
}

func TestTelegramLogin_SubmitCode_AfterExpiry(t *testing.T) {
	l, _ := newTestLogin(t, &fakeLoginAPI{sendCode: sentCode})
	expired := make(chan struct{})
	close(expired)
	l.expired = expired
	_, err := l.SubmitCode(context.Background(), "12345")
	assert.ErrorIs(t, err, errLoginOver)
}
