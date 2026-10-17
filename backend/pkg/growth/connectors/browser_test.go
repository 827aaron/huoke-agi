package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowser_Navigate_SendsTokenAndDecodesThePage(t *testing.T) {
	fb := newFakeBrowser(t)
	fb.page("https://www.facebook.com/groups/x/", &fakePage{Steps: [][]Capture{{{Seq: 7, URL: fbGraphQL, Status: 200, Body: `{"a":1}`}}}})
	c := fb.client(t, BrowserConfig{})

	page, err := c.Navigate(context.Background(), "acct", "facebook", "https://www.facebook.com/groups/x/", time.Second)
	require.NoError(t, err)
	assert.Equal(t, &BrowserPage{URL: "https://www.facebook.com/groups/x/", HTTPStatus: 200, ReachedEnd: true,
		Captures: []Capture{{Seq: 7, URL: fbGraphQL, Status: 200, Body: `{"a":1}`}}}, page)

	fb.token = "rotated"
	_, err = c.Navigate(context.Background(), "acct", "facebook", "https://www.facebook.com/groups/x/", time.Second)
	assert.Equal(t, WebErrUnauthorized, WebErrorCodeOf(err))
}

func TestBrowser_Post_MapsServiceErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		header    string
		wantCode  WebErrorCode
		wantRetry time.Duration
		wantIs    error
	}{
		{name: "needs login", status: 423,
			body:     `{"error":{"code":"needs_login","message":"login form on page"},"account":{"account_id":"acct-9","platform":"facebook","status":"needs_login","retry_after_sec":0,"challenge":true,"reason":"login form on page","warming_up":false}}`,
			wantCode: WebErrNeedsLogin, wantIs: ErrWebNeedsLogin},
		{name: "rate limited carries the wait", status: 429,
			body:     `{"error":{"code":"rate_limited","message":"slow down","retry_after_sec":600},"account":{"account_id":"acct-9","platform":"instagram","status":"rate_limited","retry_after_sec":600,"challenge":false,"reason":"slow down","warming_up":true}}`,
			wantCode: WebErrRateLimited, wantRetry: 10 * time.Minute, wantIs: ErrWebRateLimited},
		{name: "retry-after header when the body has none", status: 429,
			body: `{"error":{"code":"rate_limited","message":"slow down"}}`, header: "90",
			wantCode: WebErrRateLimited, wantRetry: 90 * time.Second},
		{name: "not found", status: 404, body: `{"error":{"code":"not_found","message":"404"}}`, wantCode: WebErrNotFound, wantIs: ErrWebNotFound},
		{name: "a proxy error page is internal", status: 502, body: `<html>Bad Gateway</html>`, wantCode: WebErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.header != "" {
					w.Header().Set("Retry-After", tt.header)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c, err := NewBrowserClient(BrowserConfig{BaseURL: srv.URL})
			require.NoError(t, err)

			_, err = c.Scroll(context.Background(), "acct-9", 1, time.Second)
			var we *WebError
			require.ErrorAs(t, err, &we)
			assert.Equal(t, tt.wantCode, we.Code)
			assert.Equal(t, tt.wantRetry, we.RetryAfter)
			assert.Equal(t, "acct-9", we.AccountID)
			if tt.wantIs != nil {
				assert.True(t, errors.Is(err, tt.wantIs))
			}
		})
	}
}

