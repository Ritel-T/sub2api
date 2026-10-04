package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/mihomo"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type astraTargetValidation struct {
	route                        codexGatewayRoute
	model                        string
	node                         mihomo.AstraNode
	key                          [32]byte
	cookieFingerprint            [32]byte
	proxyFingerprint             [32]byte
	passed                       bool
	checked, expires, retryAfter time.Time
	sourceID                     int64
	reason                       string
	answer                       string
	gateway                      string
}

type astraTargetModelKey struct {
	accountID int64
	model     string
}

type astraTargetRouteKey struct {
	accountID int64
	model     string
	sourceID  int64
	cookie    [32]byte
}
type autoBorrowRouteKey struct{}
type autoBorrowRouteSelection struct {
	sourceID int64
	route    codexGatewayRoute
}

// Call under targetMu. Keep the legacy Astra map for backwards-compatible
// diagnostics/tests, while every other model has its own independent witness.
func (p *codexGatewayPinUpstream) targetCheck(id int64, model string) (astraTargetValidation, bool) {
	if model == "gpt-6-astra" {
		check, exists := p.targetChecks[id]
		return check, exists
	}
	check, exists := p.targetModelChecks[astraTargetModelKey{id, model}]
	return check, exists
}

func (p *codexGatewayPinUpstream) setTargetCheck(id int64, model string, check astraTargetValidation) {
	check.model = model
	if model == "gpt-6-astra" {
		if p.targetChecks == nil {
			p.targetChecks = map[int64]astraTargetValidation{}
		}
		p.targetChecks[id] = check
		return
	}
	if p.targetModelChecks == nil {
		p.targetModelChecks = map[astraTargetModelKey]astraTargetValidation{}
	}
	p.targetModelChecks[astraTargetModelKey{id, model}] = check
}

func (p *codexGatewayPinUpstream) targetCheckForCookie(id int64, model, cookie string) (astraTargetValidation, bool) {
	check, exists := p.targetCheck(id, model)
	fp := sha256.Sum256([]byte(cookie))
	if exists && check.cookieFingerprint == fp {
		return check, true
	}
	for k, v := range p.targetRoutePasses {
		if k.accountID == id && k.model == model && v.cookieFingerprint == fp {
			return v, true
		}
	}
	return astraTargetValidation{}, false
}

