package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// RequiresGatewayBorrow is an independent, fail-closed account routing policy.
// Model mapping is resolved before matching; the old Sol spelling denotes 6.1.
func (a *Account) RequiresGatewayBorrow(model string) bool {
	if a == nil {
		return false
	}
	return a.RequiresGatewayBorrowUpstream(a.GetMappedModel(strings.TrimSpace(model)))
}

// RequiresGatewayBorrowUpstream takes an already-mapped wire model. Do not
// resolve it again: public->Sol61, Sol61->Luna must still borrow the Sol61 send.
func (a *Account) RequiresGatewayBorrowUpstream(model string) bool {
	if a == nil {
		return false
	}
	raw, exists := a.Extra[GatewayBorrowModelsKey]
	if !exists || raw == nil {
		return false
	}
	var models []string
	switch values := raw.(type) {
	case []string:
		models = values
	case []any:
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return true
			}
			models = append(models, text)
		}
	default:
		return true
	}
	mapped := config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model))
	for _, value := range models {
		canonical := config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(value))
		if canonical != "gpt-6-astra" && canonical != "gpt-6.1-sol" {
			return true
		}
		if canonical == mapped {
			return true
		}
	}
	return false
}

type gatewayBorrowWSProvider interface {
	CodexGatewayPinWSRequestForModel(context.Context, http.Header, string, int64, int, string) (string, string, func(), error)
	CodexGatewayPinWSBindingValidForModel(context.Context, int64, string, string) bool
	CodexGatewayPinWSBindingExpiryForModel(context.Context, int64, string, string) time.Time
}

func (s *OpenAIGatewayService) gatewayBorrowPolicyReason(ctx context.Context, a *Account, model string, compact bool) string {
	if a == nil {
		return ""
	}
	if compact && a.RequiresGatewayBorrowUpstream(resolveOpenAIAccountUpstreamModelForRequest(a, model, true)) {
		return "gateway_borrow_compact_unavailable"
	}
	return s.gatewayBorrowUpstreamPolicyReason(ctx, a, a.GetMappedModel(model), compact)
}

func (s *OpenAIGatewayService) gatewayBorrowUpstreamPolicyReason(ctx context.Context, a *Account, model string, compact bool) string {
	requires := a.RequiresGatewayBorrowUpstream(model)
	if compact && a != nil {
		requires = requires || a.RequiresGatewayBorrowUpstream(resolveOpenAIAccountUpstreamModelForRequest(a, model, true))
	}
	if !requires {
		return ""
	}
	if compact {
		return "gateway_borrow_compact_unavailable"
	}
	if !a.IsOpenAIOAuth() || a.IsShadow() || a.IsOpenAIAgentIdentity() || a.IsOpenAIPersonalAccessToken() {
		return "gateway_borrow_protocol_unavailable"
	}
	if a.isExcelBPSUpstreamModelEnabled(model) || a.isPrismBrowserUpstreamModelEnabled(model) {
		return "gateway_borrow_route_conflict"
	}
	if s == nil || s.cfg == nil {
		return "gateway_borrow_unavailable"
	}
	policy := s.cfg.AstraRouting(ctx)
	mapped := config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model))
	if !policy.CookiePool.Enabled || !policy.CookiePool.TargetRequiresModel(a.ID, mapped) {
		return "gateway_borrow_policy_unavailable"
	}
	if _, ok := s.httpUpstream.(gatewayBorrowWSProvider); !ok {
		return "gateway_borrow_provider_unavailable"
	}
	if expiry := s.gatewayBorrowBindingExpiry(ctx, a, model); expiry.IsZero() || !time.Now().Before(expiry) {
		return "gateway_borrow_route_not_ready"
	}
	return ""
}

func gatewayBorrowEligibilityReason(ctx context.Context, a *Account, model string, compact bool) string {
	if !a.RequiresGatewayBorrow(model) {
		return ""
	}
	// The common scheduling context carries the service for advanced and legacy
	// paths. A missing service cannot turn a required protocol into native access.
	var s *OpenAIGatewayService
	if ctx != nil {
		s, _ = ctx.Value(openAIExcelBPSCooldownServiceContextKey{}).(*OpenAIGatewayService)
	}
	return s.gatewayBorrowPolicyReason(ctx, a, model, compact)
}

