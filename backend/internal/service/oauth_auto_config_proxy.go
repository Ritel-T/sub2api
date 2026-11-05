package service

import (
	"context"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type oauthAccountProxySelector interface {
	LockAccountProxySelection(context.Context) error
	ListAccountProxySelection(context.Context, pagination.PaginationParams) ([]ProxyWithAccountCount, *pagination.PaginationResult, error)
}

func oauthProxyPublicIP(info *ProxyExitInfo) (netip.Addr, bool) {
	if info == nil {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(info.IP))
	if err != nil {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	return ip, ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified()
}

// Selection is part of account creation, so failure leaves no half-configured
// account. No proxy or existing account is edited while checking candidates.
func (s *adminServiceImpl) applyOAuthDedicatedProxy(ctx context.Context, input *CreateAccountInput, countries []string) error {
	if len(countries) == 0 {
		return nil
	}
	unavailable := func(message string) error {
		return infraerrors.ServiceUnavailable("AUTO_CONFIG_PROXY_UNAVAILABLE", message)
	}
	if dbent.TxFromContext(ctx) == nil {
		return unavailable("dedicated proxy selection requires an account transaction")
	}
	selector, ok := s.proxyRepo.(oauthAccountProxySelector)
	if !ok || s.proxyProber == nil {
		return unavailable("dedicated proxy selection is unavailable")
	}
	// Leave time for account persistence, ready coordination and the HTTP
	// response within the existing administrator client's 30-second timeout.
	selectionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := selector.LockAccountProxySelection(selectionCtx); err != nil {
		return unavailable("could not lock dedicated proxy selection")
	}
	var proxies []ProxyWithAccountCount
	for page := 1; ; page++ {
		items, meta, err := selector.ListAccountProxySelection(selectionCtx, pagination.PaginationParams{Page: page, PageSize: 100, SortBy: "id", SortOrder: "asc"})
		if err != nil {
			return unavailable("could not read dedicated proxy candidates")
		}
		proxies = append(proxies, items...)
		if meta == nil {
			return unavailable("dedicated proxy candidate pagination is unavailable")
		}
		if int64(len(proxies)) >= meta.Total {
			break
		}
		if len(items) == 0 {
			return unavailable("dedicated proxy candidate pagination is incomplete")
		}
	}
	// Country metadata narrows the candidate pool before its probe bound. It
	// never proves current eligibility: every selected exit is still tested.
	s.attachProxyLatency(selectionCtx, proxies)
	occupied := make([]ProxyWithAccountCount, 0)
	candidates := make([]ProxyWithAccountCount, 0)
	now := time.Now()
	for _, p := range proxies {
		if p.AccountCount > 0 {
			occupied = append(occupied, p)
		} else if p.IsActive() && !p.IsExpired(now) {
			country := strings.ToUpper(strings.TrimSpace(p.CountryCode))
			preferred := input.ProxyID != nil && p.ID == *input.ProxyID
			if slices.Contains(countries, country) || (country == "" && preferred) {
				candidates = append(candidates, p)
			}
		}
	}
	slices.SortFunc(candidates, func(a, b ProxyWithAccountCount) int {
		preferredA := input.ProxyID != nil && a.ID == *input.ProxyID
		preferredB := input.ProxyID != nil && b.ID == *input.ProxyID
		if preferredA != preferredB {
			if preferredA {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	usedIPs := make(map[netip.Addr]bool, len(occupied))
	// Each worker writes its own result slot. A failed occupied probe cancels
	// remaining work rather than assuming that an unknown exit is unique.
	observed := make([]netip.Addr, len(occupied))
	jobs := make(chan int, len(occupied))
	for i := range occupied {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	var failedID int64
	var failure sync.Once
	for range min(4, len(occupied)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if selectionCtx.Err() != nil {
					return
				}
				p := occupied[index]
				info, _, err := s.proxyProber.ProbeProxy(selectionCtx, p.URL())
				ip, valid := oauthProxyPublicIP(info)
				if err != nil || !valid {
					failure.Do(func() { failedID = p.ID; cancel() })
					return
				}
				observed[index] = ip
			}
		}()
	}
	wg.Wait()
	if failedID != 0 {
		return unavailable(fmt.Sprintf("cannot confirm occupied proxy exit: %d", failedID))
	}
	if selectionCtx.Err() != nil {
		return unavailable("dedicated proxy verification timed out or was cancelled")
	}
	for _, ip := range observed {
		usedIPs[ip] = true
	}
	for _, p := range candidates[:min(5, len(candidates))] {
		if selectionCtx.Err() != nil {
			return unavailable("dedicated proxy verification timed out or was cancelled")
		}
		info, _, err := s.proxyProber.ProbeProxy(selectionCtx, p.URL())
		ip, valid := oauthProxyPublicIP(info)
		if err != nil || !valid || usedIPs[ip] || !slices.Contains(countries, strings.ToUpper(strings.TrimSpace(info.CountryCode))) {
			continue
		}
		// Keep the existing accepted-name contract for timezone overrides.
		if info.Timezone != "" && !isAllowedOpenAIRequestTimezone(info.Timezone) {
			continue
		}
		id := p.ID
		input.ProxyID = &id
		if info.Timezone != "" {
			input.Extra = maps.Clone(input.Extra)
			if input.Extra == nil {
				input.Extra = map[string]any{}
			}
			input.Extra[openAIRequestTimezoneExtraKey] = info.Timezone
		}
		return nil
	}
	return unavailable("no healthy unused proxy with a unique exit in the configured countries")
}