// Source success is only a candidate. Validate the borrowed route with the
// existing two-shot state probe and the target credentials on the actual exit.
func (s *astraRoutingUpstream) targetRouteOnce(req *http.Request, proxy string, id int64, n int, profile *tlsfingerprint.Profile) (resultCookie *http.Cookie, resultKey [32]byte, resultProxy string, release func(), resultErr error) {
	release = func() {}
	defer func() {
		if resultErr != nil {
			release()
			release = func() {}
		}
	}()
	var zero [32]byte
	model := codexGatewayPinModel(req)
	if model == "" {
		if wire, known := service.GatewayBorrowWireModelFromContext(req.Context()); known {
			model = wire
		}
	}
	if model == "" {
		return nil, zero, proxy, release, errors.New("borrow_model_required")
	}
	pool := s.current(req.Context())
	if pool == nil {
		return nil, zero, proxy, release, errCodexGatewayPinUnavailable
	}
	if !pool.config.TargetRequiresModel(id, model) {
		return nil, zero, proxy, release, errors.New("borrow_target_model_not_configured")
	}
	cookie, sourceID := pool.currentCookie(req.URL.Path, time.Now())
	selected, hasSelection := req.Context().Value(autoBorrowRouteKey{}).(autoBorrowRouteSelection)
	if hasSelection {
		sourceID = selected.sourceID
		copyCookie := selected.route.cookie
		cookie = &copyCookie
	}
	if cookie == nil {
		if err := s.PrepareAstraGateway(req.Context()); err != nil {
			return nil, zero, proxy, release, err
		}
		pool = s.current(req.Context())
		if pool == nil {
			return nil, zero, proxy, release, errCodexGatewayPinUnavailable
		}
		cookie, sourceID = pool.currentCookie(req.URL.Path, time.Now())
	}
	if cookie == nil {
		return nil, zero, proxy, release, errCodexGatewayPinUnavailable
	}
	pool.mu.Lock()
	route := pool.routes[sourceID]
	if hasSelection {
		route = selected.route
	}
	expires := route.expires
	nodeStore := pool.nodeStore
	pool.mu.Unlock()
	if route.cookie.Value != cookie.Value || !time.Now().Before(expires) {
		return nil, zero, proxy, release, errors.New("configuration_changed")
	}
	if pool.config.RotateNodes && pool.nodeCoolingForModel(route.node, astraRoutingHost(cookie.Value), id, model, time.Now()) {
		pool.targetMu.Lock()
		if pool.targetChecks == nil {
			pool.targetChecks = map[int64]astraTargetValidation{}
		}
		pool.setTargetCheck(id, model, astraTargetValidation{node: route.node, sourceID: sourceID, cookieFingerprint: sha256.Sum256([]byte(cookie.Value)), gateway: astraRoutingHost(cookie.Value), reason: "astra_rotation_cooling", checked: time.Now()})
		pool.targetMu.Unlock()
		return nil, zero, proxy, release, errors.New("astra_rotation_cooling")
	}
	if pool.config.IPAffinity {
		proxy = route.proxy
	}
	identityProxy := proxy
	if pool.config.RotateNodes {
		if route.node.ID == "" {
			return nil, zero, proxy, release, errors.New("astra_rotation_node_unavailable")
		}
		var err error
		proxy, release, err = s.acquireNode(req.Context(), route.node)
		if err != nil {
			return nil, zero, proxy, release, err
		}
		identityProxy = route.node.Identity
		req = astraFreshRequest(req)
	}
	identity := []string{model, cookie.Value, identityProxy, req.Header.Get("Authorization"), req.Header.Get("ChatGPT-Account-ID"), req.Header.Get("User-Agent"), req.Header.Get("Originator"), req.Header.Get("Version"), req.Header.Get("X-Codex-Turn-State")}
	fingerprint, _ := json.Marshal(profile)
	identity = append(identity, string(fingerprint))
	key := sha256.Sum256([]byte(strings.Join(identity, "\x00")))
	now := time.Now()
	pool.targetMu.Lock()
	old, exists := pool.targetCheck(id, model)
	if hasSelection && old.key != key {
		if prior, found := pool.targetRoutePasses[astraTargetRouteKey{id, model, sourceID, sha256.Sum256([]byte(cookie.Value))}]; found && prior.key == key {
			old = prior
			exists = true
		}
	}
	if s.cfg.AstraRouting(req.Context()).AutoQuality {
		failure, failed := pool.targetRouteFailures[astraTargetRouteKey{id, model, sourceID, sha256.Sum256([]byte(cookie.Value))}]
		if failed && failure.key == key && now.Before(failure.retryAfter) {
			pool.targetMu.Unlock()
			return nil, key, proxy, release, errors.New(failure.reason)
		}
	}
	pool.targetMu.Unlock()
	force, _ := req.Context().Value(astraForceProbeKey{}).(bool)
	if exists && old.key == key && now.Before(old.expires) && !force {
		if old.passed {
			current, currentSource := pool.currentCookie(req.URL.Path, time.Now())
			if hasSelection {
				current = cookie
				currentSource = sourceID
			}
			if s.current(req.Context()) != pool || current == nil || current.Value != cookie.Value || currentSource != sourceID {
				return nil, key, proxy, release, errors.New("configuration_changed")
			}
			return cookie, key, proxy, release, nil
		}
		if now.Before(old.retryAfter) {
			return nil, key, proxy, release, errors.New(old.reason)
		}
	}
	if s.cfg.AstraRouting(req.Context()).AutoQuality {
		pool.targetMu.Lock()
		if pool.autoProbeSlots == nil {
			pool.autoProbeSlots = make(chan struct{}, 2)
			pool.autoProbeLocks = map[astraTargetModelKey]*sync.Mutex{}
		}
		lock := pool.autoProbeLocks[astraTargetModelKey{id, model}]
		if lock == nil {
			lock = &sync.Mutex{}
			pool.autoProbeLocks[astraTargetModelKey{id, model}] = lock
		}
		pool.targetMu.Unlock()
		if !lock.TryLock() {
			return nil, key, proxy, release, errors.New("target_validation_in_progress")
		}
		defer lock.Unlock()
		select {
		case pool.autoProbeSlots <- struct{}{}:
			defer func() { <-pool.autoProbeSlots }()
		default:
			return nil, key, proxy, release, errors.New("target_validation_in_progress")
		}
	} else {
		if !pool.targetProbeMu.TryLock() {
			return nil, key, proxy, release, errors.New("target_validation_in_progress")
		}
		defer pool.targetProbeMu.Unlock()
	}

	ctx, cancel := context.WithTimeout(req.Context(), 90*time.Second)
	defer cancel()
	probe := req.Clone(service.WithHTTPUpstreamRedirectsDisabled(ctx))
	replaceCodexGatewayCookie(probe, cookie)
	result := service.ProbeOpenAICodexBorrowQualityRoute(ctx, s.delegate, probe, proxy, id, n, profile, model)
	passed := result.Verdict == service.OpenAICodexStateHealthy
	if passed && !time.Now().Before(expires) {
		passed = false
		result.Failure = "route_changed"
	}
	reason := "target_probe_failed"
	if result.Verdict == service.OpenAICodexStateDegraded {
		reason = "target_probe_degraded"
	}
	if result.Failure == "route_changed" {
		reason = "target_route_changed"
	}
	if result.Failure == "quality_failed" {
		reason = "target_quality_failed"
	}

	if s.current(req.Context()) != pool {
		return nil, key, proxy, release, errors.New("configuration_changed")
	}
	current, currentID := pool.currentCookie(req.URL.Path, time.Now())
	if hasSelection {
		current = cookie
		currentID = sourceID
	}
	if current == nil || current.Value != cookie.Value || currentID != sourceID {
		return nil, key, proxy, release, errors.New("configuration_changed")
	}
	pool.mu.Lock()
	currentRoute := pool.routes[sourceID]
	if hasSelection {
		currentRoute = route
	}
	pool.mu.Unlock()
	if (pool.config.IPAffinity && !pool.config.RotateNodes && currentRoute.proxy != route.proxy) || currentRoute.node.Identity != route.node.Identity {
		return nil, key, proxy, release, errors.New("configuration_changed")
	}
	if !time.Now().Before(route.expires) {
		return nil, key, proxy, release, errors.New("borrow_route_expired")
	}
	pool.targetMu.Lock()
	defer func() {
		pool.targetMu.Unlock()
		if pool.historyRecorder != nil {
			pool.historyRecorder(service.AstraGatewayHistoryRecord{Gateway: astraRoutingHost(cookie.Value), SourceAccountID: sourceID, TargetAccountID: id, LastSeen: time.Now(), LastReason: reason}, passed)
		}
	}()
	if pool.targetChecks == nil {
		pool.targetChecks = make(map[int64]astraTargetValidation)
	}
	if passed {
		reason = "target_probe_passed"
	}
	host := astraRoutingHost(cookie.Value)
	retryDelay := 15 * time.Second
	if result.Verdict == service.OpenAICodexStateDegraded || result.Failure == "quality_failed" {
		retryDelay = 180 * time.Second
	}
	if result.Failure == service.OpenAICodexStateFailureRateLimited || result.Failure == service.OpenAICodexStateFailureAccountError {
		retryDelay = 5 * time.Minute
		if result.Failure == service.OpenAICodexStateFailureRateLimited {
			reason = "target_probe_rate_limited"
		} else {
			reason = "target_probe_auth_failed"
		}
	}
	check := astraTargetValidation{node: route.node, key: key, cookieFingerprint: sha256.Sum256([]byte(cookie.Value)), proxyFingerprint: sha256.Sum256([]byte(proxy)), passed: passed, checked: time.Now(), expires: expires, retryAfter: time.Now().Add(retryDelay), sourceID: sourceID, reason: reason, gateway: host}
	check.route = route
	if !passed && s.cfg.AstraRouting(req.Context()).AutoQuality {
		if pool.targetRouteFailures == nil {
			pool.targetRouteFailures = map[astraTargetRouteKey]astraTargetValidation{}
		}
		for k, v := range pool.targetRouteFailures {
			if !time.Now().Before(v.retryAfter) || !time.Now().Before(v.expires) {
				delete(pool.targetRouteFailures, k)
			}
		}
		pool.targetRouteFailures[astraTargetRouteKey{id, model, sourceID, check.cookieFingerprint}] = check
		if !exists || !old.passed || !time.Now().Before(old.expires) {
			pool.setTargetCheck(id, model, check)
		}
	} else {
		pool.setTargetCheck(id, model, check)
		if passed && s.cfg.AstraRouting(req.Context()).AutoQuality {
			if pool.targetRoutePasses == nil {
				pool.targetRoutePasses = map[astraTargetRouteKey]astraTargetValidation{}
			}
			for k, v := range pool.targetRoutePasses {
				if !time.Now().Before(v.expires) {
					delete(pool.targetRoutePasses, k)
				}
			}
			pool.targetRoutePasses[astraTargetRouteKey{id, model, sourceID, check.cookieFingerprint}] = check
		}
	}
	pool.mu.Lock()
	pool.observeGatewayTarget(host, passed)
	pool.mu.Unlock()
	if nodeStore != nil {
		nodeStore.MarkTarget(cookie.Value, id, passed, reason, time.Now(), expires, time.Now().Add(15*time.Second))
	}
	if !passed {
		check.model = model
		pool.rememberTargetFailure(id, check)
		return nil, key, proxy, release, errors.New(reason)
	}
	return cookie, key, proxy, release, nil
}