func TestBrowser_StartLogin_UsesTheLoginRoutes(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.EscapedPath())
		switch r.URL.Path {
		case "/sessions/ig-1/login/start":
			_, _ = w.Write([]byte(`{"account_id":"ig-1","platform":"instagram","state":"needs_login","login_url":"https://ops.example/browser/login/ig-1?ticket=abc","expires_at":"2026-10-16T22:30:00.000Z"}`))
		case "/sessions/ig-1/login/confirm":
			_, _ = w.Write([]byte(`{"account_id":"ig-1","platform":"instagram","state":"ok","login_confirmed_at":"2026-10-16T22:10:00.000Z"}`))
		case "/sessions/ig-1/login/cancel":
			_, _ = w.Write([]byte(`{"account_id":"ig-1","platform":"instagram","state":"ok","open":false}`))
		case "/sessions/ig-1":
			_, _ = w.Write([]byte(`{"account_id":"ig-1","platform":"instagram","state":"ok"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c, err := NewBrowserClient(BrowserConfig{BaseURL: srv.URL})
	require.NoError(t, err)

	login, err := c.StartLogin(context.Background(), "ig-1", "instagram", "")
	require.NoError(t, err)
	assert.Equal(t, "https://ops.example/browser/login/ig-1?ticket=abc", login.LoginURL)
	assert.Equal(t, time.Date(2026, 10, 16, 22, 30, 0, 0, time.UTC), login.ExpiresAt.UTC())
	assert.Equal(t, "needs_login", login.State)

	done, err := c.ConfirmLogin(context.Background(), "ig-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", done.State)
	require.NotNil(t, done.LoginConfirmedAt)

	acct, err := c.Account(context.Background(), "ig-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", acct.State)

	cancelled, err := c.CancelLogin(context.Background(), "ig-1")
	require.NoError(t, err)
	assert.False(t, cancelled.Open)
	assert.Equal(t, []string{"POST /sessions/ig-1/login/start", "POST /sessions/ig-1/login/confirm", "GET /sessions/ig-1", "POST /sessions/ig-1/login/cancel"}, seen)
}

func TestBrowser_Post_SitsOutAShortRateLimitOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","retry_after_sec":0.05}}`))
			return
		}
		_, _ = w.Write([]byte(`{"url":"https://www.instagram.com/","reached_end":true}`))
	}))
	defer srv.Close()

	c, err := NewBrowserClient(BrowserConfig{BaseURL: srv.URL, MaxInlineBackoff: time.Second})
	require.NoError(t, err)
	start := time.Now()
	page, err := c.Scroll(context.Background(), "acct", 1, 0)
	require.NoError(t, err)
	assert.True(t, page.ReachedEnd)
	assert.Equal(t, int32(2), calls.Load())
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)

	calls.Store(0)
	c, err = NewBrowserClient(BrowserConfig{BaseURL: srv.URL})
	require.NoError(t, err)
	_, err = c.Scroll(context.Background(), "acct", 1, 0)
	assert.ErrorIs(t, err, ErrWebRateLimited)
	assert.Equal(t, int32(1), calls.Load())
}

func TestBrowser_Post_StopsWaitingWhenTheContextEnds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","retry_after_sec":30}}`))
	}))
	defer srv.Close()
	c, err := NewBrowserClient(BrowserConfig{BaseURL: srv.URL, MaxInlineBackoff: time.Minute})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.Scroll(ctx, "acct", 1, 0)
		done <- err
	}()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(10 * time.Second):
		t.Fatal("Scroll kept waiting out the rate limit after its context ended")
	}
}

func TestBrowser_LoginURL_CarriesPlatformAndToken(t *testing.T) {
	c, err := NewBrowserClient(BrowserConfig{BaseURL: "http://browser:7070/", Token: "s3cret"})
	require.NoError(t, err)
	assert.Equal(t, "http://browser:7070/login/fb%20one?platform=facebook&token=s3cret", c.LoginURL("fb one", "facebook"))
	assert.Equal(t, "http://browser:7070/login/..%2Fadmin?token=s3cret", c.LoginURL("../admin", ""))
}

func TestBrowser_NewBrowserClient_NeedsAURL(t *testing.T) {
	_, err := NewBrowserClient(BrowserConfig{})
	assert.ErrorIs(t, err, ErrWebNotConfigured)
}

