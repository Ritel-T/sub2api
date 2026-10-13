package repository

import (
	"context"
	"fmt"
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

func overloadProbeResponse(code string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":%q,"type":"server_error","message":"private upstream detail"}}}`+"\n\n", code)))}
}

func TestAutomaticBorrowOverloadQuietAcrossSourceCookieAndRevision(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, QualityMode: config.GatewayBorrowAccountQualityMode, Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299, 298}, TargetAccountIDs: []int64{300}, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	calls := 0
	wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		calls++
		return overloadProbeResponse("server_is_overloaded"), nil
	}}}
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { return nil })
	seed := func(id int64, cookie string) {
		now := time.Now()
		route := codexGatewayRouteFromResponse(pinResponse("__oailb="+cookie+"; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
		wrapper.current(t.Context()).recordSource(id, now, route, true)
	}
	request := func(token, proxy, model string) error {
		req, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"`+model+`"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("ChatGPT-Account-ID", "workspace")
		return wrapper.VerifyAstraGatewayTargetForModel(t.Context(), req, proxy, 300, 1, model)
	}
	seed(299, "first")
	seed(298, "backup")
	before := time.Now()
	require.EqualError(t, request("token", "exit", "gpt-6-astra"), "target_probe_overloaded")
	require.Equal(t, 1, calls, "explicit provider overload stops this donor scan immediately")
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("ChatGPT-Account-ID", "workspace")
	quietKey, _ := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 0, nil)
	quiet, waiting := wrapper.targetQuotaWait(quietKey, time.Now())
	require.True(t, waiting)
	require.Equal(t, "target_probe_overloaded", quiet.reason)
	require.Zero(t, quiet.failures, "overload does not increase quality failure penalties")
	require.WithinDuration(t, before.Add(time.Minute), quiet.until, time.Second)
	seed(299, "renewed")
	require.EqualError(t, request("token", "exit", "gpt-6.1-sol"), "target_probe_overloaded")
	require.Equal(t, 1, calls, "Astra quality gate quiet applies to shared Sol validation")
	settings.Revision = "two"
	seed(299, "after-save")
	seed(298, "other-donor")
	require.EqualError(t, request("token", "exit", "gpt-6-astra"), "target_probe_overloaded")
	require.Equal(t, 1, calls, "save and changed Cookie/source cannot evade a quiet credential/exit")
	quietAfter, _ := wrapper.targetQuotaWait(quietKey, time.Now())
	require.Equal(t, quiet.until, quietAfter.until, "quiet reads do not slide expiry")
	require.Error(t, request("new-token", "exit", "gpt-6-astra"))
	require.Equal(t, 2, calls, "new credential is independent")
	require.Error(t, request("new-token", "new-exit", "gpt-6-astra"))
	require.Equal(t, 3, calls, "new actual exit is independent")
	_, active := wrapper.targetQuotaWait(quietKey, quiet.until.Add(time.Millisecond))
	require.False(t, active, "the fixed quiet period expires naturally")
}

func TestBorrowOverloadClassificationUsesOnlyExactProviderCode(t *testing.T) {
	for _, code := range []string{"server_is_overloaded", "server_error", "internal_error", "server_is_overloaded_other"} {
		t.Run(code, func(t *testing.T) {
			wrapper, pool := accountBorrowFixture(t)
			settings := wrapper.cfg.AstraRouting(t.Context())
			settings.CookiePool.SourceAccountIDs = []int64{299}
			wrapper.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
			pool = wrapper.current(t.Context())
			calls := 0
			wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
				calls++
				return overloadProbeResponse(code), nil
			}}
			accountBorrowAdd(t, pool, 299, "candidate", 220*time.Second)
			err := wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 1)
			require.Error(t, err)
			pool.targetMu.Lock()
			check, _ := pool.targetCheck(300, "gpt-6-astra")
			pool.targetMu.Unlock()
			require.Equal(t, 1, calls)
			if code == "server_is_overloaded" {
				require.EqualError(t, err, "target_probe_overloaded")
				require.Equal(t, service.OpenAICodexStateFailureOverloaded, check.failureCode)
				require.Equal(t, 1, check.attempts)
			} else {
				require.Equal(t, service.OpenAICodexStateFailureStreamError, check.failureCode)
				require.NotEqual(t, "target_probe_overloaded", err.Error())
				_, waiting := wrapper.targetQuotaWait(check.quotaKey, time.Now())
				require.False(t, waiting, "generic/unknown server failures get no overload quiet")
			}
			require.NotContains(t, err.Error(), "private")
			require.Zero(t, check.correct)
		})
	}
}