// Manual verification always refreshes the evidence instead of accepting a cached pass.
type astraForceProbeKey struct{}

func (s *astraRoutingUpstream) VerifyAstraGatewayTarget(ctx context.Context, req *http.Request, proxy string, id int64, n int) error {
	return s.VerifyAstraGatewayTargetForModel(ctx, req, proxy, id, n, "gpt-6-astra")
}

func (s *astraRoutingUpstream) VerifyAstraGatewayTargetForModel(ctx context.Context, req *http.Request, proxy string, id int64, n int, model string) error {
	model = config.CanonicalGatewayBorrowModel(model)
	if model == "" {
		return errors.New("borrow_model_required")
	}
	req = req.Clone(context.WithValue(ctx, astraForceProbeKey{}, true))
	body, _ := json.Marshal(map[string]string{"model": model})
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	_, _, _, release, err := s.targetRoute(req, proxy, id, n, nil)
	release()
	return err
}
func (s *astraRoutingUpstream) CodexGatewayPinWSRequest(ctx context.Context, headers http.Header, proxy string, id int64, n int) (string, string, func(), error) {
	return s.CodexGatewayPinWSRequestForModel(ctx, headers, proxy, id, n, "gpt-6-astra")
}

func (s *astraRoutingUpstream) CodexGatewayPinWSRequestForModel(ctx context.Context, headers http.Header, proxy string, id int64, n int, model string) (string, string, func(), error) {
	model = config.CanonicalGatewayBorrowModel(model)
	pool := s.current(ctx)
	if pool == nil {
		if s.cfg.AstraRouting(ctx).CookiePool.TargetRequiresModel(id, model) {
			return "", proxy, func() {}, errCodexGatewayPinUnavailable
		}
		return "", proxy, func() {}, nil
	}
	if !pool.config.TargetRequiresModel(id, model) {
		return "", proxy, func() {}, nil
	}
	body, _ := json.Marshal(map[string]string{"model": model})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(body))
	req.Header = headers.Clone()
	cookie, _, proxy, release, err := s.targetRoute(req, proxy, id, n, nil)
	if err != nil || cookie == nil {
		return "", proxy, release, err
	}
	return cookie.Value, proxy, release, nil
}

