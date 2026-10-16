package connectors

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gotd/td/telegram/dcs"
	"golang.org/x/net/proxy"
)

// Account is one platform login the connectors act as. Each account gets its
// own client, so its session, pacing and proxy never mix with another's.
type Account struct {
	ID       string   `json:"id"`
	Platform Platform `json:"platform"`
	// Proxy is an http://, https://, socks5:// or socks5h:// URL with optional
	// user:pass@; empty dials Telegram directly.
	Proxy string `json:"proxy,omitempty"`
}

var accountFileRe = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// ForAccount derives the account's config: its proxy, and a session file of
// its own next to cfg.SessionPath (the join warm-up state follows it).
func (c TelegramConfig) ForAccount(a Account) TelegramConfig {
	c.Proxy = a.Proxy
	if a.ID != "" {
		dir := filepath.Dir(c.SessionPath)
		c.SessionPath = filepath.Join(dir, "account-"+accountFileRe.ReplaceAllString(a.ID, "_")+".session.json")
	}
	return c
}

func proxyDialer(raw string) (dcs.DialFunc, error) {
	if raw == "" {
		var d net.Dialer
		return d.DialContext, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL %q", redactProxy(raw))
	}
	switch u.Scheme {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pass}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{})
		if err != nil {
			return nil, fmt.Errorf("socks5 proxy: %w", err)
		}
		cd, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("socks5 dialer does not support contexts")
		}
		return cd.DialContext, nil
	case "http", "https":
		return httpConnectDialer(u), nil
	}
	return nil, fmt.Errorf("unsupported proxy scheme %q (want http, https, socks5)", u.Scheme)
}

// redactProxy keeps credentials out of errors and logs.
func redactProxy(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	return u.Redacted()
}

func httpConnectDialer(u *url.URL) dcs.DialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", u.Host)
		if err != nil {
			return nil, fmt.Errorf("dial proxy: %w", err)
		}
		if u.Scheme == "https" {
			tc := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
			if err := tc.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, fmt.Errorf("proxy tls: %w", err)
			}
			conn = tc
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}

		req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
		if u.User != nil {
			pass, _ := u.User.Password()
			req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pass)))
		}
		if err := req.Write(conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT: %w", err)
		}
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, req)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT to %s: %s", addr, resp.Status)
		}
		_ = conn.SetDeadline(time.Time{})
		// The reader may already hold the first tunnelled bytes.
		return &bufferedConn{Conn: conn, r: br}, nil
	}
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
