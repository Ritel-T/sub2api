package repository

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func targetSourceOrderFixture(t *testing.T, sources []int64) (*astraRoutingUpstream, *codexGatewayPinUpstream) {
	t.Helper()
	wrapper, _ := accountBorrowFixture(t)
	settings := wrapper.cfg.AstraRouting(t.Context())
	settings.CookiePool.SourceAccountIDs = sources
	wrapper.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	pool := wrapper.current(t.Context())
	for _, source := range sources {
		accountBorrowAdd(t, pool, source, fmt.Sprintf("cookie-%d", source), 220*time.Second)
	}
	return wrapper, pool
}

func TestAutomaticBorrowTargetExploresNextSourceAfterOverloadQuiet(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	calls, healthyShots := 0, 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		calls++
		cookie, _ := req.Cookie("__oailb")
		if cookie.Value == pool.routes[299].cookie.Value {
			return overloadProbeResponse("server_is_overloaded"), nil
		}
		healthyShots++
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1), "target_probe_overloaded")
	require.Equal(t, 1, calls)
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1), "target_probe_overloaded")
	require.Equal(t, 1, calls, "different donor cannot evade active account/exit overload quiet")
	// Simulate only elapsed provider quiet, retaining the actual source attempts.
	wrapper.quotaMu.Lock()
	for key, wait := range wrapper.quotaBackoff {
		wait.until = time.Now().Add(-time.Second)
		wrapper.quotaBackoff[key] = wait
	}
	wrapper.quotaMu.Unlock()
	pool.targetMu.Lock()
	pool.targetRouteFailures = nil
	pool.targetMu.Unlock()
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1), "after quiet expires an untried donor gets the next bounded probe")
	require.Equal(t, 5, calls)
	require.Equal(t, 4, healthyShots)
	pool.targetMu.Lock()
	check, _ := pool.targetCheck(300, "gpt-6-astra")
	pool.targetMu.Unlock()
	require.Equal(t, int64(298), check.sourceID)
}

func TestAutomaticBorrowForcedRenewalPrefersProvenTargetSource(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	calls := []string{}
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		cookie, _ := req.Cookie("__oailb")
		calls = append(calls, cookie.Value)
		if cookie.Value == pool.routes[299].cookie.Value {
			return overloadProbeResponse("server_is_overloaded"), nil
		}
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	pool.mu.Lock()
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	pool.mu.Unlock()
	_, _, _, release, err := wrapper.targetRouteOnce(request.Clone(context.WithValue(request.Context(), autoBorrowRouteKey{}, selected)), "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Len(t, calls, 4)
	accountBorrowAdd(t, pool, 298, "renewed-proven-cookie", 220*time.Second)
	calls = nil
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), request, "exit", 300, 1), "forced warm must renew this target's successful pair before exploring config-first donor")
	require.Equal(t, []string{"renewed-proven-cookie"}, calls)
}

func TestAutomaticBorrowSourceAttemptOrderIsCredentialAndTargetScoped(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	calls := []string{}
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		cookie, _ := req.Cookie("__oailb")
		calls = append(calls, cookie.Value)
		return overloadProbeResponse("server_is_overloaded"), nil
	}}
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1))
	first := pool.routes[299].cookie.Value
	require.Equal(t, []string{first}, calls)
	changed := accountBorrowRequest(t, "gpt-6-astra", "")
	changed.Header.Set("Authorization", "Bearer new-credential")
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), changed, "exit", 300, 1))
	require.Equal(t, []string{first, first}, calls, "a changed credential starts its own target exploration scope")
}

func TestAutomaticBorrowSourceOrderScopeAndBoundedMemory(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	now := time.Now()
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	_, key := targetBorrowQuietKeys(request, 300, "gpt-6-astra", "exit", 299, nil)
	pool.rememberTargetSourceAttempt(key, now)
	require.Equal(t, []int64{298, 299}, wrapper.automaticTargetSourceOrder(request, "exit", 300, "gpt-6.1-sol", nil, pool, astraTargetValidation{}, false), "linked Sol exploration reuses only this account's Astra attempt scope")
	require.Equal(t, []int64{299, 298}, wrapper.automaticTargetSourceOrder(request, "new-exit", 300, "gpt-6-astra", nil, pool, astraTargetValidation{}, false))
	require.Equal(t, []int64{299, 298}, wrapper.automaticTargetSourceOrder(request, "exit", 301, "gpt-6-astra", nil, pool, astraTargetValidation{}, false))
	legacy := &codexGatewayPinUpstream{config: pool.config, routes: pool.routes, targetSourceAttempts: pool.targetSourceAttempts}
	legacy.config.QualityMode = ""
	require.Equal(t, []int64{299, 298}, wrapper.automaticTargetSourceOrder(request, "exit", 300, "gpt-6.1-sol", nil, legacy, astraTargetValidation{}, false), "legacy models retain independent exploration")
	for i := range 4097 {
		next := key
		next.accountID = int64(1000 + i)
		pool.rememberTargetSourceAttempt(next, now.Add(time.Duration(i)*time.Nanosecond))
	}
	require.LessOrEqual(t, len(pool.targetSourceAttempts), 4096)
	pool.rememberTargetSourceAttempt(key, now.Add(2*time.Hour))
	require.Len(t, pool.targetSourceAttempts, 1, "old exploration timestamps are pruned without keeping credentials")
}

func TestAutomaticBorrowSourceOrderRejectsStaleProvenIdentity(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	_, oldKey := targetBorrowQuietKeys(request, 300, "gpt-6-astra", "exit", 298, nil)
	old := astraTargetValidation{passed: true, sourceID: 298, negativeKey: oldKey}
	pool.targetMu.Lock()
	pool.rememberTargetSourcePreferenceLocked(oldKey, time.Now())
	pool.targetMu.Unlock()
	require.Equal(t, []int64{298, 299}, wrapper.automaticTargetSourceOrder(request, "exit", 300, "gpt-6-astra", nil, pool, old, true))
	pool.config.IPAffinity = true
	pool.routes[299] = codexGatewayRoute{cookie: pool.routes[299].cookie, proxy: "native-source-exit", expires: pool.routes[299].expires}
	pool.routes[298] = codexGatewayRoute{cookie: pool.routes[298].cookie, proxy: "changed-proven-source-exit", expires: pool.routes[298].expires}
	require.Equal(t, []int64{299, 298}, wrapper.automaticTargetSourceOrder(request, "exit", 300, "gpt-6-astra", nil, pool, old, true), "changed donor exit cannot inherit proven preference")
	pool.config.IPAffinity = false
	request.Header.Set("Authorization", "Bearer replacement-target")
	require.Equal(t, []int64{299, 298}, wrapper.automaticTargetSourceOrder(request, "exit", 300, "gpt-6-astra", nil, pool, old, true))
	request.Header.Set("Authorization", "Bearer fixture-target")
	require.Equal(t, []int64{299, 298}, wrapper.automaticTargetSourceOrder(request, "another-exit", 300, "gpt-6-astra", nil, pool, old, true))
}
