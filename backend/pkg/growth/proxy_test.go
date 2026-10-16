package growth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pentagi/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxy_ParseProxyURL_AcceptsOnlyARoutableProxy(t *testing.T) {
	for _, ok := range []string{
		"http://10.0.0.1:3128", "https://proxy.example.test:443", "socks5://user:pa%40ss@1.2.3.4:1080", "socks5h://h:1",
	} {
		_, err := parseProxyURL(ok)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "10.0.0.1:3128", "ftp://h:21", "http://h", "socks4://h:1080", "http://:8080"} {
		_, err := parseProxyURL(bad)
		assert.ErrorIs(t, err, ErrInvalidSetting, bad)
	}
}

func TestProxy_MaskEndpoint_HidesCredentialsAndTokens(t *testing.T) {
	cases := map[string]string{
		"socks5://user:secret@1.2.3.4:1080":                      "socks5://••••@1.2.3.4:1080",
		"http://10.0.0.1:3128":                                   "http://10.0.0.1:3128",
		"ws://127.0.0.1:9222/devtools/browser/0f3c-token-a1b2c3": "ws://127.0.0.1:9222/••••",
		"":                                                       "",
		"not a url":                                              "••••",
	}
	for in, want := range cases {
		assert.Equal(t, want, maskEndpoint(in), in)
	}
}

func TestProxy_CheckProxy_GoesThroughTheProxy(t *testing.T) {
	var seen string
	fwd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.String()
		if r.Header.Get("Proxy-Authorization") == "" {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(fwd.Close)
	prev := proxyCheckURL
	proxyCheckURL = "http://only-the-proxy-resolves-this.invalid/"
	t.Cleanup(func() { proxyCheckURL = prev })

	withCreds := strings.Replace(fwd.URL, "http://", "http://u:p@", 1)
	latency, err := checkProxy(context.Background(), &config.Config{}, withCreds)
	require.NoError(t, err)
	assert.Positive(t, latency)
	assert.Equal(t, "http://only-the-proxy-resolves-this.invalid/", seen)

	_, err = checkProxy(context.Background(), &config.Config{}, fwd.URL)
	assert.ErrorContains(t, err, "407")
}
