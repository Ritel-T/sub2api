// Package publicimagehttp pins untrusted image downloads to validated public IPs.
// It is deliberately separate from ordinary, operator-configured provider traffic.
package publicimagehttp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type transport struct {
	base     http.RoundTripper
	resolver resolver
}

// Wrap enforces the destination policy for every RoundTrip, including redirects.
// A nonstandard transport fails closed because its actual dial cannot be pinned.
func Wrap(base http.RoundTripper) http.RoundTripper {
	if _, ok := base.(*transport); ok {
		return base
	}
	if base == nil {
		base = &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	}
	return &transport{base: base, resolver: net.DefaultResolver}
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("image download URL is missing")
	}
	u := req.URL
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return nil, errors.New("image download requires an HTTP(S) URL without credentials")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, errors.New("image download port is invalid")
	}
	host := u.Hostname()
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") || strings.HasSuffix(strings.ToLower(strings.TrimSuffix(host, ".")), ".localhost") {
		return nil, errors.New("image download destination is not allowed")
	}
	ips, err := t.lookup(req.Context(), host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("image download DNS resolution failed")
	}
	for _, ip := range ips {
		if !isPublic(ip.IP) {
			return nil, errors.New("image download destination is not allowed")
		}
	}
	base, ok := t.base.(*http.Transport)
	//nolint:staticcheck // A legacy custom TLS dialer can bypass the destination pin, so it must also fail closed.
	if !ok || base.DialTLS != nil || base.DialTLSContext != nil {
		return nil, errors.New("image download transport cannot enforce destination policy")
	}
	tr := base.Clone()
	// No shared pool or HTTP/2 alternate transport may escape this request's pin.
	tr.DisableKeepAlives = true
	tr.ForceAttemptHTTP2 = false
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	tr.Protocols = new(http.Protocols)
	tr.Protocols.SetHTTP1(true)
	tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tr.TLSClientConfig.ServerName = host
	tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
	tr.TLSClientConfig.InsecureSkipVerify = false
	if tr.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		tr.TLSClientConfig.MinVersion = tls.VersionTLS12
	}
	if tr.DialContext == nil {
		tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	if tr.TLSHandshakeTimeout == 0 {
		tr.TLSHandshakeTimeout = 10 * time.Second
	}
	if tr.ResponseHeaderTimeout == 0 {
		tr.ResponseHeaderTimeout = 30 * time.Second
	}
	tr.MaxResponseHeaderBytes = 1 << 20
	clone := req.Clone(req.Context())
	clone.URL = new(url.URL)
	*clone.URL = *u
	clone.URL.Host = net.JoinHostPort(ips[0].IP.String(), port)
	clone.Host = u.Host
	// Forwarding plain HTTP through a proxy can replace the absolute target
	// with Request.Host. Use a CONNECT tunnel even for HTTP so the proxy always
	// receives the approved IP while the origin still receives its real Host.
	if tr.Proxy != nil {
		proxy, proxyErr := tr.Proxy(req)
		if proxyErr != nil {
			return nil, errors.New("image download proxy configuration failed")
		}
		tr.Proxy = http.ProxyURL(proxy)
		if proxy == nil {
			tr.Proxy = nil
		}
		if proxy != nil && (proxy.Scheme == "http" || proxy.Scheme == "https") {
			dial := tr.DialContext
			proxyTLS := tr.TLSClientConfig.Clone()
			proxyTLS.ServerName = proxy.Hostname()
			tr.Proxy = nil
			tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialProxyTunnel(ctx, dial, proxy, proxyTLS, network, addr)
			}
		}
	}
	resp, err := tr.RoundTrip(clone)
	if resp != nil {
		resp.Request = req
	}
	return resp, err
}

func (t *transport) lookup(ctx context.Context, host string) ([]net.IPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return t.resolver.LookupIPAddr(ctx, host)
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}
var publicIPv6 = netip.MustParsePrefix("2000::/3")

func isPublic(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is6() && !publicIPv6.Contains(addr) {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// dialProxyTunnel confines remote proxy resolution to the configured proxy host;
// the CONNECT authority is always an already-validated literal destination IP.
func dialProxyTunnel(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), proxy *url.URL, proxyTLS *tls.Config, network, target string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	port := proxy.Port()
	if port == "" {
		if proxy.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := dial(ctx, network, net.JoinHostPort(proxy.Hostname(), port))
	if err != nil {
		return nil, err
	}
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if proxy.Scheme == "https" {
		secured := tls.Client(conn, proxyTLS)
		if err := secured.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = secured
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)))
	}
	if err := request.Write(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(io.LimitReader(conn, 1<<20))
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = conn.Close()
		return nil, errors.New("image download proxy CONNECT failed")
	}
	if response.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, errors.New("image download proxy CONNECT rejected")
	}
	// A successful CONNECT has no HTTP response body. Preserve any bytes the
	// buffered parser already read, then remove the header-only size limit.
	buffered := make([]byte, reader.Buffered())
	_, _ = io.ReadFull(reader, buffered)
	_ = conn.SetDeadline(time.Time{})
	return &bufferedConn{Conn: conn, buffered: bytes.NewReader(buffered)}, nil
}

type bufferedConn struct {
	net.Conn
	buffered *bytes.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if c.buffered.Len() > 0 {
		return c.buffered.Read(p)
	}
	return c.Conn.Read(p)
}
