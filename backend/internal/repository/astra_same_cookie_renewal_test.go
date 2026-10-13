package repository

import (
	"context"
	"encoding/base64"
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

func renewalCookie(exp time.Time) string {
	claims := fmt.Sprintf(`{"host":"chat.gateway.unified-42.api.openai.com","exp":%d}`, exp.Unix())
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
}

func nativeRenewalFixture(t *testing.T) (*astraRoutingUpstream, *codexGatewayPinUpstream, *int, *string, func(int)) {
	t.Helper()
	wrapper, _ := accountBorrowFixture(t)
	settings := wrapper.cfg.AstraRouting(t.Context())
	settings.CookiePool.SourceAccountIDs = []int64{299}
	wrapper.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	pool := wrapper.current(t.Context())
	probes, answer := 0, "21"
	cookie := renewalCookie(time.Now().Add(10 * time.Minute))
	maxAge := 180
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		if id == 299 {
			return pinResponse(fmt.Sprintf("__oailb=%s; Path=/; Secure; Max-Age=%d", cookie, maxAge)), nil
		}
		require.Equal(t, int64(300), id)
		require.True(t, service.IsOpenAICodexStateProbeRequest(req.Context()), "fixture sends quality probes only")
		probes++
		return accountBorrowResponse("gpt-6-astra", answer), nil
	}}
	pool.delegate = wrapper.delegate
	acquire := func(age int) {
		maxAge = age
		req, err := http.NewRequestWithContext(service.WithAstraSourceAcquisitionForAccount(t.Context(), 299), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer native-source")
		resp, err := wrapper.Do(req, "source-exit", 299, 1)
		consumeAffinityResponse(t, resp, err)
	}
	return wrapper, pool, &probes, &answer, acquire
}

func TestAccountBorrowFreshSameCookieRecoversExpiredFailure(t *testing.T) {
	wrapper, pool, probes, answer, acquire := nativeRenewalFixture(t)
	acquire(180)
	*answer = "29"
	err := wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "target-exit", 300, 1)
	require.ErrorContains(t, err, "target_quality_failed")
	require.Equal(t, 4, *probes)
	pool.targetMu.Lock()
	old, _ := pool.targetCheck(300, "gpt-6-astra")
	old.expires, old.retryAfter = time.Now().Add(-time.Second), time.Now().Add(-time.Second)
	pool.setTargetCheck(300, "gpt-6-astra", old)
	pool.targetRouteFailures = nil
	pool.targetMu.Unlock()
	// Time has passed beyond both the route lease and independent quiet period.
	wrapper.resetTargetQualityFailure(old.negativeKey)
	pool.mu.Lock()
	route := pool.routes[299]
	route.expires = old.expires
	pool.routes[299] = route
	pool.mu.Unlock()
	*answer = "21"
	acquire(230) // A genuinely completed native source request returns the same Cookie.
	err = wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "target-exit", 300, 1)
	require.NoError(t, err, "fresh acquisition must permit a new IQ result after old evidence expires")
	require.Equal(t, 8, *probes, "a never-proven pair must settle four new Astra answers")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", route.cookie.Value))
	pool.targetMu.Lock()
	next, _ := pool.targetCheck(300, "gpt-6-astra")
	pool.targetMu.Unlock()
	require.True(t, next.expires.After(old.expires))
	require.Equal(t, old.expires, route.expires, "the previously copied lease is never mutated")
}

func TestAccountBorrowFreshSameCookiePrewarmsWithOneNewAnswer(t *testing.T) {
	wrapper, pool, probes, _, acquire := nativeRenewalFixture(t)
	acquire(180)
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "target-exit", 300, 1))
	require.Equal(t, 4, *probes)
	pool.targetMu.Lock()
	old, _ := pool.targetCheck(300, "gpt-6-astra")
	pool.targetMu.Unlock()
	acquire(230)
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "target-exit", 300, 1))
	require.Equal(t, 5, *probes, "fresh acquisition cannot silently reuse the earlier IQ deadline")
	pool.targetMu.Lock()
	next, _ := pool.targetCheck(300, "gpt-6-astra")
	pool.targetMu.Unlock()
	require.True(t, next.expires.After(old.expires), "one new correct answer proves the new lease on the old pair")
	require.Equal(t, 4, old.attempts, "the old proof remains a detached value")
}

func TestCodexGatewayCookieLeaseRespectsAbsoluteExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	exp := now.Add(70 * time.Second)
	cookie := renewalCookie(exp)
	response := pinResponse("__oailb=" + cookie + "; Path=/; Secure; Max-Age=3600")
	route := codexGatewayRouteFromResponse(response, "/backend-api/codex/responses", now, 230)
	require.NotNil(t, route)
	require.Equal(t, exp, route.expires, "replayed Max-Age cannot exceed the Cookie's unchanged JWT exp")
	response = pinResponse("__oailb=" + renewalCookie(now.Add(-time.Second)) + "; Path=/; Secure; Max-Age=3600")
	stale := codexGatewayRouteFromResponse(response, "/backend-api/codex/responses", now, 230)
	require.True(t, stale == nil || stale.cookie.Value == "" || !now.Before(stale.expires))
	// Attribute expiry remains a bound even when Max-Age is also supplied.
	response = pinResponse("__oailb=" + renewalCookie(now.Add(time.Hour)) + "; Path=/; Secure; Max-Age=3600; Expires=" + exp.UTC().Format(http.TimeFormat))
	route = codexGatewayRouteFromResponse(response, "/backend-api/codex/responses", now, 230)
	require.NotNil(t, route)
	require.True(t, exp.Equal(route.expires))
	_, _ = io.Copy(io.Discard, response.Body)
}
