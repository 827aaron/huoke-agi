package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserView_RegisterBrowserView_PassesOnlyTheTicketRoutesWithoutTheOperatorsCredentials(t *testing.T) {
	type seenRequest struct{ method, uri, auth, cookie, body string }
	var (
		mu   sync.Mutex
		seen []seenRequest
	)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, seenRequest{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Cookie"), string(body)})
		mu.Unlock()
		_, _ = w.Write([]byte("window"))
	}))
	t.Cleanup(service.Close)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NoError(t, registerBrowserView(engine, service.URL))
	// A recorder lacks the CloseNotifier gin hands the reverse proxy, so the UI side is a server too.
	ui := httptest.NewServer(engine)
	t.Cleanup(ui.Close)
	do := func(req *http.Request) (int, string) {
		t.Helper()
		res, err := ui.Client().Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}

	get, _ := http.NewRequest(http.MethodGet, ui.URL+"/browser/login/facebook-3?ticket=t1", nil)
	get.Header.Set("Cookie", "auth=operator-session")
	get.Header.Set("Authorization", "Bearer operator-token")
	status, body := do(get)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "window", body)

	post, _ := http.NewRequest(http.MethodPost, ui.URL+"/browser/sessions/facebook-3/input?ticket=t1", strings.NewReader(`{"type":"click"}`))
	do(post)

	for _, path := range []string{"/browser/healthz", "/browser/", "/browser/sessions"} {
		req, _ := http.NewRequest(http.MethodGet, ui.URL+path, nil)
		status, _ := do(req)
		assert.Equal(t, http.StatusNotFound, status, path)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []seenRequest{
		{method: "GET", uri: "/login/facebook-3?ticket=t1"},
		{method: "POST", uri: "/sessions/facebook-3/input?ticket=t1", body: `{"type":"click"}`},
	}, seen)
}

func TestBrowserView_RegisterBrowserView_RefusesARelativeServiceURL(t *testing.T) {
	err := registerBrowserView(gin.New(), "browser:7070")
	require.ErrorContains(t, err, "is not an absolute URL")
}
