package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func accountBorrowFixture(t *testing.T) (*astraRoutingUpstream, *codexGatewayPinUpstream) {
	t.Helper()
	settings := config.AstraRoutingSettings{AutoQuality: true, QualityMode: config.GatewayBorrowAccountQualityMode, Revision: "account-policy", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299, 298}, TargetAccountIDs: []int64{300}, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	wrapper := &astraRoutingUpstream{cfg: cfg}
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { return nil })
	return wrapper, wrapper.current(t.Context())
}
func accountBorrowRequest(t *testing.T, model, state string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"`+model+`"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer fixture-target")
	req.Header.Set("ChatGPT-Account-ID", "target-identity")
	req.Header.Set("X-Codex-Turn-State", state)
	return req
}
func accountBorrowAdd(t *testing.T, pool *codexGatewayPinUpstream, id int64, value string, remaining time.Duration) {
	t.Helper()
	now := time.Now()
	route := codexGatewayRouteFromResponse(pinResponse("__oailb="+value+"; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
	route.expires = now.Add(remaining)
	pool.recordSource(id, now, route, true)
}
func accountBorrowResponse(model, answer string) *http.Response {
	response := pinResponse("")
	response.Header.Set("X-Codex-Turn-State", "resigned-is-not-quality-failure")
	response.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"model":"` + model + `","status":"completed","output":[{"content":[{"type":"output_text","text":"` + answer + `"}]}]}}` + "\n\n"))
	return response
}

func TestAccountBorrowSharedProofRenewalAndIdentity(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	probes, business := 0, 0
	answer := "21"
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		model := gjson.GetBytes(raw, "model").String()
		if gjson.GetBytes(raw, "input.0.content.0.text").String() == "" {
			business++
			return accountBorrowResponse(model, "OK"), nil
		}
		probes++
		require.Equal(t, "gpt-6-astra", model, "Sol must never get an intelligence probe")
		return accountBorrowResponse(model, answer), nil
	}}
	accountBorrowAdd(t, pool, 299, "old", 220*time.Second)
	response, err := wrapper.Do(accountBorrowRequest(t, "gpt-6.1-sol", "sol-business-state"), "exit", 300, 2)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, 4, probes)
	require.Equal(t, 1, business)
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"))
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "old"))
	oldExpiry := wrapper.CodexGatewayPinWSBindingExpiryForModel(t.Context(), 300, "gpt-6.1-sol", "old")
	response, err = wrapper.Do(accountBorrowRequest(t, "gpt-6-astra", "different-business-state"), "exit", 300, 2)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, 4, probes, "other model and business turn state reuse shared proof")
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	require.Equal(t, 4, probes, "same positive Cookie is not retested on background force")
	require.Equal(t, oldExpiry, wrapper.CodexGatewayPinWSBindingExpiryForModel(t.Context(), 300, "gpt-6-astra", "old"))
	// A fresh Cookie on the proven pair uses just the first Astra candy answer.
	accountBorrowAdd(t, pool, 299, "renewed", 220*time.Second)
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	require.Equal(t, 5, probes)
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "renewed"))
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "old"), "existing response route remains usable until its own deadline")
	// Another donor starts with the complete four shots even on the same exit.
	accountBorrowAdd(t, pool, 298, "other-donor", 220*time.Second)
	req := accountBorrowRequest(t, "gpt-6.1-sol", "")
	pool.mu.Lock()
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	pool.mu.Unlock()
	req = req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected))
	_, _, _, release, err := wrapper.targetRouteOnce(req, "exit", 300, 2, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, 9, probes)
	// A changed target credential is a new identity, never a cached proof.
	req = accountBorrowRequest(t, "gpt-6.1-sol", "")
	req.Header.Set("Authorization", "Bearer refreshed-target")
	req = req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected))
	_, _, _, release, err = wrapper.targetRouteOnce(req, "exit", 300, 2, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, 13, probes)
}

func TestAccountBorrowFailedReplacementKeepsOldSharedProof(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	probes := 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		model := gjson.GetBytes(raw, "model").String()
		cookie, _ := req.Cookie("__oailb")
		if gjson.GetBytes(raw, "input.0.content.0.text").String() == "" {
			return accountBorrowResponse(model, "OK"), nil
		}
		probes++
		answer := "21"
		if cookie.Value == "bad" {
			answer = "29"
		}
		return accountBorrowResponse(model, answer), nil
	}}
	accountBorrowAdd(t, pool, 299, "old", 220*time.Second)
	response, err := wrapper.Do(accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2)
	require.NoError(t, err)
	_ = response.Body.Close()
	accountBorrowAdd(t, pool, 299, "bad", 220*time.Second)
	err = wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2)
	require.EqualError(t, err, "target_quality_failed")
	require.Equal(t, 8, probes, "wrong renewal first answer expands to the complete four")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"))
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "old"))
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "bad"))
	response, err = wrapper.Do(accountBorrowRequest(t, "gpt-6.1-sol", ""), "exit", 300, 2)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, 8, probes, "preserved Cookie dispatches without another probe")
	pool.targetMu.Lock()
	for k, v := range pool.targetRoutePasses {
		v.expires = time.Now().Add(-time.Second)
		pool.targetRoutePasses[k] = v
	}
	check, _ := pool.targetCheck(300, "gpt-6-astra")
	check.expires = time.Now().Add(-time.Second)
	pool.setTargetCheck(300, "gpt-6-astra", check)
	pool.targetMu.Unlock()
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "old"))
}

func TestAccountBorrowCrossModelProbeConcurrency(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	accountBorrowAdd(t, pool, 299, "fixture", 220*time.Second)
	started, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		once.Do(func() { close(started); <-proceed })
		raw, _ := io.ReadAll(req.Body)
		return accountBorrowResponse(gjson.GetBytes(raw, "model").String(), "21"), nil
	}}
	done := make(chan error, 1)
	go func() {
		response, err := wrapper.Do(accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	<-started
	_, err := wrapper.Do(accountBorrowRequest(t, "gpt-6.1-sol", ""), "exit", 300, 2)
	require.EqualError(t, err, "target_validation_in_progress")
	close(proceed)
	require.NoError(t, <-done)
}

func TestAccountBorrowRenewalStartsAt120OnlyForProvenSource(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	accountBorrowAdd(t, pool, 299, "proven", 110*time.Second)
	accountBorrowAdd(t, pool, 298, "candidate", 110*time.Second)
	pool.targetMu.Lock()
	pool.setTargetCheck(300, "gpt-6-astra", astraTargetValidation{passed: true, sourceID: 299, expires: time.Now().Add(110 * time.Second)})
	pool.targetMu.Unlock()
	pool.mu.Lock()
	for id, status := range pool.statuses {
		at := time.Now().Add(-time.Minute)
		status.CheckedAt = &at
		pool.statuses[id] = status
	}
	pool.mu.Unlock()
	var calls []int64
	wrapper.SetAstraGatewayPreparer(func(_ context.Context, id int64) error { calls = append(calls, id); return nil })
	require.NoError(t, wrapper.PrepareAstraGateway(t.Context()))
	require.Equal(t, []int64{299}, calls, "only a proven source receives the longer renewal window")
}

func TestAccountBorrowReissuedSameCookieNeverExtendsExpiredWitness(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		return accountBorrowResponse(gjson.GetBytes(raw, "model").String(), "21"), nil
	}}
	accountBorrowAdd(t, pool, 299, "fixed", 220*time.Second)
	response, err := wrapper.Do(accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2)
	require.NoError(t, err)
	_ = response.Body.Close()
	pool.targetMu.Lock()
	check, _ := pool.targetCheck(300, "gpt-6-astra")
	check.expires = time.Now().Add(-time.Second)
	pool.setTargetCheck(300, "gpt-6-astra", check)
	for k, v := range pool.targetRoutePasses {
		v.expires = check.expires
		pool.targetRoutePasses[k] = v
	}
	pool.targetMu.Unlock()
	pool.mu.Lock()
	route := pool.routes[299]
	route.expires = time.Now().Add(220 * time.Second) // A repeated source response must not revive the witness.
	pool.routes[299] = route
	pool.mu.Unlock()
	req := accountBorrowRequest(t, "gpt-6.1-sol", "")
	req.Header.Set("Authorization", "Bearer refreshed-target")
	_, err = wrapper.Do(req, "exit", 300, 2)
	require.EqualError(t, err, "borrow_route_expired")
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "fixed"))
}
