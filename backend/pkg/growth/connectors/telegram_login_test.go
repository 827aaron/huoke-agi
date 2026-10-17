package connectors

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"pentagi/pkg/config"

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

func newTestLogin(t *testing.T, api *fakeLoginAPI) *telegramLogin {
	t.Helper()
	l := &telegramLogin{timeout: 5 * time.Second, phone: "+66812345678", api: api, expired: make(chan struct{})}
	require.NoError(t, l.sendCode(context.Background()))
	return l
}

func sentCode(phone string) (tg.AuthSentCodeClass, error) {
	return &tg.AuthSentCode{PhoneCodeHash: "hash-for-" + phone}, nil
}

func TestTelegramLogin_Code_SignsIn(t *testing.T) {
	signIns := 0
	l := newTestLogin(t, &fakeLoginAPI{
		sendCode: sentCode,
		signIn: func(phone, code, hash string) (*tg.AuthAuthorization, error) {
			signIns++
			assert.Equal(t, "+66812345678", phone)
			assert.Equal(t, "hash-for-+66812345678", hash)
			if code != "12345" {
				return nil, tgerr.New(400, "PHONE_CODE_INVALID")
			}
			return &tg.AuthAuthorization{}, nil
		},
	})
	ctx := context.Background()

	assert.ErrorIs(t, l.Code(ctx, "00000"), ErrBadCode)
	require.NoError(t, l.Code(ctx, " 12345 "))
	require.NoError(t, l.Code(ctx, "12345"), "a signed-in login stays signed in")
	assert.Equal(t, 2, signIns)
}

func TestTelegramLogin_Password_TwoStep(t *testing.T) {
	l := newTestLogin(t, &fakeLoginAPI{
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

	assert.ErrorContains(t, l.Password(ctx, "s3cret"), "waiting for its code")
	assert.ErrorIs(t, l.Code(ctx, "12345"), ErrPasswordNeeded)
	assert.ErrorIs(t, l.Code(ctx, "12345"), ErrPasswordNeeded, "a second code does not restart the login")
	assert.ErrorIs(t, l.Password(ctx, "wrong"), ErrBadCode)
	require.NoError(t, l.Password(ctx, "s3cret"))
}

func TestTelegramLogin_SendCode(t *testing.T) {
	t.Run("signed in without a code", func(t *testing.T) {
		l := newTestLogin(t, &fakeLoginAPI{sendCode: func(string) (tg.AuthSentCodeClass, error) {
			return &tg.AuthSentCodeSuccess{}, nil
		}})
		assert.NoError(t, l.Code(context.Background(), ""))
	})
	t.Run("flood wait", func(t *testing.T) {
		l := &telegramLogin{timeout: time.Second, api: &fakeLoginAPI{sendCode: func(string) (tg.AuthSentCodeClass, error) {
			return nil, tgerr.New(420, "FLOOD_WAIT_300")
		}}}
		var flood *FloodWaitError
		require.ErrorAs(t, l.sendCode(context.Background()), &flood)
		assert.Equal(t, 300*time.Second, flood.Wait)
	})
	t.Run("bad number", func(t *testing.T) {
		l := &telegramLogin{timeout: time.Second, api: &fakeLoginAPI{sendCode: func(string) (tg.AuthSentCodeClass, error) {
			return nil, tgerr.New(400, "PHONE_NUMBER_INVALID")
		}}}
		assert.ErrorContains(t, l.sendCode(context.Background()), "international format")
	})
}

func TestTelegramLogin_Code_ExpiredCodeIsNotRetryable(t *testing.T) {
	l := newTestLogin(t, &fakeLoginAPI{sendCode: sentCode, signIn: func(string, string, string) (*tg.AuthAuthorization, error) {
		return nil, tgerr.New(400, "PHONE_CODE_EXPIRED")
	}})
	err := l.Code(context.Background(), "12345")
	assert.ErrorContains(t, err, "start the login again")
	assert.NotErrorIs(t, err, ErrBadCode)
}

func TestTelegramLogin_Code_AfterExpiry(t *testing.T) {
	l := newTestLogin(t, &fakeLoginAPI{sendCode: sentCode})
	expired := make(chan struct{})
	close(expired)
	l.expired = expired
	assert.ErrorIs(t, l.Code(context.Background(), "12345"), errLoginOver)
}

func TestTelegramLogin_StartTelegramLogin_NamesTheProxyWhenTelegramIsUnreachable(t *testing.T) {
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		_ = silent.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	go func() {
		for {
			conn, err := silent.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go func() {
				// Grant the SOCKS5 greeting and CONNECT, then say nothing, as a route that
				// reaches no Telegram data centre does.
				buf := make([]byte, 262)
				if _, err := io.ReadAtLeast(conn, buf, 3); err != nil {
					return
				}
				_, _ = conn.Write([]byte{5, 0})
				if _, err := io.ReadAtLeast(conn, buf, 10); err != nil {
					return
				}
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
			}()
		}
	}()
	t.Setenv("TG_CONNECT_TIMEOUT", "300ms")

	start := time.Now()
	_, err = startTelegramLogin(t.Context(),
		&config.Config{TGAPIID: 12345, TGAPIHash: "abc", TGSessionDir: t.TempDir()},
		Account{ID: 1, Proxy: "socks5://" + silent.Addr().String()}, "+66812345678")
	assert.ErrorIs(t, err, ErrUnreachable)
	assert.Less(t, time.Since(start), 5*time.Second)
}
