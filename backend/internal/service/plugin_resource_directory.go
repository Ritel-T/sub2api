package service

import (
	"context"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Resource access follows the same capability gate as the OAuth account directory.
// The catalog never carries tokens, usernames, passwords or authenticated URLs.
type PluginResourceDirectory interface {
	ListPluginResources(context.Context) (*pluginv1.ListResourcesResponse, error)
	ResolvePluginProxy(context.Context, int64) (string, error)
}
type pluginResourceDirectory struct {
	PluginAccountDirectory
	accounts AccountRepository
	proxies  ProxyRepository
}

func NewPluginResourceDirectory(base PluginAccountDirectory, accounts AccountRepository, proxies ProxyRepository) PluginAccountDirectory {
	return &pluginResourceDirectory{base, accounts, proxies}
}
func (d *pluginResourceDirectory) ListPluginResources(ctx context.Context) (*pluginv1.ListResourcesResponse, error) {
	ids, err := d.ListPluginAccounts(ctx, PlatformOpenAI, AccountTypeOAuth)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int64]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	accounts, err := d.accounts.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}
	out := &pluginv1.ListResourcesResponse{ActionsSupported: true}
	for _, a := range accounts {
		if allowed[a.ID] {
			out.Accounts = append(out.Accounts, &pluginv1.AccountSummary{Id: a.ID, Name: a.Name})
		}
	}
	proxies, err := d.proxies.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range proxies {
		if p.IsActive() && !p.IsExpired(time.Now()) {
			out.Proxies = append(out.Proxies, &pluginv1.ProxySummary{Id: p.ID, Name: p.Name, Protocol: p.Protocol, Host: p.Host, Port: int32(p.Port)})
		}
	}
	return out, nil
}
func (d *pluginResourceDirectory) ResolvePluginProxy(ctx context.Context, id int64) (string, error) {
	p, err := d.proxies.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if p == nil || !p.IsActive() || p.IsExpired(time.Now()) {
		return "", nil
	}
	return p.URL(), nil
}
func (s *pluginHostServiceServer) ListResources(ctx context.Context, req *pluginv1.ListResourcesRequest) (*pluginv1.ListResourcesResponse, error) {
	if s == nil || s.directory == nil {
		return nil, status.Error(codes.PermissionDenied, "resource directory unavailable")
	}
	d, ok := s.directory.(PluginResourceDirectory)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "resource directory not supported")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	out, err := d.ListPluginResources(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "resource directory unavailable")
	}
	return out, nil
}
func (s *pluginHostServiceServer) ResolveProxy(ctx context.Context, req *pluginv1.ResolveProxyRequest) (*pluginv1.ResolveProxyResponse, error) {
	if s == nil || s.directory == nil {
		return nil, status.Error(codes.PermissionDenied, "proxy directory unavailable")
	}
	d, ok := s.directory.(PluginResourceDirectory)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "proxy directory not supported")
	}
	if req == nil || req.ProxyId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid proxy id")
	}
	u, err := d.ResolvePluginProxy(ctx, req.ProxyId)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "selected proxy unavailable")
	}
	return &pluginv1.ResolveProxyResponse{Found: u != "", ProxyUrl: u}, nil
}
