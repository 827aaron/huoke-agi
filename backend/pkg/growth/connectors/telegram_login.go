package connectors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pentagi/pkg/config"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type loginStep int

const (
	stepCode loginStep = iota
	stepPassword
	stepDone
)

// loginTTL bounds an abandoned login; Telegram expires the code sooner.
const loginTTL = 15 * time.Minute

var errLoginOver = errors.New("this telegram login has expired or been cancelled; start a new one")

// loginAPI is the part of *auth.Client a login calls; tests replace it.
type loginAPI interface {
	SendCode(ctx context.Context, phone string, options auth.SendCodeOptions) (tg.AuthSentCodeClass, error)
	SignIn(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error)
	Password(ctx context.Context, password string) (*tg.AuthAuthorization, error)
}

// telegramLogin is one operator login of an account. Telegram binds the code
// to the connection that asked for it, so the connection stays open from
// startTelegramLogin until Cancel or loginTTL. gotd writes the session to the
// account's SessionStore (or session file) as it creates it; it is signed in
// once Code or Password returns nil.
type telegramLogin struct {
	timeout time.Duration
	phone   string

	mu       sync.Mutex
	api      loginAPI
	codeHash string
	step     loginStep
	expired  <-chan struct{}
	stop     context.CancelFunc
	done     chan struct{}
}

var _ PhoneLoginSession = (*telegramLogin)(nil)

// startTelegramLogin connects as account, through account.Proxy, and asks
// Telegram to send a login code to phone (international format).
func startTelegramLogin(ctx context.Context, cfg *config.Config, account Account, phone string) (PhoneLoginSession, error) {
	tc, err := TelegramConfigFor(cfg)
	if err != nil {
		return nil, err
	}
	client, err := newTelegramClient(tc, account)
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loginTTL)
	ready := make(chan struct{})
	done := make(chan struct{})
	runErr := make(chan error, 1)
	go func() {
		defer close(done)
		runErr <- client.Run(runCtx, func(ctx context.Context) error {
			close(ready)
			<-ctx.Done()
			return nil
		})
	}()
	select {
	case <-ready:
	case err := <-runErr:
		cancel()
		return nil, fmt.Errorf("connect to telegram: %w", err)
	case <-time.After(tc.ConnectTimeout):
		cancel()
		<-done
		return nil, fmt.Errorf("%w: no connection to telegram within %s", ErrUnreachable, tc.ConnectTimeout)
	case <-ctx.Done():
		cancel()
		<-done
		return nil, ctx.Err()
	}

	l := &telegramLogin{timeout: tc.CallTimeout, phone: phone, api: client.Auth(), expired: runCtx.Done(), stop: cancel, done: done}
	if err := l.sendCode(ctx); err != nil {
		l.Cancel()
		return nil, err
	}
	return l, nil
}

func (l *telegramLogin) sendCode(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	sent, err := l.api.SendCode(ctx, l.phone, auth.SendCodeOptions{})
	if err != nil {
		return loginError(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch v := sent.(type) {
	case *tg.AuthSentCode:
		l.codeHash, l.step = v.PhoneCodeHash, stepCode
	case *tg.AuthSentCodeSuccess:
		l.step = stepDone
	default:
		return fmt.Errorf("unexpected telegram code response %T", sent)
	}
	return nil
}

// Code signs in with the code. ErrPasswordNeeded means the account has
// two-step verification; ErrBadCode leaves the login waiting for another code.
func (l *telegramLogin) Code(ctx context.Context, code string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.isExpired():
		return errLoginOver
	case l.step == stepDone:
		return nil
	case l.step == stepPassword:
		return ErrPasswordNeeded
	}
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	_, err := l.api.SignIn(ctx, l.phone, strings.TrimSpace(code), l.codeHash)
	switch {
	case errors.Is(err, auth.ErrPasswordAuthNeeded):
		l.step = stepPassword
		return ErrPasswordNeeded
	case errors.Is(err, &auth.SignUpRequired{}):
		return errors.New("this phone number has no Telegram account")
	case err != nil:
		return loginError(err)
	}
	l.step = stepDone
	return nil
}

// Password completes two-step verification; ErrBadCode leaves the login
// waiting for another password.
func (l *telegramLogin) Password(ctx context.Context, password string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.isExpired():
		return errLoginOver
	case l.step == stepDone:
		return nil
	case l.step == stepCode:
		return errors.New("this telegram login is still waiting for its code")
	}
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	if _, err := l.api.Password(ctx, password); err != nil {
		if errors.Is(err, auth.ErrPasswordInvalid) {
			return fmt.Errorf("%w: the two-step verification password is wrong", ErrBadCode)
		}
		return loginError(err)
	}
	l.step = stepDone
	return nil
}

func (l *telegramLogin) isExpired() bool {
	select {
	case <-l.expired:
		return true
	default:
		return false
	}
}

// Cancel drops the connection; the account's connector opens its own.
func (l *telegramLogin) Cancel() {
	if l.stop != nil {
		l.stop()
		<-l.done
	}
}

func loginError(err error) error {
	switch {
	case tgerr.Is(err, "PHONE_NUMBER_INVALID"):
		return errors.New("telegram does not accept this phone number; use the international format, such as +66812345678")
	case tgerr.Is(err, "PHONE_NUMBER_BANNED"):
		return errors.New("telegram has banned this phone number")
	case tgerr.Is(err, "PHONE_CODE_INVALID", "PHONE_CODE_EMPTY"):
		return fmt.Errorf("%w: the login code is wrong", ErrBadCode)
	case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
		return errors.New("the login code has expired; start the login again")
	}
	return errorOf(err)
}
