package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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
	failureCode                  string
	attempts, correct            int
	quotaKey, negativeKey        astraTargetQuotaKey
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

// Quota/auth rejection describes this credential and exit, not a routing
// Cookie. Retain the quiet period across donor/Cookie and settings revisions.
type astraTargetQuotaKey struct {
	accountID int64
	model     string
	identity  [32]byte
	proxy     [32]byte
	sourceID  int64
}
type astraTargetQuotaBackoff struct {
	until       time.Time
	reason      string
	failures    int
	lastFailure time.Time
}

func (v astraTargetQuotaBackoff) discardAt() time.Time {
	if v.failures > 0 {
		return v.lastFailure.Add(time.Hour)
	}
	return v.until
}

func targetBorrowQuietKeys(req *http.Request, id int64, model, proxy string, sourceID int64, profile *tlsfingerprint.Profile) (astraTargetQuotaKey, astraTargetQuotaKey) {
	quotaIdentity, _ := json.Marshal([]string{req.Header.Get("Authorization"), req.Header.Get("ChatGPT-Account-ID")})
	negativeIdentity, _ := json.Marshal([]string{req.Header.Get("Authorization"), req.Header.Get("ChatGPT-Account-ID"), req.Header.Get("User-Agent"), req.Header.Get("Originator"), req.Header.Get("Version")})
	profileIdentity, _ := json.Marshal(profile)
	proxyFingerprint := sha256.Sum256([]byte(proxy))
	return astraTargetQuotaKey{accountID: id, model: model, identity: sha256.Sum256(quotaIdentity), proxy: proxyFingerprint},
		astraTargetQuotaKey{accountID: id, model: model, identity: sha256.Sum256(append(negativeIdentity, profileIdentity...)), proxy: proxyFingerprint, sourceID: sourceID}
}

func (s *astraRoutingUpstream) targetQuotaWait(key astraTargetQuotaKey, now time.Time) (astraTargetQuotaBackoff, bool) {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	for k, v := range s.quotaBackoff {
		if !now.Before(v.discardAt()) {
			delete(s.quotaBackoff, k)
		}
	}
	v, ok := s.quotaBackoff[key]
	return v, ok && now.Before(v.until)
}

func (s *astraRoutingUpstream) rememberTargetQuota(key astraTargetQuotaKey, reason string, until time.Time) {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	if s.quotaBackoff == nil {
		s.quotaBackoff = map[astraTargetQuotaKey]astraTargetQuotaBackoff{}
	}
	for k, v := range s.quotaBackoff {
		if !time.Now().Before(v.discardAt()) {
			delete(s.quotaBackoff, k)
		}
	}
	if len(s.quotaBackoff) >= 4096 {
		for k := range s.quotaBackoff {
			delete(s.quotaBackoff, k)
			break
		}
	}
	if prior, ok := s.quotaBackoff[key]; ok && time.Now().Before(prior.until) {
		return
	}
	s.quotaBackoff[key] = astraTargetQuotaBackoff{until: until, reason: reason}
}

// Only settled quality failures increase the tier; cache reads and quota/auth
// rejections do not. Expired quiet periods retain their count for one hour.
func (s *astraRoutingUpstream) rememberTargetQualityFailure(key astraTargetQuotaKey, reason string, now time.Time) time.Time {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	if s.quotaBackoff == nil {
		s.quotaBackoff = map[astraTargetQuotaKey]astraTargetQuotaBackoff{}
	}
	for k, v := range s.quotaBackoff {
		if !now.Before(v.discardAt()) {
			delete(s.quotaBackoff, k)
		}
	}
	prior := s.quotaBackoff[key]
	if now.Before(prior.until) {
		return prior.until
	}
	count := min(prior.failures+1, 4)
	delay := min(time.Duration(180*(1<<(count-1)))*time.Second, 15*time.Minute)
	if len(s.quotaBackoff) >= 4096 {
		for k := range s.quotaBackoff {
			delete(s.quotaBackoff, k)
			break
		}
	}
	until := now.Add(delay)
	s.quotaBackoff[key] = astraTargetQuotaBackoff{until: until, reason: reason, failures: count, lastFailure: now}
	return until
}

func (s *astraRoutingUpstream) resetTargetQualityFailure(key astraTargetQuotaKey) {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	delete(s.quotaBackoff, key)
}

type autoBorrowRouteKey struct{}
type autoBorrowRouteSelection struct {
	sourceID int64
	route    codexGatewayRoute
}

// Quality keys collapse to Astra in the explicitly activated account policy.
// Transport, quota and response-owner checks retain the requested wire model.
func (p *codexGatewayPinUpstream) qualityModel(model string) string {
	if p.config.QualityMode == config.GatewayBorrowAccountQualityMode && config.CanonicalGatewayBorrowModel(model) != "" {
		return "gpt-6-astra"
	}
	return model
}