func TestBrowser_SetProfile_EveryCallForTheAccountCarriesIt(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies[r.URL.Path] = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"account_id":"a1","proxy":"http://***:***@p:8080","external_browser":false,"fingerprint":{"platform":"Win32"}}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewBrowserClient(BrowserConfig{BaseURL: srv.URL})
	require.NoError(t, err)
	NewFacebook(c, "a1", FacebookConfig{Profile: &BrowserProfile{Proxy: "http://u:p@p:8080", Region: "JP", Fingerprint: json.RawMessage(`{"platform":"Win32"}`)}})

	acct, err := c.Configure(context.Background(), "a1", PlatformFacebook)
	require.NoError(t, err)
	assert.JSONEq(t, `{"platform":"Win32"}`, string(acct.Fingerprint))
	assert.Equal(t, "http://***:***@p:8080", acct.Proxy)
	_, err = c.StartLogin(context.Background(), "a1", PlatformFacebook, "")
	require.NoError(t, err)
	_, err = c.Navigate(context.Background(), "other", PlatformFacebook, "https://x", 0)
	require.NoError(t, err)

	want := map[string]any{"proxy": "http://u:p@p:8080", "external_browser_ws": "", "region": "JP", "fingerprint": map[string]any{"platform": "Win32"}}
	for _, path := range []string{"/sessions/a1/config", "/sessions/a1/login/start"} {
		for k, v := range want {
			assert.Equal(t, v, bodies[path][k], path+" "+k)
		}
	}
	assert.NotContains(t, bodies["/sessions/other/navigate"], "proxy", "another account keeps its own settings")
}

func TestBrowser_RemoteLogin_StartFinishCancelDriveTheAccountsBrowser(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	var startBody map[string]any
	signedIn := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.URL.Path)
		switch r.URL.Path {
		case "/sessions/instagram-7/login/start":
			startBody = body
			_, _ = w.Write([]byte(`{"account_id":"instagram-7","state":"new","login_url":"http://localhost:7070/login/instagram-7?ticket=t","expires_at":"2026-10-17T01:00:00Z","fingerprint":{"platform":"MacIntel"}}`))
		case "/sessions/instagram-7/login/confirm":
			if signedIn {
				_, _ = w.Write([]byte(`{"account_id":"instagram-7","state":"ok"}`))
				return
			}
			_, _ = w.Write([]byte(`{"account_id":"instagram-7","state":"new","reason":"login form is shown"}`))
		case "/sessions/instagram-7/login/cancel":
			_, _ = w.Write([]byte(`{"account_id":"instagram-7","state":"new"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("BROWSER_SERVICE_URL", srv.URL)
	resetSharedBrowser(t)

	login, ok := LookupRemoteLogin(PlatformInstagram)
	require.True(t, ok)
	acc := Account{ID: 7, Platform: PlatformInstagram, Kind: "browser_session", Proxy: "socks5://10.0.0.1:1080"}

	view, err := login.Start(context.Background(), nil, acc)
	require.NoError(t, err)
	assert.Equal(t, "/browser/login/instagram-7?ticket=t", view.ViewURL, "the window opens on the UI's own origin")
	assert.Equal(t, time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC), view.ExpiresAt.UTC())
	assert.JSONEq(t, `{"platform":"MacIntel"}`, string(view.Fingerprint))
	assert.Equal(t, "socks5://10.0.0.1:1080", startBody["proxy"])
	assert.Equal(t, "instagram", startBody["platform"])

	done, reason, err := login.Finish(context.Background(), nil, acc)
	require.NoError(t, err)
	assert.False(t, done)
	assert.Equal(t, "login form is shown", reason)

	mu.Lock()
	signedIn = true
	mu.Unlock()
	done, reason, err = login.Finish(context.Background(), nil, acc)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Empty(t, reason)

	require.NoError(t, login.Cancel(context.Background(), nil, acc))
	assert.Equal(t, []string{"/sessions/instagram-7/login/start", "/sessions/instagram-7/login/confirm", "/sessions/instagram-7/login/confirm", "/sessions/instagram-7/login/cancel"}, seen)

	_, ok = LookupRemoteLogin(PlatformFacebook)
	assert.True(t, ok)
}
