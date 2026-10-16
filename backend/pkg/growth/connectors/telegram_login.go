package connectors

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"pentagi/pkg/config"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type LoginStep string

const (
	LoginStepCode     LoginStep = "code"
	LoginStepPassword LoginStep = "password"
	LoginStepDone     LoginStep = "done"
)

// loginTTL bounds an abandoned login; Telegram expires the code sooner.
const loginTTL = 15 * time.Minute

var errLoginOver = errors.New("this telegram login is finished or expired; start a new one")

// loginAPI is the part of *auth.Client a login calls; tests replace it.
type loginAPI interface {
	SendCode(ctx context.Context, phone string, options auth.SendCodeOptions) (tg.AuthSentCodeClass, error)
	SignIn(ctx context.Context, phone, code, codeHash string) (*tg.AuthAuthorization, error)
	Password(ctx context.Context, password string) (*tg.AuthAuthorization, error)
}

// TelegramLogin is one operator login of an account. Telegram binds the code
// to the connection that asked for it, so the connection stays open from
// StartTelegramLogin until the login is done, closed or expired. The session
// is written to the account's SessionStore (or session file) as gotd
// creates it, and is authorized once the step reaches done.
type TelegramLogin struct {
	timeout time.Duration
	phone   string

	mu       sync.Mutex
	api      loginAPI
	codeHash string
	step     LoginStep
	expired  <-chan struct{}
	stop     context.CancelFunc
	done     chan struct{}
}

// StartTelegramLogin connects as acc, through acc.Proxy, and asks Telegram to
// send a login code to phone (international format). The step is code, or
// done when Telegram signs the session in without one.
func StartTelegramLogin(ctx context.Context, cfg *config.Config, acc Account, phone string) (*TelegramLogin, LoginStep, error) {
	tc, err := TelegramConfigFor(cfg)
	if err != nil {
		return nil, "", err
	}
	client, err := newTelegramClient(tc, acc)
	if err != nil {
		return nil, "", err
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
		return nil, "", fmt.Errorf("connect to telegram: %w", err)
	case <-ctx.Done():
		cancel()
		<-done
		return nil, "", ctx.Err()
	}

	l := &TelegramLogin{timeout: tc.CallTimeout, phone: phone, api: client.Auth(), expired: runCtx.Done(), stop: cancel, done: done}
	step, err := l.sendCode(ctx)
	if err != nil {
		_ = l.Close()
		return nil, "", err
	}
	return l, step, nil
}

func (l *TelegramLogin) sendCode(ctx context.Context) (LoginStep, error) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	sent, err := l.api.SendCode(ctx, l.phone, auth.SendCodeOptions{})
	if err != nil {
		return "", loginError(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch v := sent.(type) {
	case *tg.AuthSentCode:
		l.codeHash, l.step = v.PhoneCodeHash, LoginStepCode
	case *tg.AuthSentCodeSuccess:
		l.step = LoginStepDone
	default:
		return "", fmt.Errorf("unexpected telegram code response %T", sent)
	}
	return l.step, nil
}

// SubmitCode signs in with the code. The step is done, or password when the
// account has two-step verification. A wrong or expired code is an error and
// leaves the step at code.
func (l *TelegramLogin) SubmitCode(ctx context.Context, code string) (LoginStep, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.step != LoginStepCode || l.isExpired() {
		return l.step, l.wrongStep(LoginStepCode)
	}
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	_, err := l.api.SignIn(ctx, l.phone, code, l.codeHash)
	switch {
	case errors.Is(err, auth.ErrPasswordAuthNeeded):
		l.step = LoginStepPassword
	case errors.Is(err, &auth.SignUpRequired{}):
		return l.step, errors.New("this phone number has no Telegram account")
	case err != nil:
		return l.step, loginError(err)
	default:
		l.finish()
	}
	return l.step, nil
}

// SubmitPassword completes two-step verification. A wrong password is an
// error and leaves the step at password.
func (l *TelegramLogin) SubmitPassword(ctx context.Context, password string) (LoginStep, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.step != LoginStepPassword || l.isExpired() {
		return l.step, l.wrongStep(LoginStepPassword)
	}
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	if _, err := l.api.Password(ctx, password); err != nil {
		if errors.Is(err, auth.ErrPasswordInvalid) {
			return l.step, errors.New("the two-step verification password is wrong")
		}
		return l.step, loginError(err)
	}
	l.finish()
	return l.step, nil
}

func (l *TelegramLogin) wrongStep(want LoginStep) error {
	if l.step == "" || l.step == LoginStepDone || l.isExpired() {
		return errLoginOver
	}
	return fmt.Errorf("this telegram login is waiting for its %s, not its %s", l.step, want)
}

func (l *TelegramLogin) isExpired() bool {
	select {
	case <-l.expired:
		return true
	default:
		return false
	}
}

// finish ends the connection once signed in; the connector opens its own.
// The caller holds l.mu.
func (l *TelegramLogin) finish() {
	l.step = LoginStepDone
	go func() { _ = l.Close() }()
}

// Close drops the connection; an unfinished login cannot continue after it.
func (l *TelegramLogin) Close() error {
	if l.stop != nil {
		l.stop()
		<-l.done
	}
	return nil
}

func loginError(err error) error {
	switch {
	case tgerr.Is(err, "PHONE_NUMBER_INVALID"):
		return errors.New("telegram does not accept this phone number; use the international format, such as +66812345678")
	case tgerr.Is(err, "PHONE_NUMBER_BANNED"):
		return errors.New("telegram has banned this phone number")
	case tgerr.Is(err, "PHONE_CODE_INVALID"):
		return errors.New("the login code is wrong")
	case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
		return errors.New("the login code has expired; start the login again")
	}
	return errorOf(err)
}
