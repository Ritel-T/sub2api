package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const accountTestGatewayBorrowKey = "account_test_gateway_borrow"

type gatewayBorrowAccountTestIdentityKey struct{}
type gatewayBorrowAccountTestIdentity struct{ profile *tlsfingerprint.Profile }

// AccountTestUsedGatewayBorrow distinguishes a route test from evidence that
// the account's native or other model-specific restrictions recovered.
func AccountTestUsedGatewayBorrow(c *gin.Context) bool {
	return c != nil && c.GetBool(accountTestGatewayBorrowKey)
}

// The background verifier and account-test button use the same final identity.
// Refresh through the production token provider, then read the persisted account
// again so model policy, proxy and workspace identity are never copied from a
// snapshot taken before refresh.
func (s *AccountTestService) prepareGatewayBorrowAccountIdentity(ctx context.Context, selected *Account, model string) (*Account, http.Header, *tlsfingerprint.Profile, error) {
	if s == nil || s.openaiGatewayService == nil || s.accountRepo == nil {
		return nil, nil, nil, denyOpenAITurn("gateway_borrow_unavailable")
	}
	selectedRoute := openAITurnRouteFingerprint(selected)
	token, _, err := s.openaiGatewayService.GetAccessToken(ctx, selected)
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, nil, nil, denyOpenAITurn("target_probe_auth_failed")
	}
	account, err := s.accountRepo.GetByID(ctx, selected.ID)
	if err != nil || account == nil || account.ID != selected.ID {
		return nil, nil, nil, denyOpenAITurn("latest_state_unavailable")
	}
	if openAITurnRouteFingerprint(account) != selectedRoute {
		return nil, nil, nil, denyOpenAITurn("account_binding_changed")
	}
	if account.GetOpenAIAccessToken() != token {
		// A dedicated OpenAI refresh can persist new auth while leaving the
		// older provider-cache entry. Invalidate only this account and retry
		// once; routing changes never enter this recovery path.
		provider := s.openaiGatewayService.openAITokenProvider
		if provider == nil || provider.tokenCache == nil || strings.TrimSpace(account.GetOpenAIAccessToken()) == "" {
			return nil, nil, nil, denyOpenAITurn("account_binding_changed")
		}
		if err := provider.tokenCache.DeleteAccessToken(ctx, OpenAITokenCacheKey(account)); err != nil {
			return nil, nil, nil, denyOpenAITurn("account_binding_changed")
		}
		fresh := account
		freshRoute := openAITurnRouteFingerprint(fresh)
		token, _, err = s.openaiGatewayService.GetAccessToken(ctx, fresh)
		if err != nil || strings.TrimSpace(token) == "" {
			return nil, nil, nil, denyOpenAITurn("target_probe_auth_failed")
		}
		account, err = s.accountRepo.GetByID(ctx, fresh.ID)
		if err != nil || account == nil || account.ID != fresh.ID {
			return nil, nil, nil, denyOpenAITurn("latest_state_unavailable")
		}
		if openAITurnRouteFingerprint(account) != freshRoute || account.GetOpenAIAccessToken() != token {
			return nil, nil, nil, denyOpenAITurn("account_binding_changed")
		}
	}
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")
	headers.Set("OpenAI-Beta", "responses=experimental")
	headers.Set("Authorization", "Bearer "+token)
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, headers, account); err != nil {
		return nil, nil, nil, denyOpenAITurn("target_probe_auth_failed")
	}
	applyOpenAICodexTicketHarvestIdentity(headers, model)
	enforceCodexIdentityHeadersWithUA(headers, account.GetOpenAIUserAgent())
	applyOpenAIAPIKeyIdentityHeaders(headers, account, account.GetOpenAIUserAgent())
	account.ApplyHeaderOverrides(headers)
	var profile *tlsfingerprint.Profile
	if s.tlsFPProfileService != nil {
		profile = s.tlsFPProfileService.ResolveTLSProfile(account)
	}
	return account, headers, profile, nil
}

