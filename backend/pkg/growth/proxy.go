package growth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/system"

	"golang.org/x/net/proxy"
)

var (
	proxySchemes    = []string{"http", "https", "socks5", "socks5h"}
	endpointSchemes = []string{"ws", "wss", "http", "https"}

	// proxyCheckURL is what a proxy test fetches: any HTTP answer proves the route works.
	proxyCheckURL = "https://api.telegram.org"
)

// ProxyCheck is the outcome of reaching proxyCheckURL through a proxy.
type ProxyCheck struct {
	OK        bool   `json:"ok"`
	Proxy     string `json:"proxy,omitempty"`
	Source    string `json:"source"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

func parseProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !slices.Contains(proxySchemes, strings.ToLower(u.Scheme)) || u.Hostname() == "" || u.Port() == "" {
		return nil, fmt.Errorf("%w: a proxy is scheme://[user:pass@]host:port with scheme %s",
			ErrInvalidSetting, strings.Join(proxySchemes, ", "))
	}
	return u, nil
}

func parseEndpointURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !slices.Contains(endpointSchemes, strings.ToLower(u.Scheme)) || u.Host == "" {
		return nil, fmt.Errorf("%w: external_browser_ws must be a ws://, wss:// or http(s):// DevTools endpoint", ErrInvalidSetting)
	}
	return u, nil
}

// maskEndpoint keeps scheme, host and port: the credentials of a proxy and the path
// of a DevTools endpoint (its session token) are secrets.
func maskEndpoint(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "••••"
	}
	masked := u.Scheme + "://"
	if u.User != nil {
		masked += "••••@"
	}
	masked += u.Host
	if u.Path != "" && u.Path != "/" {
		masked += "/••••"
	}
	return masked
}

// proxyClient builds an HTTP client that reaches the internet only through raw.
func proxyClient(cfg *config.Config, raw string) (*http.Client, error) {
	u, err := parseProxyURL(raw)
	if err != nil {
		return nil, err
	}
	base, err := system.GetHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{}
	if t, ok := base.Transport.(*http.Transport); ok {
		transport.TLSClientConfig = t.TLSClientConfig
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(u)
	default:
		dialer, err := proxy.FromURL(u, &net.Dialer{Timeout: 15 * time.Second})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSetting, err)
		}
		ctxDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("%w: the %s dialer cannot take a context", ErrInvalidSetting, u.Scheme)
		}
		transport.DialContext = ctxDialer.DialContext
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}, nil
}

func checkProxy(ctx context.Context, cfg *config.Config, raw string) (time.Duration, error) {
	client, err := proxyClient(cfg, raw)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyCheckURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot reach %s through the proxy: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	// A proxy refusing the tunnel or asking for credentials answers 407 itself.
	if resp.StatusCode == http.StatusProxyAuthRequired {
		return 0, fmt.Errorf("the proxy rejected its credentials (407)")
	}
	return time.Since(start), nil
}