// prepareGatewayBorrowWS authorizes this exact model and binds the upstream
// cookie before a connection is acquired. Empty/unsupported providers fail closed.
func (s *OpenAIGatewayService) prepareGatewayBorrowWS(ctx context.Context, a *Account, model string, headers http.Header, proxy string) (string, string, func(), error) {
	release := func() {}
	if !a.RequiresGatewayBorrowUpstream(model) {
		return "", proxy, release, nil
	}
	if reason := s.gatewayBorrowUpstreamPolicyReason(ctx, a, model, false); reason != "" {
		return "", proxy, release, denyOpenAITurn(reason)
	}
	mapped := config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model))
	provider, ok := s.httpUpstream.(gatewayBorrowWSProvider)
	if !ok {
		return "", proxy, func() {}, denyOpenAITurn("gateway_borrow_provider_unavailable")
	}
	cookie, effectiveProxy, release, err := provider.CodexGatewayPinWSRequestForModel(ctx, headers, proxy, a.ID, a.Concurrency, mapped)
	if release == nil {
		release = func() {}
	}
	if err != nil || cookie == "" {
		release()
		return "", proxy, func() {}, denyOpenAITurn("gateway_borrow_route_unavailable")
	}
	if !provider.CodexGatewayPinWSBindingValidForModel(ctx, a.ID, mapped, cookie) {
		release()
		return "", proxy, func() {}, denyOpenAITurn("gateway_borrow_route_expired")
	}
	replaceCodexWSAnchorCookie(headers, cookie)
	policy := s.cfg.AstraRouting(ctx)
	fingerprint := openAITurnRouteFingerprint(a)
	expiry := provider.CodexGatewayPinWSBindingExpiryForModel(ctx, a.ID, mapped, cookie)
	if expiry.IsZero() || !time.Now().Before(expiry) {
		release()
		return "", proxy, func() {}, denyOpenAITurn("gateway_borrow_route_expired")
	}
	scope := fmt.Sprintf("gateway-borrow:%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%x:%x:%d:%s", policy.Revision, a.ID, mapped, fingerprint, gatewayBorrowCredentialFingerprint(a), expiry.UnixNano(), cookie))))
	return scope, effectiveProxy, release, nil
}

func gatewayBorrowCookie(headers http.Header) string {
	request := &http.Request{Header: headers}
	for _, cookie := range request.Cookies() {
		if cookie.Name == "__oailb" {
			return cookie.Value
		}
	}
	return ""
}

func (s *OpenAIGatewayService) gatewayBorrowBindingExpiry(ctx context.Context, a *Account, model string) time.Time {
	if s == nil || s.cfg == nil {
		return time.Time{}
	}
	policy := s.cfg.AstraRouting(ctx)
	provider, ok := s.httpUpstream.(AstraGatewayRuntimeProvider)
	if !ok {
		return time.Time{}
	}
	snapshot := provider.AstraGatewaySnapshot(ctx)
	if snapshot.Revision != policy.Revision {
		return time.Time{}
	}
	mapped := config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model))
	for _, row := range snapshot.Targets {
		if row.AccountID == a.ID && config.CanonicalGatewayBorrowModel(row.Model) == mapped && row.State == "ready" && row.Reason == "target_probe_passed" && row.ExpiresAt != nil {
			return *row.ExpiresAt
		}
	}
	return time.Time{}
}

func gatewayBorrowCredentialFingerprint(a *Account) [32]byte {
	if a == nil {
		return [32]byte{}
	}
	proxy := ""
	if a.Proxy != nil {
		proxy = a.Proxy.URL()
	}
	return sha256.Sum256([]byte(a.GetCredential("access_token") + "\x00" + a.GetCredential("chatgpt_account_id") + "\x00" + a.GetCredential("email") + "\x00" + proxy))
}

func (s *OpenAIGatewayService) checkGatewayBorrowWSHeaders(ctx context.Context, a *Account, model string, h http.Header) error {
	if !a.RequiresGatewayBorrowUpstream(model) {
		return nil
	}
	if reason := s.gatewayBorrowUpstreamPolicyReason(ctx, a, model, false); reason != "" {
		return denyOpenAITurn(reason)
	}
	cookie := gatewayBorrowCookie(h)
	provider, ok := s.httpUpstream.(gatewayBorrowWSProvider)
	if !ok || cookie == "" || !provider.CodexGatewayPinWSBindingValidForModel(ctx, a.ID, config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model)), cookie) {
		return denyOpenAITurn("connection_borrow_revoked")
	}
	return nil
}

type gatewayBorrowRequiredModelContextKey struct{}

// WithGatewayBorrowRequiredModel carries the canonical, service-owned outbound
// model into the shared upstream transport. It is not a client header.
func WithGatewayBorrowRequiredModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, gatewayBorrowRequiredModelContextKey{}, config.CanonicalGatewayBorrowModel(model))
}
func GatewayBorrowRequiredModelFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	model, ok := ctx.Value(gatewayBorrowRequiredModelContextKey{}).(string)
	return model, ok
}
func (a *Account) gatewayBorrowPolicyActive() bool {
	if a == nil {
		return false
	}
	switch models := a.Extra[GatewayBorrowModelsKey].(type) {
	case nil:
		return false
	case []string:
		return len(models) > 0
	case []any:
		return len(models) > 0
	default:
		return true
	}
}

type openAIFinalSendScopeKey struct{}
type openAIFinalSendScope struct {
	groupID      int64
	enforceGroup bool
	model        string
	continuation bool
	previousID   string
	keyID        int64
}

