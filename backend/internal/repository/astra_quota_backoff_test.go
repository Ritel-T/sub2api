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
	"github.com/stretchr/testify/require"
)

func TestAutomaticBorrowQuotaDoesNotRetryAcrossCookieSourceOrRevision(t *testing.T) {
	for _, status := range []int{401, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299, 298}, TargetAccountIDs: []int64{300}}}
			cfg := &config.Config{}
			cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
			calls := 0
			wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"usage_limit_reached"}}`))}, nil
			}}}
			wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { return nil })
			seed := func(id int64, value string) {
				now := time.Now()
				route := codexGatewayRouteFromResponse(pinResponse("__oailb="+value+"; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
				wrapper.current(t.Context()).recordSource(id, now, route, true)
			}
			request := func(token, proxy string) error {
				r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("ChatGPT-Account-ID", "fixture-id")
				_, err := wrapper.Do(r, proxy, 300, 1)
				return err
			}
			seed(299, "first")
			seed(298, "backup")
			require.Error(t, request("token", "exit"))
			require.Equal(t, 1, calls)
			seed(299, "fresh")
			require.Error(t, request("token", "exit"))
			require.Equal(t, 1, calls, "new Cookie must not clear credential quota/auth quiet period")
			settings.Revision = "two"
			seed(299, "revision")
			seed(298, "revisionbackup")
			require.Error(t, request("token", "exit"))
			require.Equal(t, 1, calls, "pool reset cannot retry a still-cooling credential")
			require.Error(t, request("new-token", "exit"))
			require.Equal(t, 2, calls, "changed credentials own a new scope")
			require.Error(t, request("new-token", "new-exit"))
			require.Equal(t, 3, calls, "changed actual exit owns a new scope")
		})
	}
}

func TestAutomaticBorrowBadSourceCookieRefreshDoesNotSpendMoreProbes(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299, 298}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	calls := 0
	wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(r *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		calls++
		response := pinResponse("")
		if r.Header.Get("X-Codex-Turn-State") != "" {
			response.Header.Set("X-Codex-Turn-State", "different")
		}
		return response, nil
	}}}
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { return nil })
	seed := func(id int64, value string) {
		now := time.Now()
		route := codexGatewayRouteFromResponse(pinResponse("__oailb="+value+"; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
		wrapper.current(t.Context()).recordSource(id, now, route, true)
	}
	request := func(token string) error {
		r, _ := http.NewRequestWithContext(t.Context(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
		r.Header.Set("Authorization", "Bearer "+token)
		_, err := wrapper.Do(r, "target", 300, 1)
		return err
	}
	seed(299, "first")
	require.Error(t, request("token"))
	require.Equal(t, 2, calls)
	seed(299, "fresh")
	require.Error(t, request("token"))
	require.Equal(t, 2, calls, "same donor newCookie cannot repeat failed stable-ticket tests")
	seed(298, "other")
	require.Error(t, request("token"))
	require.Equal(t, 4, calls, "different donor is immediately eligible")
	require.Error(t, request("new-token"))
	require.Equal(t, 8, calls, "new credentials can revalidate each donor independently")
}

func TestAdaptiveBorrowQualityBackoffIsBoundedAndIdentityScoped(t *testing.T) {
	wrapper := &astraRoutingUpstream{}
	req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set("Authorization", "Bearer fixture")
	quota, key := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 299, nil)
	now := time.Now()
	for _, want := range []time.Duration{180 * time.Second, 360 * time.Second, 720 * time.Second, 900 * time.Second, 900 * time.Second} {
		until := wrapper.rememberTargetQualityFailure(key, "target_probe_degraded", now)
		require.Equal(t, want, until.Sub(now))
		before := wrapper.quotaBackoff[key].failures
		_, waiting := wrapper.targetQuotaWait(key, now.Add(time.Second))
		require.True(t, waiting)
		require.Equal(t, before, wrapper.quotaBackoff[key].failures, "quiet cache read cannot increase tier")
		require.Equal(t, until, wrapper.rememberTargetQualityFailure(key, "target_probe_degraded", now.Add(time.Second)), "duplicate in quiet window cannot slide it")
		now = until.Add(time.Second)
	}
	wrapper.rememberTargetQuota(quota, "target_probe_rate_limited", now.Add(300*time.Second))
	require.Zero(t, wrapper.quotaBackoff[quota].failures)
	req.Header.Set("Authorization", "Bearer changed")
	_, newKey := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 299, nil)
	require.Equal(t, 180*time.Second, wrapper.rememberTargetQualityFailure(newKey, "target_quality_failed", now).Sub(now))
	_, otherSource := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 298, nil)
	_, waiting := wrapper.targetQuotaWait(otherSource, now)
	require.False(t, waiting)
	wrapper.resetTargetQualityFailure(key)
	require.Equal(t, 180*time.Second, wrapper.rememberTargetQualityFailure(key, "target_probe_degraded", now).Sub(now), "success resets source-specific failure history")
	_, waiting = wrapper.targetQuotaWait(key, now.Add(time.Hour+time.Second))
	require.False(t, waiting)
	_, exists := wrapper.quotaBackoff[key]
	require.False(t, exists, "old counts are collected after one hour")
	for i := 0; i < 4100; i++ {
		k := astraTargetQuotaKey{accountID: int64(i + 1), model: "gpt-6-astra"}
		wrapper.rememberTargetQualityFailure(k, "target_quality_failed", now)
	}
	require.LessOrEqual(t, len(wrapper.quotaBackoff), 4096)
}
