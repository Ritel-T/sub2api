package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAutomaticBorrowRejectsBadDonorAndKeepsOldProof(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299, 298}, TargetAccountIDs: []int64{300}, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	wrapper := &astraRoutingUpstream{cfg: cfg}
	business := 0
	wrapper.delegate = gatewayPinDelegate{call: func(r *http.Request, p string, id int64, n int, f *tlsfingerprint.Profile) (*http.Response, error) {
		cookie, _ := r.Cookie("__oailb")
		raw, _ := io.ReadAll(r.Body)
		model := gjson.GetBytes(raw, "model").String()
		probe := gjson.GetBytes(raw, "input.0.content.0.text").String()
		if probe == "" {
			business++
			return pinResponse(""), nil
		}
		resp := pinResponse("")
		resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"model":"` + model + `","status":"completed","output":[{"content":[{"type":"output_text","text":"21"}]}]}}` + "\n\n"))
		if cookie.Value == "bad" && r.Header.Get("X-Codex-Turn-State") != "" {
			resp.Header.Set("X-Codex-Turn-State", "changed")
		}
		return resp, nil
	}}
	pool := wrapper.current(t.Context())
	add := func(id int64, value string) {
		now := time.Now()
		route := codexGatewayRouteFromResponse(pinResponse("__oailb="+value+"; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
		pool.recordSource(id, now, route, true)
	}
	add(299, "old")
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { return nil })
	request := func(ctx context.Context) *http.Request {
		r, _ := http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
		r.Header.Set("Authorization", "Bearer target")
		return r
	}
	resp, err := wrapper.Do(request(t.Context()), "target", 300, 1)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"))
	add(299, "bad")
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), request(t.Context()), "target", 300, 1), "replacement candidate from same source must fail and enter source backoff")
	before := business
	retained, retainErr := wrapper.Do(request(t.Context()), "target", 300, 1)
	require.NoError(t, retainErr, "replacement failure cannot block exact old verified Cookie until its own deadline")
	_ = retained.Body.Close()
	require.Equal(t, before+1, business, "old pass sends only the business request without revalidation")
	add(298, "good")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"), "source candidate replacement cannot revoke an unexpired proof")
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), request(t.Context()), "target", 300, 1), "bad source must not prevent next configured donor validation")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "good"))
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"), "good replacement preserves old response proof until its deadline")
	require.Equal(t, 2, business, "candidate comparisons never dispatch a business request")
	pool.targetMu.Lock()
	check, _ := pool.targetCheck(300, "gpt-6-astra")
	check.expires = time.Now().Add(-time.Second)
	pool.setTargetCheck(300, "gpt-6-astra", check)
	for k, v := range pool.targetRoutePasses {
		v.expires = time.Now().Add(-time.Second)
		pool.targetRoutePasses[k] = v
	}
	pool.targetMu.Unlock()
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"))
	_ = service.OpenAICodexStateHealthy
}

func TestAutomaticPrepareVisitsAllDonorsBounded(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{1, 2, 3, 4}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	wrapper := &astraRoutingUpstream{cfg: cfg}
	pool := wrapper.current(t.Context())
	var calls []int64
	wrapper.SetAstraGatewayPreparer(func(ctx context.Context, id int64) error {
		calls = append(calls, id)
		now := time.Now()
		route := codexGatewayRouteFromResponse(pinResponse("__oailb=fixture; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
		pool.recordSource(id, now, route, true)
		return nil
	})
	require.NoError(t, wrapper.PrepareAstraGateway(t.Context()))
	require.Equal(t, []int64{1, 2, 3}, calls, "available first donor cannot stop preparing bounded backups")
	require.NoError(t, wrapper.PrepareAstraGateway(t.Context()))
	require.Equal(t, []int64{1, 2, 3, 4}, calls, "fresh donors do not consume another probe budget")
}

func TestAutomaticBorrowBackoffDoesNotConsumeFourthDonorBudget(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{1, 2, 3, 4}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	shots := 0
	wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		shots++
		cookie, err := req.Cookie("__oailb")
		require.NoError(t, err)
		require.Equal(t, "fourth", cookie.Value, "backoff donors may not send even one probe")
		return pinResponse(""), nil
	}}}
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error {
		t.Fatal("fresh prepared candidates must not mint again")
		return nil
	})
	pool := wrapper.current(t.Context())
	now := time.Now()
	r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
	r.Header.Set("Authorization", "Bearer target")
	for _, id := range []int64{1, 2, 3, 4} {
		value := "bad"
		if id == 4 {
			value = "fourth"
		}
		route := codexGatewayRouteFromResponse(pinResponse("__oailb="+value+"; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
		pool.recordSource(id, now, route, true)
		if id != 4 {
			_, key := targetBorrowQuietKeys(r, 300, "gpt-6-astra", "target", id, nil)
			wrapper.rememberTargetQuota(key, "target_probe_degraded", now.Add(180*time.Second))
		}
	}
	response, err := wrapper.Do(r, "target", 300, 1)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, 7, shots, "only fourth donor's six probes and one business request are emitted")
	response, err = wrapper.Do(r, "target", 300, 1)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, 8, shots, "cached pass avoids all donor validation repeats")
}
