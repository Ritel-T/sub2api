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
