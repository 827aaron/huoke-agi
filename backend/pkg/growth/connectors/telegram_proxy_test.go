package connectors

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listen(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(c)
		}
	}()
	return ln.Addr().String()
}

func echoServer(t *testing.T) string {
	return listen(t, func(c net.Conn) {
		defer c.Close()
		_, _ = io.Copy(c, c)
	})
}

func pipe(a, b net.Conn) {
	go func() { _, _ = io.Copy(a, b); a.Close() }()
	_, _ = io.Copy(b, a)
	b.Close()
}

type proxySeen struct {
	target string
	auth   string
}

func httpConnectProxy(t *testing.T, seen chan<- proxySeen) string {
	return listen(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil {
			c.Close()
			return
		}
		seen <- proxySeen{target: req.Host, auth: req.Header.Get("Proxy-Authorization")}
		up, err := net.Dial("tcp", req.Host)
		if err != nil {
			_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			c.Close()
			return
		}
		// The greeting rides in the same write as the status line, so the dialer
		// must hand back bytes its reader already buffered.
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\nhello|")
		pipe(c, up)
	})
}

func socks5Proxy(t *testing.T, seen chan<- proxySeen) string {
	return listen(t, func(c net.Conn) {
		defer c.Close()
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(c, hdr); err != nil {
			return
		}
		methods := make([]byte, hdr[1])
		_, _ = io.ReadFull(c, methods)
		_, _ = c.Write([]byte{5, 2})
		// RFC 1929 username/password sub-negotiation.
		_, _ = io.ReadFull(c, hdr[:1])
		ulen := make([]byte, 1)
		_, _ = io.ReadFull(c, ulen)
		user := make([]byte, ulen[0])
		_, _ = io.ReadFull(c, user)
		plen := make([]byte, 1)
		_, _ = io.ReadFull(c, plen)
		pass := make([]byte, plen[0])
		_, _ = io.ReadFull(c, pass)
		_, _ = c.Write([]byte{1, 0})

		req := make([]byte, 4)
		_, _ = io.ReadFull(c, req)
		var host string
		switch req[3] {
		case 1:
			ip := make([]byte, 4)
			_, _ = io.ReadFull(c, ip)
			host = net.IP(ip).String()
		case 3:
			n := make([]byte, 1)
			_, _ = io.ReadFull(c, n)
			name := make([]byte, n[0])
			_, _ = io.ReadFull(c, name)
			host = string(name)
		}
		port := make([]byte, 2)
		_, _ = io.ReadFull(c, port)
		target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port))))
		seen <- proxySeen{target: target, auth: string(user) + ":" + string(pass)}
		up, err := net.Dial("tcp", target)
		if err != nil {
			_, _ = c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		pipe(c, up)
	})
}

func roundTrip(t *testing.T, conn net.Conn, greeting string) {
	t.Helper()
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))
	_, err := conn.Write([]byte("ping"))
	require.NoError(t, err)
	got := make([]byte, len(greeting)+4)
	_, err = io.ReadFull(conn, got)
	require.NoError(t, err)
	assert.Equal(t, greeting+"ping", string(got))
}

func receive(t *testing.T, seen <-chan proxySeen) proxySeen {
	t.Helper()
	select {
	case s := <-seen:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("the proxy never saw a connection")
		return proxySeen{}
	}
}

func TestTelegramProxy_ProxyDialer_HTTPConnect(t *testing.T) {
	target := echoServer(t)
	seen := make(chan proxySeen, 1)
	proxyAddr := httpConnectProxy(t, seen)

	dial, err := proxyDialer("http://alice:s3cret@" + proxyAddr)
	require.NoError(t, err)
	conn, err := dial(context.Background(), "tcp", target)
	require.NoError(t, err)
	roundTrip(t, conn, "hello|")

	s := receive(t, seen)
	assert.Equal(t, target, s.target)
	assert.Equal(t, "Basic YWxpY2U6czNjcmV0", s.auth)
}

func TestTelegramProxy_ProxyDialer_HTTPConnectRefused(t *testing.T) {
	proxyAddr := listen(t, func(c net.Conn) {
		defer c.Close()
		_, _ = http.ReadRequest(bufio.NewReader(c))
		_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
	})
	dial, err := proxyDialer("http://" + proxyAddr)
	require.NoError(t, err)
	_, err = dial(context.Background(), "tcp", "149.154.167.50:443")
	assert.ErrorContains(t, err, "407")
}

func TestTelegramProxy_ProxyDialer_SOCKS5(t *testing.T) {
	target := echoServer(t)
	seen := make(chan proxySeen, 1)
	proxyAddr := socks5Proxy(t, seen)

	dial, err := proxyDialer("socks5://bob:pw@" + proxyAddr)
	require.NoError(t, err)
	conn, err := dial(context.Background(), "tcp", target)
	require.NoError(t, err)
	roundTrip(t, conn, "")

	s := receive(t, seen)
	assert.Equal(t, target, s.target)
	assert.Equal(t, "bob:pw", s.auth)
}

func TestTelegramProxy_ProxyDialer_Rejects(t *testing.T) {
	for _, raw := range []string{"ftp://host:21", "http://", "::bad"} {
		_, err := proxyDialer(raw)
		assert.Error(t, err, raw)
	}
	_, err := proxyDialer("ftp://user:topsecret@host:21")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret")

	direct, err := proxyDialer("")
	require.NoError(t, err)
	assert.NotNil(t, direct)
}
