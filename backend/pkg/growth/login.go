package growth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"

	"github.com/sirupsen/logrus"
	"github.com/sqlc-dev/pqtype"
)

// loginTTL is how long a sign-in waits for the operator's next step; Telegram codes
// expire sooner than this, and the browser service's view tickets do too.
const loginTTL = 15 * time.Minute

const (
	LoginNextCode     = "code"
	LoginNextPassword = "password"
	LoginNextDone     = "done"
)

var (
	ErrLoginNotFound    = errors.New("sign-in not found or expired")
	ErrLoginUnavailable = errors.New("this platform has no sign-in in this build")
	// ErrLoginIncomplete is returned by FinishRemoteLogin while the browser is not signed in yet.
	ErrLoginIncomplete = errors.New("the browser is not signed in yet")
)

// LoginStep is where a Telegram sign-in stands; Account is set once Next is done.
type LoginStep struct {
	LoginID string       `json:"login_id"`
	Next    string       `json:"next"`
	Account *AccountView `json:"account"`
}

// RemoteLoginView is a server-side browser opened for an operator; ViewURL is the live view.
type RemoteLoginView struct {
	SessionID   string          `json:"session_id"`
	ViewURL     string          `json:"view_url"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	Fingerprint json.RawMessage `json:"fingerprint" swaggertype:"object"`
}

// LoginStart names the account a sign-in is for: AccountID signs an existing account in
// again, otherwise a new one is added. Proxy and ExternalBrowserWS are stored on the
// account first, so the sign-in already goes out the way the account's work will.
type LoginStart struct {
	AccountID         *int64  `json:"account_id,omitempty"`
	Platform          string  `json:"platform,omitempty"`
	Phone             string  `json:"phone,omitempty"`
	Proxy             *string `json:"proxy,omitempty"`
	ExternalBrowserWS *string `json:"external_browser_ws,omitempty"`
}

type pendingLogin struct {
	userID    int64
	accountID int64
	created   bool
	expires   time.Time

	// mu serialises the operator's steps on one sign-in.
	mu     sync.Mutex
	phone  connectors.PhoneLoginSession
	remote connectors.RemoteLogin
}

type loginRegistry struct {
	mu      sync.Mutex
	pending map[string]*pendingLogin
}

func newLoginID() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) addLogin(p *pendingLogin) string {
	s.logins.mu.Lock()
	defer s.logins.mu.Unlock()
	if s.logins.pending == nil {
		s.logins.pending = make(map[string]*pendingLogin)
	}
	s.sweepLoginsLocked()
	id := newLoginID()
	p.expires = time.Now().Add(loginTTL)
	s.logins.pending[id] = p
	return id
}

// login returns a pending sign-in of userID; another user's id reads as not found.
func (s *Service) login(userID int64, id string) (*pendingLogin, error) {
	s.logins.mu.Lock()
	defer s.logins.mu.Unlock()
	s.sweepLoginsLocked()
	p, ok := s.logins.pending[id]
	if !ok || p.userID != userID {
		return nil, ErrLoginNotFound
	}
	p.expires = time.Now().Add(loginTTL)
	return p, nil
}

func (s *Service) dropLogin(id string) {
	s.logins.mu.Lock()
	defer s.logins.mu.Unlock()
	delete(s.logins.pending, id)
}

func (s *Service) sweepLoginsLocked() {
	now := time.Now()
	for id, p := range s.logins.pending {
		if now.After(p.expires) {
			delete(s.logins.pending, id)
			if p.phone != nil {
				go p.phone.Cancel()
			}
		}
	}
}

// connectorAccount is what a factory or a sign-in gets for acc.
func (s *Service) connectorAccount(ctx context.Context, acc database.ChannelAccount) (connectors.Account, *config.Config, error) {
	cfg, err := s.ConfigFor(ctx, acc.UserID)
	if err != nil {
		return connectors.Account{}, nil, err
	}
	proxyURL, _, err := s.resolveProxy(acc, cfg)
	if err != nil {
		return connectors.Account{}, nil, err
	}
	browserWS, err := s.accountSecret(acc, accountFieldBrowserWS, acc.ExternalBrowserWs)
	if err != nil {
		return connectors.Account{}, nil, err
	}
	var fingerprint json.RawMessage
	if acc.Fingerprint.Valid {
		fingerprint = acc.Fingerprint.RawMessage
	}
	return connectors.Account{
		ID:         acc.ID,
		UserID:     acc.UserID,
		Platform:   connectors.Platform(acc.Platform),
		Kind:       acc.Kind,
		ExternalID: acc.ExternalID,
		ProfileDir: acc.ProfileDir,
		Session:    &accountSession{db: s.db, accountID: acc.ID},
		Health:     &accountHealth{s: s, accountID: acc.ID},

		Proxy:             proxyURL,
		Fingerprint:       fingerprint,
		ExternalBrowserWS: browserWS,
	}, cfg, nil
}

// loginAccount finds or adds the account of a sign-in and stores its network settings.
// created reports a row this sign-in added, which cancelling it removes again.
func (s *Service) loginAccount(
	ctx context.Context,
	userID int64,
	in LoginStart,
	spec AccountSpec,
) (acc database.ChannelAccount, created bool, err error) {
	if in.AccountID != nil {
		acc, err = s.userAccount(ctx, userID, *in.AccountID)
		if err != nil {
			return acc, false, err
		}
		if acc.Platform != string(spec.Platform) {
			return acc, false, fmt.Errorf("%w: account %d is a %s account", ErrInvalidSetting, acc.ID, acc.Platform)
		}
	} else {
		existing, err := s.db.GetUserChannelAccounts(ctx, userID)
		if err != nil {
			return acc, false, fmt.Errorf("failed to list accounts: %w", err)
		}
		found := false
		for _, a := range existing {
			if a.Platform == string(spec.Platform) && spec.Label != "" && a.Label == spec.Label {
				acc, found = a, true
				break
			}
		}
		if !found {
			if spec.Label == "" {
				spec.Label = nextLabel(existing, spec.Platform)
			}
			spec.Status = AccountStatusNeedsLogin
			if acc, err = s.EnsureAccount(ctx, userID, spec); err != nil {
				return acc, false, err
			}
			created = true
		}
	}

	if in.Proxy != nil || in.ExternalBrowserWS != nil {
		u := AccountUpdate{ProxyURL: in.Proxy, ExternalBrowserWS: in.ExternalBrowserWS}
		if _, err := s.UpdateAccount(ctx, userID, acc.ID, u); err != nil {
			if created {
				_, _ = s.db.DeleteUserChannelAccount(context.WithoutCancel(ctx),
					database.DeleteUserChannelAccountParams{ID: acc.ID, UserID: userID})
			}
			return acc, false, err
		}
		if acc, err = s.userAccount(ctx, userID, acc.ID); err != nil {
			return acc, false, err
		}
	}
	return acc, created, nil
}

// nextLabel is the first "<platform> N" no account of the platform carries yet.
func nextLabel(existing []database.ChannelAccount, platform connectors.Platform) string {
	used := make(map[string]bool, len(existing))
	for _, a := range existing {
		if a.Platform == string(platform) {
			used[a.Label] = true
		}
	}
	for n := 1; ; n++ {
		label := fmt.Sprintf("%s %d", platform, n)
		if !used[label] {
			return label
		}
	}
}

func (s *Service) signedIn(ctx context.Context, userID, accountID int64) (*AccountView, error) {
	acc, err := s.db.MarkChannelAccountSignedIn(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to activate account %d: %w", accountID, err)
	}
	s.DropConnector(accountID)
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	view := s.accountView(acc, cfg)
	return &view, nil
}

// StartTelegramLogin adds or re-signs a Telegram account by phone and has Telegram
// send the code. The phone number becomes the label of a new account.
func (s *Service) StartTelegramLogin(ctx context.Context, userID int64, in LoginStart) (LoginStep, error) {
	login, ok := connectors.LookupPhoneLogin(connectors.PlatformTelegram)
	if !ok {
		return LoginStep{}, fmt.Errorf("%w: %s", ErrLoginUnavailable, connectors.PlatformTelegram)
	}
	phone := normalizePhone(in.Phone)
	if phone == "" {
		return LoginStep{}, fmt.Errorf("%w: phone must be an international number such as +66812345678", ErrInvalidSetting)
	}
	acc, created, err := s.loginAccount(ctx, userID, in, AccountSpec{
		Platform: connectors.PlatformTelegram, Kind: AccountKindAPISession, Label: phone, ExternalID: phone,
	})
	if err != nil {
		return LoginStep{}, err
	}
	account, cfg, err := s.connectorAccount(ctx, acc)
	if err == nil {
		var session connectors.PhoneLoginSession
		if session, err = login(ctx, cfg, account, phone); err == nil {
			id := s.addLogin(&pendingLogin{userID: userID, accountID: acc.ID, created: created, phone: session})
			return LoginStep{LoginID: id, Next: LoginNextCode}, nil
		}
	}
	if created {
		_, _ = s.db.DeleteUserChannelAccount(context.WithoutCancel(ctx),
			database.DeleteUserChannelAccountParams{ID: acc.ID, UserID: userID})
	}
	return LoginStep{}, err
}

func normalizePhone(raw string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return ""
		}
	}
	digits := strings.TrimPrefix(b.String(), "+")
	if len(digits) < 7 || len(digits) > 15 {
		return ""
	}
	return "+" + digits
}

// SubmitTelegramCode passes the code on. A rejected code leaves the sign-in open
// for another try; two-step verification moves it to the password step.
func (s *Service) SubmitTelegramCode(ctx context.Context, userID int64, loginID, code string) (LoginStep, error) {
	return s.telegramStep(ctx, userID, loginID, func(p *pendingLogin) error {
		return p.phone.Code(ctx, strings.TrimSpace(code))
	})
}

func (s *Service) SubmitTelegramPassword(ctx context.Context, userID int64, loginID, password string) (LoginStep, error) {
	return s.telegramStep(ctx, userID, loginID, func(p *pendingLogin) error {
		return p.phone.Password(ctx, password)
	})
}

func (s *Service) telegramStep(ctx context.Context, userID int64, loginID string, step func(*pendingLogin) error) (LoginStep, error) {
	p, err := s.login(userID, loginID)
	if err != nil || p.phone == nil {
		return LoginStep{}, ErrLoginNotFound
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	switch err := step(p); {
	case errors.Is(err, connectors.ErrPasswordNeeded):
		return LoginStep{LoginID: loginID, Next: LoginNextPassword}, nil
	case err != nil:
		return LoginStep{}, err
	}
	s.dropLogin(loginID)
	p.phone.Cancel()
	view, err := s.signedIn(ctx, userID, p.accountID)
	if err != nil {
		return LoginStep{}, err
	}
	return LoginStep{LoginID: loginID, Next: LoginNextDone, Account: view}, nil
}

// StartRemoteLogin opens the account's browser on the server for an operator to sign
// in by hand, adding a browser_session account when none is named.
func (s *Service) StartRemoteLogin(ctx context.Context, userID int64, in LoginStart) (RemoteLoginView, error) {
	platform := connectors.Platform(strings.TrimSpace(in.Platform))
	if in.AccountID != nil && platform == "" {
		acc, err := s.userAccount(ctx, userID, *in.AccountID)
		if err != nil {
			return RemoteLoginView{}, err
		}
		platform = connectors.Platform(acc.Platform)
	}
	remote, ok := connectors.LookupRemoteLogin(platform)
	if !ok {
		return RemoteLoginView{}, fmt.Errorf("%w: %q", ErrLoginUnavailable, platform)
	}
	acc, created, err := s.loginAccount(ctx, userID, in, AccountSpec{Platform: platform, Kind: AccountKindBrowserSession})
	if err != nil {
		return RemoteLoginView{}, err
	}
	discard := func() {
		if created {
			_, _ = s.db.DeleteUserChannelAccount(context.WithoutCancel(ctx),
				database.DeleteUserChannelAccountParams{ID: acc.ID, UserID: userID})
		}
	}
	account, cfg, err := s.connectorAccount(ctx, acc)
	if err != nil {
		discard()
		return RemoteLoginView{}, err
	}
	// The browser must not be driven by the work while an operator is in it.
	s.DropConnector(acc.ID)
	view, err := remote.Start(ctx, cfg, account)
	if err != nil {
		discard()
		return RemoteLoginView{}, err
	}

	if len(view.Fingerprint) > 0 && string(view.Fingerprint) != "null" && !acc.Fingerprint.Valid {
		_, _ = s.db.UpdateUserChannelAccount(ctx, database.UpdateUserChannelAccountParams{
			ID: acc.ID, UserID: userID, Label: acc.Label, Status: acc.Status,
			Proxy: acc.Proxy, ExternalBrowserWs: acc.ExternalBrowserWs,
			Fingerprint: pqtype.NullRawMessage{RawMessage: view.Fingerprint, Valid: true},
		})
	}
	fingerprint := view.Fingerprint
	if acc.Fingerprint.Valid {
		fingerprint = acc.Fingerprint.RawMessage
	}
	out := RemoteLoginView{
		SessionID:   s.addLogin(&pendingLogin{userID: userID, accountID: acc.ID, created: created, remote: remote}),
		ViewURL:     view.ViewURL,
		Fingerprint: fingerprint,
	}
	if !view.ExpiresAt.IsZero() {
		out.ExpiresAt = &view.ExpiresAt
	}
	return out, nil
}

// FinishRemoteLogin checks the page the operator left. Signed in, the account turns
// active and the view closes; otherwise ErrLoginIncomplete carries what the page shows
// and the view stays open.
func (s *Service) FinishRemoteLogin(ctx context.Context, userID int64, sessionID string) (AccountView, error) {
	p, account, cfg, err := s.remoteLogin(ctx, userID, sessionID)
	if err != nil {
		return AccountView{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	ok, reason, err := p.remote.Finish(ctx, cfg, account)
	switch {
	case err != nil:
		return AccountView{}, err
	case !ok:
		if reason == "" {
			reason = "the page still shows the login form"
		}
		return AccountView{}, fmt.Errorf("%w: %s", ErrLoginIncomplete, reason)
	}
	s.dropLogin(sessionID)
	view, err := s.signedIn(ctx, userID, p.accountID)
	if err != nil {
		return AccountView{}, err
	}
	return *view, nil
}

// CancelRemoteLogin closes the view and leaves the account as it was; an account the
// sign-in added is removed again.
func (s *Service) CancelRemoteLogin(ctx context.Context, userID int64, sessionID string) error {
	p, account, cfg, err := s.remoteLogin(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	s.dropLogin(sessionID)
	err = p.remote.Cancel(ctx, cfg, account)
	if p.created {
		if _, derr := s.db.DeleteUserChannelAccount(ctx, database.DeleteUserChannelAccountParams{
			ID: p.accountID, UserID: userID,
		}); derr != nil {
			err = errors.Join(err, fmt.Errorf("failed to remove account %d: %w", p.accountID, derr))
		}
	}
	return err
}

func (s *Service) remoteLogin(
	ctx context.Context,
	userID int64,
	sessionID string,
) (*pendingLogin, connectors.Account, *config.Config, error) {
	p, err := s.login(userID, sessionID)
	if err != nil || p.remote == nil {
		return nil, connectors.Account{}, nil, ErrLoginNotFound
	}
	acc, err := s.userAccount(ctx, userID, p.accountID)
	if err != nil {
		s.dropLogin(sessionID)
		return nil, connectors.Account{}, nil, err
	}
	account, cfg, err := s.connectorAccount(ctx, acc)
	return p, account, cfg, err
}

// DeleteAccount removes an account with its session and its join records: its sources
// stay in the pool, now not joined by it, and its leads stay as they are. A task that
// was using it moves to another account on its next call. A browser_session account's
// profile is then removed from the browser service, best effort, so an unreachable
// service leaves an orphaned profile rather than an account that cannot be deleted.
func (s *Service) DeleteAccount(ctx context.Context, userID, accountID int64) error {
	a, err := s.userAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}
	logger := logrus.WithContext(ctx).WithField("account_id", accountID)

	var (
		forgetter connectors.ProfileForgetter
		account   connectors.Account
		cfg       *config.Config
	)
	if remote, ok := connectors.LookupRemoteLogin(connectors.Platform(a.Platform)); ok {
		if f, ok := remote.(connectors.ProfileForgetter); ok {
			if account, cfg, err = s.connectorAccount(ctx, a); err != nil {
				logger.WithError(err).Warn("deleting account without removing its browser profile")
			} else {
				forgetter = f
			}
		}
	}

	n, err := s.db.DeleteUserChannelAccount(ctx, database.DeleteUserChannelAccountParams{ID: accountID, UserID: userID})
	if err != nil {
		return fmt.Errorf("failed to delete account %d: %w", accountID, err)
	}
	if n == 0 {
		return ErrAccountNotFound
	}
	s.dropLoginsOf(accountID)
	s.DropConnector(accountID)

	if forgetter != nil {
		if err := forgetter.Forget(ctx, cfg, account); err != nil {
			logger.WithError(err).Warn("account deleted but its browser profile was not removed")
		}
	}
	return nil
}

func (s *Service) dropLoginsOf(accountID int64) {
	s.logins.mu.Lock()
	defer s.logins.mu.Unlock()
	for id, p := range s.logins.pending {
		if p.accountID == accountID {
			delete(s.logins.pending, id)
			if p.phone != nil {
				go p.phone.Cancel()
			}
		}
	}
}

// SetAccountEnabled disables an account, or returns it to work: active when it has
// been signed in before, needs_login otherwise.
func (s *Service) SetAccountEnabled(ctx context.Context, userID, accountID int64, enabled bool) (AccountView, error) {
	a, err := s.userAccount(ctx, userID, accountID)
	if err != nil {
		return AccountView{}, err
	}
	status := AccountStatusDisabled
	if enabled {
		status = AccountStatusNeedsLogin
		if a.LastSuccessAt.Valid || (a.Kind == AccountKindAPISession && len(a.Session) > 0) {
			status = AccountStatusActive
		}
	}
	if a.Status != AccountStatusDisabled && enabled {
		status = a.Status
	}
	updated, err := s.db.UpdateChannelAccountStatus(ctx, database.UpdateChannelAccountStatusParams{ID: a.ID, Status: status})
	if err != nil {
		return AccountView{}, fmt.Errorf("failed to update account %d: %w", accountID, err)
	}
	s.DropConnector(accountID)
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return AccountView{}, err
	}
	return s.accountView(updated, cfg), nil
}