// Call under targetMu. Legacy pools preserve independent per-model witnesses.
func (p *codexGatewayPinUpstream) targetCheck(id int64, model string) (astraTargetValidation, bool) {
	model = p.qualityModel(model)
	if model == "gpt-6-astra" {
		check, exists := p.targetChecks[id]
		return check, exists
	}
	check, exists := p.targetModelChecks[astraTargetModelKey{id, model}]
	return check, exists
}

func (p *codexGatewayPinUpstream) setTargetCheck(id int64, model string, check astraTargetValidation) {
	model = p.qualityModel(model)
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
	model = p.qualityModel(model)
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
// selected quality policy and the target credentials on the actual exit.
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
	qualityModel := pool.qualityModel(model)
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
		if err := s.PrepareAstraGateway(targetPreparationContext(req, proxy, id, model, profile)); err != nil {
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
	if pool.config.RotateNodes && pool.nodeCoolingForModel(route.node, astraRoutingHost(cookie.Value), id, qualityModel, time.Now()) {
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
	identity := []string{qualityModel, cookie.Value, strconv.FormatInt(sourceID, 10), identityProxy, req.Header.Get("Authorization"), req.Header.Get("ChatGPT-Account-ID"), req.Header.Get("User-Agent"), req.Header.Get("Originator"), req.Header.Get("Version"), req.Header.Get("X-Codex-Turn-State")}
	if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode {
		identity[len(identity)-1] = ""
	}
	quotaKey, _ := targetBorrowQuietKeys(req, id, model, identityProxy, sourceID, profile)
	_, negativeKey := targetBorrowQuietKeys(req, id, qualityModel, identityProxy, sourceID, profile)
	if s.cfg.AstraRouting(req.Context()).AutoQuality {
		if prior, waiting := s.targetQuotaWait(quotaKey, time.Now()); waiting {
			return nil, zero, proxy, release, errors.New(prior.reason)
		}
	}
	fingerprint, _ := json.Marshal(profile)
	identity = append(identity, string(fingerprint))
	key := sha256.Sum256([]byte(strings.Join(identity, "\x00")))
	pairIdentity := append([]string{strconv.FormatInt(id, 10)}, identity...)
	pairIdentity[2] = "" // New Cookies inherit only the short-test strategy, never a quality pass.
	pairKey := sha256.Sum256([]byte(strings.Join(pairIdentity, "\x00")))
	now := time.Now()
	pool.targetMu.Lock()
	old, exists := pool.targetCheck(id, model)
	if hasSelection && old.key != key {
		if prior, found := pool.targetRoutePasses[astraTargetRouteKey{id, qualityModel, sourceID, sha256.Sum256([]byte(cookie.Value))}]; found && prior.key == key {
			old = prior
			exists = true
		}
	}
	if s.cfg.AstraRouting(req.Context()).AutoQuality {
		failure, failed := pool.targetRouteFailures[astraTargetRouteKey{id, qualityModel, sourceID, sha256.Sum256([]byte(cookie.Value))}]
		if failed && failure.key == key && now.Before(failure.retryAfter) {
			pool.targetMu.Unlock()
			return nil, key, proxy, release, errors.New(failure.reason)
		}
	}
	pool.targetMu.Unlock()
	if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode && exists &&
		old.cookieFingerprint == sha256.Sum256([]byte(cookie.Value)) && old.sourceID == sourceID && old.expires.Before(expires) {
		expires = old.expires
		if !now.Before(expires) {
			return nil, key, proxy, release, errors.New("borrow_route_expired")
		}
	}
	force, _ := req.Context().Value(astraForceProbeKey{}).(bool)
	if exists && old.key == key && now.Before(old.expires) && (!force || pool.config.QualityMode == config.GatewayBorrowAccountQualityMode) {
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
		if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode {
			probeQuotaKey, _ := targetBorrowQuietKeys(req, id, qualityModel, identityProxy, sourceID, profile)
			if prior, waiting := s.targetQuotaWait(probeQuotaKey, time.Now()); waiting {
				return nil, key, proxy, release, errors.New(prior.reason)
			}
		}
		if prior, waiting := s.targetQuotaWait(negativeKey, time.Now()); waiting {
			return nil, key, proxy, release, errors.New(prior.reason)
		}
	}
	if s.cfg.AstraRouting(req.Context()).AutoQuality {
		pool.targetMu.Lock()
		if pool.autoProbeSlots == nil {
			pool.autoProbeSlots = make(chan struct{}, 2)
			pool.autoProbeLocks = map[astraTargetModelKey]*sync.Mutex{}
		}
		lock := pool.autoProbeLocks[astraTargetModelKey{id, qualityModel}]
		if lock == nil {
			lock = &sync.Mutex{}
			pool.autoProbeLocks[astraTargetModelKey{id, qualityModel}] = lock
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
	var result *service.OpenAICodexStateProbeResult
	if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode {
		pool.targetMu.Lock()
		provenPair := pool.targetPairPasses[pairKey]
		pool.targetMu.Unlock()
		result = service.ProbeOpenAICodexBorrowAccountQualityRoute(ctx, s.delegate, probe, proxy, id, n, profile, provenPair)
	} else {
		result = service.ProbeOpenAICodexBorrowQualityRoute(ctx, s.delegate, probe, proxy, id, n, profile, model)
	}
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
			pool.historyRecorder(service.AstraGatewayHistoryRecord{Gateway: astraRoutingHost(cookie.Value), SourceAccountID: sourceID, TargetAccountID: id, LastSeen: time.Now(), LastReason: reason, LastAnswer: result.Answer}, passed)
		}
	}()
	if pool.targetChecks == nil {
		pool.targetChecks = make(map[int64]astraTargetValidation)
	}
	if passed {
		reason = "target_probe_passed"
		if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode {
			if pool.targetPairPasses == nil {
				pool.targetPairPasses = map[[32]byte]bool{}
			}
			if len(pool.targetPairPasses) < 4096 {
				pool.targetPairPasses[pairKey] = true
			}
		}
	}
	// Revalidating an identical Cookie must not advance its proven deadline.
	if exists && old.cookieFingerprint == sha256.Sum256([]byte(cookie.Value)) && old.sourceID == sourceID && old.expires.Before(expires) {
		expires = old.expires
	}
	host := astraRoutingHost(cookie.Value)
	retryDelay := 15 * time.Second
	if result.Verdict == service.OpenAICodexStateDegraded || result.Failure == "quality_failed" {
		retryDelay = 180 * time.Second
		if s.cfg.AstraRouting(req.Context()).AutoQuality {
			retryDelay = time.Until(s.rememberTargetQualityFailure(negativeKey, reason, time.Now()))
		}
	}
	if result.Failure == service.OpenAICodexStateFailureRateLimited || result.Failure == service.OpenAICodexStateFailureAccountError {
		retryDelay = 5 * time.Minute
		if result.Failure == service.OpenAICodexStateFailureRateLimited {
			reason = "target_probe_rate_limited"
		} else {
			reason = "target_probe_auth_failed"
		}
		if s.cfg.AstraRouting(req.Context()).AutoQuality {
			probeQuotaKey := quotaKey
			if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode {
				probeQuotaKey, _ = targetBorrowQuietKeys(req, id, qualityModel, identityProxy, sourceID, profile)
			}
			s.rememberTargetQuota(probeQuotaKey, reason, time.Now().Add(retryDelay))
		}
	}
	if passed && s.cfg.AstraRouting(req.Context()).AutoQuality {
		s.resetTargetQualityFailure(negativeKey)
	}
	check := astraTargetValidation{node: route.node, key: key, cookieFingerprint: sha256.Sum256([]byte(cookie.Value)), proxyFingerprint: sha256.Sum256([]byte(proxy)), passed: passed, checked: time.Now(), expires: expires, retryAfter: time.Now().Add(retryDelay), sourceID: sourceID, reason: reason, gateway: host, quotaKey: quotaKey, negativeKey: negativeKey, failureCode: result.Failure, attempts: result.Attempts, correct: result.Correct, answer: result.Answer}
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
		pool.targetRouteFailures[astraTargetRouteKey{id, qualityModel, sourceID, check.cookieFingerprint}] = check
		if pool.config.QualityMode == config.GatewayBorrowAccountQualityMode && result.Failure == "quality_failed" {
			delete(pool.targetRoutePasses, astraTargetRouteKey{id, qualityModel, sourceID, check.cookieFingerprint})
		}
		if !exists || !old.passed || !time.Now().Before(old.expires) ||
			(pool.config.QualityMode == config.GatewayBorrowAccountQualityMode && result.Failure == "quality_failed" && old.cookieFingerprint == check.cookieFingerprint && old.sourceID == sourceID) {
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
			pool.targetRoutePasses[astraTargetRouteKey{id, qualityModel, sourceID, check.cookieFingerprint}] = check
		}
	}
	pool.mu.Lock()
	pool.observeGatewayTarget(host, passed)
	pool.mu.Unlock()
	if nodeStore != nil {
		nodeStore.MarkTarget(cookie.Value, id, passed, reason, time.Now(), expires, time.Now().Add(15*time.Second))
	}
	if !passed {
		check.model = qualityModel
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
	return s.VerifyAstraGatewayTargetForModelWithTLS(ctx, req, proxy, id, n, model, nil)
}

func (s *astraRoutingUpstream) VerifyAstraGatewayTargetForModelWithTLS(ctx context.Context, req *http.Request, proxy string, id int64, n int, model string, profile *tlsfingerprint.Profile) error {
	model = config.CanonicalGatewayBorrowModel(model)
	if model == "" {
		return errors.New("borrow_model_required")
	}
	req = req.Clone(context.WithValue(ctx, astraForceProbeKey{}, true))
	body, _ := json.Marshal(map[string]string{"model": model})
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	_, _, _, release, err := s.targetRoute(req, proxy, id, n, profile)
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
			if k.accountID == id && k.model == pool.qualityModel(model) && v.cookieFingerprint == sha256.Sum256([]byte(cookie)) {
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
