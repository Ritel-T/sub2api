package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type resourceAccounts struct{ AccountRepository }

func (*resourceAccounts) ListByPlatform(context.Context, string) ([]Account, error) {
	return []Account{{ID: 7, Name: "Mail Pro"}, {ID: 8, Name: "Hidden"}}, nil
}

type resourceProxies struct {
	ProxyRepository
	entries []Proxy
	err     error
}

func (r *resourceProxies) ListActive(context.Context) ([]Proxy, error) { return r.entries, r.err }
func (r *resourceProxies) GetByID(_ context.Context, id int64) (*Proxy, error) {
	for i := range r.entries {
		if r.entries[i].ID == id {
			return &r.entries[i], r.err
		}
	}
	return nil, r.err
}
func TestPluginResourceDirectoryCatalogAndResolution(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	proxies := &resourceProxies{entries: []Proxy{
		{ID: 3, Name: "Front", Protocol: "http", Host: "front.example", Port: 8080, Username: "private-user", Password: "private-password", Status: StatusActive},
		{ID: 4, Status: StatusActive, ExpiresAt: &past}, {ID: 5, Status: "inactive"},
	}}
	base := &fakeAccountDirectory{ids: []int64{7}}
	d := NewPluginResourceDirectory(base, &resourceAccounts{}, proxies)
	s := newPluginHostServiceServer("test.plugin", nil, d)
	out, err := s.ListResources(context.Background(), &pluginv1.ListResourcesRequest{})
	require.NoError(t, err)
	require.Len(t, out.Accounts, 1)
	require.Equal(t, "Mail Pro", out.Accounts[0].Name)
	require.Len(t, out.Proxies, 1)
	b, _ := json.Marshal(out)
	require.False(t, strings.Contains(string(b), "private-"))
	for _, id := range []int64{3, 4, 5, 999} {
		res, err := s.ResolveProxy(context.Background(), &pluginv1.ResolveProxyRequest{ProxyId: id})
		require.NoError(t, err)
		require.Equal(t, id == 3, res.Found)
		if id != 3 {
			require.Empty(t, res.ProxyUrl)
		}
	}
	proxies.entries[0].Password = "updated"
	res, err := s.ResolveProxy(context.Background(), &pluginv1.ResolveProxyRequest{ProxyId: 3})
	require.NoError(t, err)
	require.Contains(t, res.ProxyUrl, "updated")
	proxies.err = errors.New("private-password")
	_, err = s.ResolveProxy(context.Background(), &pluginv1.ResolveProxyRequest{ProxyId: 3})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-password")
}
func TestPluginResourceDirectoryCapabilityGateAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		d    PluginAccountDirectory
		code codes.Code
	}{{nil, codes.PermissionDenied}, {&fakeAccountDirectory{}, codes.Unimplemented}} {
		s := newPluginHostServiceServer("test.plugin", nil, tc.d)
		_, err := s.ListResources(context.Background(), &pluginv1.ListResourcesRequest{})
		require.Equal(t, tc.code, status.Code(err))
		_, err = s.ResolveProxy(context.Background(), &pluginv1.ResolveProxyRequest{ProxyId: 3})
		require.Equal(t, tc.code, status.Code(err))
	}
}