// Pure witness check for an already-bound WS connection; never refreshes a
// route or probes. A settings revision creates a new pool and invalidates it.
func (s *astraRoutingUpstream) CodexGatewayPinWSBindingValidForModel(ctx context.Context, id int64, model, cookie string) bool {
	model = config.CanonicalGatewayBorrowModel(model)
	pool := s.current(ctx)
	if pool == nil || cookie == "" || !pool.config.TargetRequiresModel(id, model) {
		return false
	}
	now := time.Now()
	pool.targetMu.Lock()
	defer pool.targetMu.Unlock()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	check, exists := pool.targetCheck(id, model)
	if s.cfg.AstraRouting(ctx).AutoQuality && (!exists || check.cookieFingerprint != sha256.Sum256([]byte(cookie))) {
		for k, v := range pool.targetRoutePasses {
			if k.accountID == id && k.model == model && v.cookieFingerprint == sha256.Sum256([]byte(cookie)) {
				check = v
				exists = true
				break
			}
		}
	}
	if !exists || !check.passed || !now.Before(check.expires) || check.cookieFingerprint != sha256.Sum256([]byte(cookie)) {
		return false
	}
	route, exists := pool.routes[check.sourceID]
	if s.cfg.AstraRouting(ctx).AutoQuality && check.route.cookie.Value != "" {
		route = check.route
		exists = true
	}
	return exists && route.cookie.Value == cookie && now.Before(route.expires) &&
		(!pool.config.RotateNodes || route.node.Identity == check.node.Identity) &&
		(!pool.config.IPAffinity || pool.config.RotateNodes || check.proxyFingerprint == sha256.Sum256([]byte(route.proxy)))
}

func (s *astraRoutingUpstream) CodexGatewayPinWSBindingExpiryForModel(ctx context.Context, id int64, model, cookie string) time.Time {
	model = config.CanonicalGatewayBorrowModel(model)
	pool := s.current(ctx)
	if pool == nil {
		return time.Time{}
	}
	pool.targetMu.Lock()
	defer pool.targetMu.Unlock()
	check, ok := pool.targetCheckForCookie(id, model, cookie)
	if !ok || !check.passed || !time.Now().Before(check.expires) {
		return time.Time{}
	}
	return check.expires
}
