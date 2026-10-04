package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayBorrowModelsNeverReuseAstraWitnessForSol(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	cfg.Gateway.CodexGatewayPin.Models = []string{"gpt-6-astra", "gpt-6.1-sol"}
	shots := map[string]int{}
	business := 0
	wrapper := &astraRoutingUpstream{cfg: cfg}
	wrapper.delegate = gatewayPinDelegate{call: func(r *http.Request, p string, id int64, n int, f *tlsfingerprint.Profile) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		model := "gpt-6-astra"
		if strings.Contains(string(raw), "gpt-6.1-sol") {
			model = "gpt-6.1-sol"
		}
		if strings.Contains(string(raw), "Reply with OK.") {
			shots[model]++
			token := "fixture-first"
			if shots[model]%2 == 0 {
				token = ""
				if model == "gpt-6.1-sol" {
					token = "fixture-new"
				}
			}
			response := pinResponse("")
			response.Header.Set("X-Codex-Turn-State", token)
			response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"" + model + "\",\"status\":\"completed\",\"output\":[{\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"))
			return response, nil
		}
		if strings.Contains(string(raw), "只输出最终整数") {
			return pinResponse(""), nil
		}
		business++
		return pinResponse(""), nil
	}}
	pool := wrapper.current(t.Context())
	now := time.Now()
	route := codexGatewayRouteFromResponse(pinResponse("__oailb=fixture; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
	pool.recordSource(299, now, route, true)
	send := func(model string) error {
		r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"`+model+`","input":"business"}`))
		r.Header.Set("Authorization", "Bearer fixture-target")
		r.Header.Set("ChatGPT-Account-ID", "fixture-id")
		response, err := wrapper.Do(r, "target-proxy", 300, 1)
		if response != nil {
			_ = response.Body.Close()
		}
		return err
	}
	require.NoError(t, send("gpt-6-astra"))
	require.NoError(t, send("gpt-6-astra"))
	require.Equal(t, 2, shots["gpt-6-astra"])
	require.ErrorContains(t, send("gpt-6.1-sol"), "target_probe_degraded")
	require.Equal(t, 2, shots["gpt-6.1-sol"])
	require.Equal(t, 2, business, "failed Sol validation must not dispatch a user request")
	require.NoError(t, send("gpt-6-astra"), "Sol rejection must retain independent Astra pass")
	require.Equal(t, 2, shots["gpt-6-astra"])
	pool.targetProbeMu.Lock()
	require.NoError(t, send("gpt-6-astra"), "another slow target probe must not block a cached valid Astra route")
	pool.targetProbeMu.Unlock()
	largeCtx := service.WithGatewayBorrowWireModel(t.Context(), "gpt-6-astra")
	largeCtx = service.WithGatewayBorrowRequiredModel(largeCtx, "gpt-6-astra")
	large, _ := http.NewRequestWithContext(largeCtx, "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"`+strings.Repeat("a", 5<<20)+`"}`))
	large.Header.Set("Authorization", "Bearer fixture-target")
	large.Header.Set("ChatGPT-Account-ID", "fixture-id")
	largeResponse, largeErr := wrapper.Do(large, "target-proxy", 300, 1)
	require.NoError(t, largeErr, "trusted final-model context preserves large borrowed native payloads")
	_ = largeResponse.Body.Close()
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "fixture"))
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "fixture"))
}

func TestGatewayBorrowTargetOtherModelRetainsNative(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	called := 0
	wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(r *http.Request, p string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		called++
		require.Empty(t, r.Header.Get("Cookie"))
		require.Equal(t, "target-proxy", p)
		return pinResponse(""), nil
	}}}
	r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-luna"}`))
	resp, err := wrapper.Do(r, "target-proxy", 300, 1)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 1, called)
	large := `{"model":"gpt-6-luna","input":"` + strings.Repeat("a", 5<<20) + `"}`
	ctx := service.WithGatewayBorrowWireModel(t.Context(), "gpt-6-luna")
	r, _ = http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(large))
	resp, err = wrapper.Do(r, "target-proxy", 300, 1)
	require.NoError(t, err, "a borrow target may still send large native Luna requests")
	_ = resp.Body.Close()
	require.Equal(t, 2, called)
}

func TestGatewayBorrowEmptyAutomaticDonorFailsClosed(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, CookiePool: config.CodexGatewayPinConfig{Enabled: true, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	called := 0
	wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		called++
		return pinResponse(""), nil
	}}}
	r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6.1-sol"}`))
	_, err := wrapper.Do(r, "target-proxy", 300, 1)
	require.Error(t, err)
	require.Zero(t, called, "no donor may not silently send native business traffic")
}

func TestGatewayBorrowUninspectableTargetBodyCannotFailOpen(t *testing.T) {
	for _, kind := range []string{"no_getbody", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.CodexGatewayPin = pinConfig()
			called := 0
			wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
				called++
				return pinResponse(""), nil
			}}}
			body := `{"model":"gpt-6-astra","input":"` + strings.Repeat("a", 4<<20) + `"}`
			r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(body))
			if kind == "no_getbody" {
				r.GetBody = nil
			}
			_, err := wrapper.Do(r, "target-proxy", 300, 1)
			require.ErrorContains(t, err, "borrow_request_model_unavailable")
			require.Zero(t, called)
		})
	}
}