func gatewayBorrowAccountTestErrorCode(err error) string {
	var admission *OpenAITurnAdmissionError
	if errors.As(err, &admission) {
		return admission.Reason
	}
	for _, reason := range []string{"target_validation_in_progress", "preparation_in_progress", "target_probe_degraded", "target_quality_failed", "target_probe_rate_limited", "target_probe_auth_failed", "target_probe_failed", "target_route_changed", "borrow_route_expired", "no_qualified_source_route", "source_probe_cooldown", "borrow_required_route_unavailable", "configuration_changed"} {
		if err != nil && strings.Contains(err.Error(), reason) {
			return reason
		}
	}
	return "gateway_borrow_request_failed"
}

func (s *AccountTestService) sendGatewayBorrowTestError(c *gin.Context, err error) error {
	code := gatewayBorrowAccountTestErrorCode(err)
	s.sendEvent(c, TestEvent{Type: "error", Code: code, Channel: "gateway_borrow", Error: "Gateway borrow test failed: " + code})
	return fmt.Errorf("gateway borrow test failed: %s", code)
}

func (s *AccountTestService) testGatewayBorrowAccountConnection(c *gin.Context, selected *Account, model, mode string) error {
	c.Set(accountTestGatewayBorrowKey, true)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model, Channel: "gateway_borrow"})
	if mode != AccountTestModeDefault {
		return s.sendGatewayBorrowTestError(c, denyOpenAITurn("gateway_borrow_compact_unavailable"))
	}
	ctx := c.Request.Context()
	account, headers, profile, err := s.prepareGatewayBorrowAccountIdentity(ctx, selected, model)
	if err != nil {
		return s.sendGatewayBorrowTestError(c, err)
	}
	payload := createOpenAITestPayload(model, true)
	if options, ok := pelicanTestOptionsFromContext(ctx); ok {
		payload = createPelicanOpenAIPayload(model, true, options.prompt, options.reasoningEffort)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return s.sendGatewayBorrowTestError(c, err)
	}
	ctx = WithGatewayBorrowRequiredModel(withOpenAIFinalSendScope(ctx, c, body), model)
	ctx = context.WithValue(ctx, gatewayBorrowAccountTestIdentityKey{}, gatewayBorrowAccountTestIdentity{profile: profile})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(body))
	if err != nil {
		return s.sendGatewayBorrowTestError(c, err)
	}
	req.Host = "chatgpt.com"
	req.Header = headers
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	s.sendEvent(c, TestEvent{Type: "status", Code: "gateway_borrow_testing", Channel: "gateway_borrow", Text: "Testing the verified gateway borrow route"})
	resp, err := s.doOpenAIAccountTestUpstream(req, openAIAccountProxyURL(account), account, true)
	if err != nil {
		return s.sendGatewayBorrowTestError(c, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		code := "gateway_borrow_request_failed"
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			code = "target_probe_rate_limited"
		case http.StatusUnauthorized, http.StatusForbidden:
			code = "target_probe_auth_failed"
		}
		return s.sendGatewayBorrowTestError(c, errors.New(code))
	}
	// Account tests have a small bounded payload. Validate the entire terminal
	// response before emitting success so failed or contradictory SSE cannot
	// claim that this borrowed model completed successfully.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, openAICodexStateProbeMaxBody+1))
	if err != nil || len(raw) > openAICodexStateProbeMaxBody {
		return s.sendGatewayBorrowTestError(c, errors.New("gateway_borrow_request_failed"))
	}
	if err := validateCodexProbeResponse(raw, model); err != nil {
		payload := openAICodexStateStreamErrorPayload(raw)
		code := gjson.GetBytes(payload, "error.code").String()
		if code == "" {
			code = gjson.GetBytes(payload, "error.type").String()
		}
		if code == "usage_limit_reached" || code == "rate_limit_exceeded" || code == "rate_limit_error" {
			return s.sendGatewayBorrowTestError(c, errors.New("target_probe_rate_limited"))
		}
		return s.sendGatewayBorrowTestError(c, errors.New("gateway_borrow_request_failed"))
	}
	completedModel := ""
	for _, event := range openAICodexStateStreamEvents(raw) {
		if gjson.GetBytes(event, "type").String() == "response.completed" {
			completedModel = gjson.GetBytes(event, "response.model").String()
		}
	}
	if completedModel != model {
		return s.sendGatewayBorrowTestError(c, errors.New("gateway_borrow_response_model_mismatch"))
	}
	return s.processOpenAIStream(c, bytes.NewReader(raw))
}
