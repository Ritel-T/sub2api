package repository

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http"
	"sort"
	"time"
)

func (s *astraRoutingUpstream) automaticTargetRoute(req *http.Request, proxy string, id int64, n int, profile *tlsfingerprint.Profile, pool *codexGatewayPinUpstream) (*http.Cookie, [32]byte, string, func(), error) {
	model := codexGatewayPinModel(req)
	if model == "" {
		if wireModel, known := service.GatewayBorrowWireModelFromContext(req.Context()); known {
			model = wireModel
		}
	}
	pool.targetMu.Lock()
	old, exists := pool.targetCheck(id, model)
	pool.targetMu.Unlock()
	expected := service.GatewayBorrowExpectedCookieFromContext(req.Context())
	if expected != "" {
		pool.targetMu.Lock()
		for k, check := range pool.targetRoutePasses {
			if k.accountID == id && k.model == model && check.route.cookie.Value == expected && time.Now().Before(check.expires) {
				old = check
				exists = true
				break
			}
		}
		pool.targetMu.Unlock()
	}
	force, _ := req.Context().Value(astraForceProbeKey{}).(bool)
	// An active user connection/continuation must retain its exact route until
	// expiry. Background forced warm is the only caller trying a replacement.
	if exists && old.passed && time.Now().Before(old.expires) && old.route.cookie.Value != "" && !force {
		selected := autoBorrowRouteSelection{old.sourceID, old.route}
		return s.targetRouteOnce(req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected)), proxy, id, n, profile)
	}
	if expected != "" {
		return nil, [32]byte{}, proxy, func() {}, errors.New("borrow_response_owner_route_expired")
	}
	if !s.hasAutomaticBorrowCandidate(req, proxy, id, model, profile, pool) {
		if err := s.PrepareAstraGateway(req.Context()); err != nil && err.Error() != "source_probe_cooldown" && err.Error() != "preparation_in_progress" {
			return nil, [32]byte{}, proxy, func() {}, err
		}
	}
	var last error
	attempts := 0
	for _, source := range pool.config.SourceAccountIDs {
		pool.mu.Lock()
		route, valid := pool.routes[source]
		pool.mu.Unlock()
		if !valid || route.cookie.Value == "" || !time.Now().Before(route.expires) {
			continue
		}
		if time.Until(route.expires) < 90*time.Second {
			continue
		}
		effectiveProxy := proxy
		if pool.config.IPAffinity {
			effectiveProxy = route.proxy
		}
		quotaKey, negativeKey := targetBorrowQuietKeys(req, id, model, effectiveProxy, source, profile)
		if prior, waiting := s.targetQuotaWait(quotaKey, time.Now()); waiting {
			last = errors.New(prior.reason)
			continue
		}
		if prior, waiting := s.targetQuotaWait(negativeKey, time.Now()); waiting {
			last = errors.New(prior.reason)
			continue
		}
		attempts++
		if attempts > 3 {
			break
		}
		selected := autoBorrowRouteSelection{source, route}
		cookie, key, egress, release, err := s.targetRouteOnce(req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected)), proxy, id, n, profile)
		if err == nil {
			return cookie, key, egress, release, nil
		}
		release()
		last = err
		if err.Error() == "target_probe_rate_limited" || err.Error() == "target_probe_auth_failed" {
			break
		}
		if req.Context().Err() != nil || s.current(req.Context()) != pool {
			break
		}
	}
	if last == nil {
		last = errCodexGatewayPinUnavailable
	}
	return nil, [32]byte{}, proxy, func() {}, last
}

func (s *astraRoutingUpstream) hasAutomaticBorrowCandidate(req *http.Request, proxy string, id int64, model string, profile *tlsfingerprint.Profile, pool *codexGatewayPinUpstream) bool {
	for _, source := range pool.config.SourceAccountIDs {
		pool.mu.Lock()
		route, exists := pool.routes[source]
		pool.mu.Unlock()
		if !exists || route.cookie.Value == "" || time.Until(route.expires) < 90*time.Second {
			continue
		}
		effectiveProxy := proxy
		if pool.config.IPAffinity {
			effectiveProxy = route.proxy
		}
		quota, negative := targetBorrowQuietKeys(req, id, model, effectiveProxy, source, profile)
		if _, waiting := s.targetQuotaWait(quota, time.Now()); waiting {
			continue
		}
		if _, waiting := s.targetQuotaWait(negative, time.Now()); waiting {
			continue
		}
		return true
	}
	return false
}

func (s *astraRoutingUpstream) prepareAutomaticSources(ctx context.Context, pool *codexGatewayPinUpstream) error {
	if s.preparer == nil {
		return errors.New("source_preparer_unavailable")
	}
	s.preparing.Store(true)
	defer s.preparing.Store(false)
	available := false
	attempts := 0
	// Keep renewals of verified routes ahead of exploration, but make never-
	// attempted/oldest donors fair independently of configuration order.
	ids := append([]int64(nil), pool.config.SourceAccountIDs...)
	checked := map[int64]time.Time{}
	verified := map[int64]bool{}
	pool.targetMu.Lock()
	for _, id := range pool.config.TargetAccountIDs {
		for _, model := range pool.config.ModelsForTarget(id) {
			if check, ok := pool.targetCheck(id, model); ok && check.passed && time.Now().Before(check.expires) {
				verified[check.sourceID] = true
			}
		}
	}
	pool.targetMu.Unlock()
	pool.mu.Lock()
	for _, id := range ids {
		if prior := pool.statuses[id]; prior.CheckedAt != nil {
			checked[id] = *prior.CheckedAt
		}
	}
	pool.mu.Unlock()
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		if verified[a] != verified[b] {
			return verified[a]
		}
		return checked[a].Before(checked[b])
	})
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.current(ctx) != pool {
			return errors.New("configuration_changed")
		}
		pool.mu.Lock()
		route, exists := pool.routes[id]
		prior := pool.statuses[id]
		pool.mu.Unlock()
		if exists && route.cookie.Value != "" && time.Now().Before(route.expires) {
			available = true
			if time.Until(route.expires) > 90*time.Second {
				continue
			}
		}
		if prior.CheckedAt != nil && time.Since(*prior.CheckedAt) < 20*time.Second {
			continue
		}
		attempts++
		if attempts > 3 {
			break
		}
		started := time.Now()
		if err := s.preparer(ctx, id); err != nil {
			pool.recordSourceReason(id, started, nil, false, "source_test_failed")
		}
		pool.mu.Lock()
		route, exists = pool.routes[id]
		pool.mu.Unlock()
		available = available || (exists && route.cookie.Value != "" && time.Now().Before(route.expires))
	}
	if !available {
		return errors.New("no_qualified_source_route")
	}
	return nil
}
