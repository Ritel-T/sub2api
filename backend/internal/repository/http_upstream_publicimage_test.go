package repository

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Exercise the real repository client, not just the URL preflight, so future
// refactors cannot accidentally detach PublicHostsOnly from the actual dial.
func TestHTTPUpstreamPublicImagePinsProxyTarget(t *testing.T) {
	target := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy method = %s", r.Method)
			w.WriteHeader(400)
			return
		}
		target <- r.Host
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("missing HTTP hijacker")
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		origin, err := http.ReadRequest(bufio.NewReader(rw))
		if err != nil {
			t.Error(err)
			return
		}
		_ = origin.Body.Close()
		if origin.Host != "8.8.8.8" {
			t.Errorf("origin Host = %q", origin.Host)
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 3\r\nConnection: close\r\n\r\nimg")
	}))
	defer proxy.Close()
	req, err := http.NewRequestWithContext(service.WithHTTPUpstreamPublicHostsOnly(t.Context()), http.MethodGet, "http://8.8.8.8/a.png", nil)
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, proxy.URL, 1, 1)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "img", string(body))
	require.Equal(t, "8.8.8.8:80", <-target)
}