// Image multipart payloads are already parsed and mapped by their protocol
// handler; preserve their wire model without reinterpreting binary media as JSON.
func withOpenAIFinalSendModel(ctx context.Context, c *gin.Context, model string) context.Context {
	groupID, enforce := openAITurnAdmissionGroupFromContext(c)
	return context.WithValue(ctx, openAIFinalSendScopeKey{}, openAIFinalSendScope{groupID: groupID, enforceGroup: enforce, model: model, keyID: getAPIKeyIDFromContext(c)})
}

func withOpenAIFinalSendScope(ctx context.Context, c *gin.Context, body []byte) context.Context {
	groupID, enforce := openAITurnAdmissionGroupFromContext(c)
	return context.WithValue(ctx, openAIFinalSendScopeKey{}, openAIFinalSendScope{groupID: groupID, enforceGroup: enforce, model: gjson.GetBytes(body, "model").String(), continuation: gjson.GetBytes(body, "previous_response_id").String() != "" || gjson.GetBytes(body, "conversation").Exists(), previousID: gjson.GetBytes(body, "previous_response_id").String(), keyID: getAPIKeyIDFromContext(c)})
}

// Every HTTP attempt, including compatibility bridges and proxy retries,
// rechecks the selected account before plugins or transports can send bytes.
func (s *OpenAIGatewayService) admitOpenAIHTTPRequest(req *http.Request, a *Account) (*Account, error) {
	if s == nil || a == nil || !a.IsOpenAI() {
		return a, nil
	}
	_, hasReader := s.accountRepo.(OpenAITurnAdmissionReader)
	if !hasReader && !s.requireLatestTurnAdmission && !a.gatewayBorrowPolicyActive() {
		return a, nil
	}
	if req == nil {
		return nil, denyOpenAITurn("request_model_unavailable")
	}
	scope, _ := req.Context().Value(openAIFinalSendScopeKey{}).(openAIFinalSendScope)
	model := scope.model
	if model == "" {
		if req.GetBody == nil {
			return nil, denyOpenAITurn("request_model_unavailable")
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, denyOpenAITurn("request_model_unavailable")
		}
		defer func() { _ = body.Close() }()
		raw, err := io.ReadAll(io.LimitReader(body, (4<<20)+1))
		if err != nil || len(raw) > 4<<20 || !gjson.ValidBytes(raw) {
			return nil, denyOpenAITurn("request_model_unavailable")
		}
		field := gjson.GetBytes(raw, "model")
		if field.Type != gjson.String || strings.TrimSpace(field.String()) == "" {
			return nil, denyOpenAITurn("request_model_unavailable")
		}
		model = field.String()
		scope.previousID = gjson.GetBytes(raw, "previous_response_id").String()
		scope.continuation = scope.previousID != "" || gjson.GetBytes(raw, "conversation").Exists()
	}
	latest, err := s.latestOpenAITurnAccountForGroup(req.Context(), a, scope.groupID, scope.enforceGroup)
	if err == nil && openAITurnRouteFingerprint(latest) != openAITurnRouteFingerprint(a) {
		err = denyOpenAITurn("account_binding_changed")
	}
	if err == nil {
		if reason := s.gatewayBorrowUpstreamPolicyReason(req.Context(), latest, model, false); reason != "" {
			err = denyOpenAITurn(reason)
		}
	}
	if err == nil {
		if scope.enforceGroup && !latest.IsModelAllowedInGroup(&scope.groupID, model) {
			err = denyOpenAITurn("model_not_allowed_in_group")
		}
	}
	if err == nil && (s.getOpenAIAccountModelTransientState().isBlocked(latest.ID, openAIAccountModelTransientModel(model), time.Now()) || latest.isRateLimitActiveForKey(model) || (openAIImageGenerationRateLimitApplies(req.Context(), model, model) && latest.isRateLimitActiveForKey(openAIImageGenerationRateLimitKey))) {
		err = denyOpenAITurn("model_rate_limited")
	}
	if err != nil {
		return nil, err
	}
	if latest.RequiresGatewayBorrowUpstream(model) {
		if scope.continuation {
			canonical := config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model))
			if scope.previousID == "" {
				return nil, denyOpenAITurn("gateway_borrow_continuation_unverified")
			}
			if _, err := s.gatewayBorrowHTTPContinuation(req.Context(), latest, canonical, scope.previousID, scope.keyID, scope.groupID); err != nil {
				return nil, err
			}
		}
		if req.URL == nil || req.URL.Scheme != "https" || req.URL.Hostname() != "chatgpt.com" || strings.TrimRight(req.URL.Path, "/") != "/backend-api/codex/responses" {
			return nil, denyOpenAITurn("gateway_borrow_endpoint_unavailable")
		}
		if gatewayBorrowCredentialFingerprint(latest) != gatewayBorrowCredentialFingerprint(a) {
			return nil, denyOpenAITurn("account_binding_changed")
		}
	}
	if s.openAICodexTicketBlocksAccount(latest, model) {
		return nil, denyOpenAITicket()
	}
	return latest, nil
}
