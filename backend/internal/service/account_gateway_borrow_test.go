package service

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type borrowSafetyProvider struct {
	HTTPUpstream
	cookie   string
	snapshot AstraGatewayRuntime
	valid    bool
	calls    int
}

func (p *borrowSafetyProvider) CodexGatewayPinWSRequestForModel(_ context.Context, h http.Header, proxy string, id int64, n int, model string) (string, string, func(), error) {
	p.calls++
	if p.cookie == "" {
		return "", proxy, func() {}, errors.New("not ready")
	}
	return p.cookie, "http://borrow-exit.invalid:80", func() {}, nil
}
func (p *borrowSafetyProvider) CodexGatewayPinWSBindingValidForModel(_ context.Context, id int64, model, cookie string) bool {
	return p.valid && cookie == p.cookie
}
func (p *borrowSafetyProvider) AstraGatewaySnapshot(context.Context) AstraGatewayRuntime {
	return p.snapshot
}
func (p *borrowSafetyProvider) SetAstraGatewayPreparer(func(context.Context, int64) error) {}
func (p *borrowSafetyProvider) PrepareAstraGateway(context.Context) error                  { return nil }
func borrowSafetyService() (*OpenAIGatewayService, *Account, *borrowSafetyProvider) {
	a := ticketTestAccount(901)
	a.Extra = map[string]any{GatewayBorrowModelsKey: []string{"gpt-6-astra", "gpt-6.1-sol"}}
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{902}, TargetAccountIDs: []int64{a.ID}, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}}
	expires := time.Now().Add(time.Minute)
	p := &borrowSafetyProvider{cookie: "private-fixture-cookie", valid: true, snapshot: AstraGatewayRuntime{Targets: []AstraRouteStatus{{AccountID: a.ID, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expires}}}}
	return &OpenAIGatewayService{cfg: cfg, httpUpstream: p}, a, p
}
func TestGatewayBorrowModelPolicyMappingAndCorruption(t *testing.T) {
	_, a, _ := borrowSafetyService()
	a.Credentials = maps.Clone(a.Credentials)
	a.Credentials["model_mapping"] = map[string]any{"public": "gpt-6.1-sol"}
	for _, model := range []string{"public", "gpt-6-sol", "gpt-6.1-sol-high"} {
		require.True(t, a.RequiresGatewayBorrow(model), model)
	}
	require.False(t, a.RequiresGatewayBorrow("gpt-6-luna"))
	a.Extra[GatewayBorrowModelsKey] = []any{"gpt-6-astra", true}
	require.True(t, a.RequiresGatewayBorrow("gpt-6-luna"), "corrupt policy cannot create native access")
}
func TestGatewayBorrowSelectionAndCompactFailClosed(t *testing.T) {
	s, a, _ := borrowSafetyService()
	scheduler, ok := newDefaultOpenAIAccountScheduler(s, nil).(*defaultOpenAIAccountScheduler)
	require.True(t, ok)
	require.Empty(t, s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6-astra", false))
	compatible, reason := scheduler.isAccountRequestCompatibleReason(t.Context(), a, OpenAIAccountScheduleRequest{RequestedModel: "gpt-6-astra", RequireCompact: true})
	require.False(t, compatible)
	require.Equal(t, "gateway_borrow_compact_unavailable", reason)
	s.cfg.Gateway.CodexGatewayPin.Enabled = false
	require.Equal(t, "gateway_borrow_policy_unavailable", s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6-astra", false))
	s.cfg.Gateway.CodexGatewayPin.Enabled = true
	s.httpUpstream = nil
	require.Equal(t, "gateway_borrow_provider_unavailable", s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6-astra", false))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses/compact", nil)
	_, err := s.admitOpenAITurn(t.Context(), c, a, "gpt-6-astra")
	require.ErrorContains(t, err, "gateway_borrow_compact_unavailable")
}
func TestGatewayBorrowWSUsesVerifiedCookieAndNewPoolScope(t *testing.T) {
	s, a, p := borrowSafetyService()
	headers := http.Header{"Cookie": []string{"unrelated=kept; __oailb=client-cookie"}}
	scope, proxy, release, err := s.prepareGatewayBorrowWS(t.Context(), a, "gpt-6-astra", headers, "")
	require.NoError(t, err)
	defer release()
	require.NotEmpty(t, scope)
	require.Equal(t, "http://borrow-exit.invalid:80", proxy)
	require.Equal(t, p.cookie, gatewayBorrowCookie(headers))
	require.Contains(t, headers.Get("Cookie"), "unrelated=kept")
	b := s.bindOpenAIWSHandshake(a, "gpt-6-astra", headers)
	require.NoError(t, s.checkOpenAIWSBinding(a, "gpt-6-astra", b))
	s.cfg.Gateway.CodexGatewayPin.Enabled = false
	require.Error(t, s.checkOpenAIWSBinding(a, "gpt-6-astra", b))
	s.cfg.Gateway.CodexGatewayPin.Enabled = true
	p.cookie = "rotated-cookie"
	require.ErrorContains(t, s.checkOpenAIWSBinding(a, "gpt-6-astra", b), "connection_borrow_revoked")
	next, _, closeNext, err := s.prepareGatewayBorrowWS(t.Context(), a, "gpt-6-astra", headers, "")
	require.NoError(t, err)
	defer closeNext()
	require.NotEqual(t, scope, next)
	p.valid = false
	_, _, _, err = s.prepareGatewayBorrowWS(t.Context(), a, "gpt-6-astra", headers, "")
	require.Error(t, err)
}
func TestGatewayBorrowWSRejectsOldNativeExpiredCredentialAndPolicyBindings(t *testing.T) {
	s, a, p := borrowSafetyService()
	headers := http.Header{}
	_, _, release, err := s.prepareGatewayBorrowWS(t.Context(), a, "gpt-6-astra", headers, "")
	require.NoError(t, err)
	defer release()
	b := s.bindOpenAIWSHandshake(a, "gpt-6-astra", headers)
	native := *b
	native.borrowCookie = ""
	require.Error(t, s.checkOpenAIWSBinding(a, "gpt-6-astra", &native))
	expired := *b
	expired.borrowExpires = time.Now().Add(-time.Second)
	require.ErrorContains(t, s.checkOpenAIWSBinding(a, "gpt-6-astra", &expired), "connection_borrow_expired")
	a.Credentials = maps.Clone(a.Credentials)
	a.Credentials["access_token"] = "new-private-fixture"
	require.ErrorContains(t, s.checkOpenAIWSBinding(a, "gpt-6-astra", b), "connection_borrow_expired")
	require.Equal(t, 1, p.calls, "per-turn binding checks must not issue a new probe")
}
func TestGatewayBorrowFinalAdmissionPreservesPauseQuotaAndModelGates(t *testing.T) {
	for _, change := range []string{"pause", "quota", "auth", "model", "policy"} {
		t.Run(change, func(t *testing.T) {
			s, a, _ := borrowSafetyService()
			latest := *a
			latest.Extra = maps.Clone(a.Extra)
			s.accountRepo = &turnAdmissionRepo{account: &latest}
			switch change {
			case "pause":
				latest.Schedulable = false
			case "quota":
				until := time.Now().Add(time.Hour)
				latest.RateLimitResetAt = &until
			case "auth":
				latest.Status = StatusError
			case "model":
				latest.Extra[modelRateLimitsKey] = map[string]any{"gpt-6-astra": map[string]any{"rate_limit_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}
			case "policy":
				latest.Extra[GatewayBorrowModelsKey] = []string{}
			}
			_, err := s.admitOpenAITurnWithGroup(t.Context(), a, "gpt-6-astra", 0, false)
			require.Error(t, err)
		})
	}
}

func TestGatewayBorrowReadinessIsPerModelAndNeverFallsBack(t *testing.T) {
	s, a, p := borrowSafetyService()
	require.Empty(t, s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6-astra", false))
	require.Equal(t, "gateway_borrow_route_not_ready", s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6.1-sol", false))
	require.Empty(t, s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6-luna", false), "unprotected native model is unaffected")
	p.snapshot.Targets[0].State = "failed"
	require.Equal(t, "gateway_borrow_route_not_ready", s.gatewayBorrowPolicyReason(t.Context(), a, "gpt-6-astra", false))
	require.Zero(t, p.calls, "candidate filtering never probes or repeats model work")
}

func TestGatewayBorrowFinalHTTPSendRejectsConcurrentPolicyInstall(t *testing.T) {
	s, borrowed, _ := borrowSafetyService()
	selected := *borrowed
	selected.Extra = map[string]any{}
	latest := *borrowed
	latest.Extra = maps.Clone(borrowed.Extra)
	s.accountRepo = &turnAdmissionRepo{account: &latest}
	req, buildErr := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"fixture"}`))
	require.NoError(t, buildErr)
	// Native account was chosen before the worker marked it required-borrow.
	_, err := s.doOpenAIProxyAttempt(req, &selected, runtimeProxyEgress{})
	require.ErrorContains(t, err, "account_binding_changed")
}
func TestGatewayBorrowFinalHTTPRequestKeepsGroupAndLargeNativeBody(t *testing.T) {
	s, a, _ := borrowSafetyService()
	a.Extra = map[string]any{}
	a.GroupIDs = []int64{7}
	latest := *a
	latest.GroupIDs = []int64{8}
	s.accountRepo = &turnAdmissionRepo{account: &latest}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	group := int64(7)
	c.Set("api_key", &APIKey{GroupID: &group})
	body := []byte(`{"model":"gpt-6-luna","input":"` + strings.Repeat("x", 5<<20) + `"}`)
	req := httptest.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(string(body)))
	req = req.WithContext(withOpenAIFinalSendScope(req.Context(), c, body))
	_, err := s.admitOpenAIHTTPRequest(req, a)
	require.ErrorContains(t, err, "group_membership_changed")
	latest.GroupIDs = []int64{7}
	_, err = s.admitOpenAIHTTPRequest(req, a)
	require.NoError(t, err, "normal large native payload does not inherit a new transport limit")
}
func TestGatewayBorrowPluginBypassGuardAndContextModel(t *testing.T) {
	_, a, _ := borrowSafetyService()
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-luna"} {
		req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"`+model+`","input":"fixture"}`))
		mapped, required, err := gatewayBorrowRequiredUpstreamRequest(req, a)
		require.NoError(t, err)
		require.Equal(t, model != "gpt-6-luna", required)
		if required {
			require.Equal(t, model, mapped)
			ctx := WithGatewayBorrowRequiredModel(req.Context(), mapped)
			got, present := GatewayBorrowRequiredModelFromContext(ctx)
			require.True(t, present)
			require.Equal(t, model, got)
		}
	}
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
	req.GetBody = nil
	_, _, err := gatewayBorrowRequiredUpstreamRequest(req, a)
	require.ErrorContains(t, err, "request_model_unavailable")
}

func TestGatewayBorrowPoolCannotReuseNativeOrOtherModelConnection(t *testing.T) {
	s, a, _ := borrowSafetyService()
	headers := http.Header{}
	scope, proxy, release, err := s.prepareGatewayBorrowWS(t.Context(), a, "gpt-6-astra", headers, "")
	require.NoError(t, err)
	defer release()
	native := openAIWSAcquireRequest{Account: a, Headers: headers, ProxyURL: proxy}
	borrowed := native
	borrowed.AnchorScope = scope
	require.NotEqual(t, openAIWSAcquireCompatibility(native), openAIWSAcquireCompatibility(borrowed), "real pool compatibility key separates old native connections")
	binding := s.bindOpenAIWSHandshake(a, "gpt-6-astra", headers)
	require.ErrorContains(t, s.checkOpenAIWSBinding(a, "gpt-6-luna", binding), "connection_borrow_model_changed")
}

func TestGatewayBorrowFinalWireModelIsNotMappedTwice(t *testing.T) {
	s, a, p := borrowSafetyService()
	a.Credentials = maps.Clone(a.Credentials)
	a.Credentials["model_mapping"] = map[string]any{"public": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6-luna"}
	require.True(t, a.RequiresGatewayBorrow("public"))
	require.False(t, a.RequiresGatewayBorrow("gpt-6.1-sol"))
	require.True(t, a.RequiresGatewayBorrowUpstream("gpt-6.1-sol"))
	expires := time.Now().Add(time.Minute)
	p.snapshot.Targets = append(p.snapshot.Targets, AstraRouteStatus{AccountID: a.ID, Model: "gpt-6.1-sol", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expires})
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":"fixture"}`))
	model, required, err := gatewayBorrowRequiredUpstreamRequest(req, a)
	require.NoError(t, err)
	require.True(t, required)
	require.Equal(t, "gpt-6.1-sol", model)
	headers := http.Header{}
	_, _, release, err := s.prepareGatewayBorrowWS(t.Context(), a, "gpt-6.1-sol", headers, "")
	require.NoError(t, err)
	defer release()
	b := s.bindOpenAIWSHandshake(a, "gpt-6.1-sol", headers)
	require.Equal(t, p.cookie, b.borrowCookie)
	require.NoError(t, s.checkOpenAIWSBinding(a, "gpt-6.1-sol", b))
	p.valid = false
	require.Error(t, s.checkOpenAIWSBinding(a, "gpt-6.1-sol", b))
}

func TestGatewayBorrowMultipartCannotUseProtectedTextRoute(t *testing.T) {
	s, a, _ := borrowSafetyService()
	s.accountRepo = &turnAdmissionRepo{account: a}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/images/edits", strings.NewReader("multipart-fixture"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	req = req.WithContext(withOpenAIFinalSendModel(req.Context(), c, "gpt-6-astra"))
	_, err := s.admitOpenAIHTTPRequest(req, a)
	require.ErrorContains(t, err, "gateway_borrow_endpoint_unavailable")
}
