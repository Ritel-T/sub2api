package repository

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestAutomaticBorrowProvenSourcePreferenceSurvivesOtherSourceFailure(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298, 297})
	mode := "pass"
	seen := []string{}
	provenCookie := pool.routes[298].cookie.Value
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		cookie, _ := req.Cookie("__oailb")
		seen = append(seen, cookie.Value)
		if mode == "other-failure" {
			return overloadProbeResponse("server_is_overloaded"), nil
		}
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	_, _, _, release, err := wrapper.targetRouteOnce(req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected)), "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Len(t, seen, 4)
	pool.targetMu.Lock()
	old, _ := pool.targetCheck(300, "gpt-6-astra")
	old.expires = time.Now().Add(-time.Second)
	pool.setTargetCheck(300, "gpt-6-astra", old)
	for key, check := range pool.targetRoutePasses {
		check.expires = old.expires
		pool.targetRoutePasses[key] = check
	}
	pool.targetMu.Unlock()
	pool.mu.Lock()
	route := pool.routes[298]
	route.expires = time.Now().Add(80 * time.Second) // Successful source cannot currently fit the existing candidate window.
	pool.routes[298] = route
	pool.mu.Unlock()
	mode = "other-failure"
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1))
	pool.targetMu.Lock()
	failed, _ := pool.targetCheck(300, "gpt-6-astra")
	pool.targetMu.Unlock()
	require.False(t, failed.passed)
	require.NotEqual(t, int64(298), failed.sourceID)
	// Only the independent provider quiet has elapsed; keep preference/order.
	wrapper.quotaMu.Lock()
	for key, quiet := range wrapper.quotaBackoff {
		quiet.until = time.Now().Add(-time.Second)
		wrapper.quotaBackoff[key] = quiet
	}
	wrapper.quotaMu.Unlock()
	pool.targetMu.Lock()
	pool.targetRouteFailures = nil
	pool.targetMu.Unlock()
	accountBorrowAdd(t, pool, 298, "renewed-preferred", 220*time.Second)
	mode, seen = "pass", nil
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1))
	require.Equal(t, []string{"renewed-preferred"}, seen, "an unfailed prior successful source survives another source's failed primary record")
	require.NotEqual(t, provenCookie, seen[0])
}

func TestAutomaticBorrowSourcePreferenceDemotesAfterActualAttempt(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298, 297})
	mode := "pass"
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		if mode == "overload" {
			return overloadProbeResponse("server_is_overloaded"), nil
		}
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	_, _, _, release, err := wrapper.targetRouteOnce(req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected)), "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	accountBorrowAdd(t, pool, 298, "new-failing-cookie", 220*time.Second)
	mode = "overload"
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 1), "target_probe_overloaded")
	pool.targetMu.Lock()
	old, exists := pool.targetCheck(300, "gpt-6-astra")
	pool.targetMu.Unlock()
	require.True(t, old.passed, "still-live old business proof is retained, but is independent of exploration preference")
	order := wrapper.automaticTargetSourceOrder(req, "exit", 300, "gpt-6-astra", nil, pool, old, exists)
	require.Equal(t, []int64{299, 297, 298}, order, "a sent failing same-key replacement loses preference while unknown sources rotate first")
	_, key := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 298, nil)
	_, quiet := wrapper.targetQuotaWait(astraTargetQuotaKey{accountID: key.accountID, model: key.model, identity: old.quotaKey.identity, proxy: key.proxy}, time.Now())
	require.True(t, quiet, "the ordering change does not bypass provider overload quiet")
}

func TestAutomaticBorrowSourcePreferenceIgnoresCachedQuietAndUnsentCancel(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298, 297})
	calls := 0
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		calls++
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	selectedReq := req.Clone(context.WithValue(req.Context(), autoBorrowRouteKey{}, selected))
	_, _, _, release, err := wrapper.targetRouteOnce(selectedReq, "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	_, key := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 298, nil)
	preferredAt := pool.targetSourcePreferences[key]
	require.False(t, preferredAt.IsZero())
	_, _, _, release, err = wrapper.targetRouteOnce(selectedReq, "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, 4, calls)
	require.Equal(t, preferredAt, pool.targetSourcePreferences[key], "cached proof does not rewrite success rank")
	quotaKey, _ := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 298, nil)
	wrapper.rememberTargetQuota(quotaKey, "target_probe_overloaded", time.Now().Add(time.Minute))
	_, _, _, release, err = wrapper.targetRouteOnce(selectedReq, "exit", 300, 1, nil)
	release()
	require.EqualError(t, err, "target_probe_overloaded")
	require.Equal(t, preferredAt, pool.targetSourcePreferences[key], "active provider quiet sends nothing and keeps prior preference")
	ctx, cancel := context.WithCancel(selectedReq.Context())
	cancel()
	_, _, _, release, err = wrapper.targetRouteOnce(selectedReq.WithContext(ctx), "exit", 300, 1, nil)
	release()
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 4, calls)
	require.Equal(t, preferredAt, pool.targetSourcePreferences[key], "cancellation before send is not a failed source IQ attempt")
}

func TestAutomaticBorrowSourcePreferenceIsBoundedAndRevokedByMembership(t *testing.T) {
	wrapper, a, cookie, _ := establishedContinuityPair(t)
	pool := wrapper.current(t.Context())
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	_, key := targetBorrowQuietKeys(request, 300, "gpt-6-astra", "exit", 298, nil)
	preferredAt := pool.targetSourcePreferences[key]
	require.False(t, preferredAt.IsZero())
	b := a
	b.Revision, b.CookiePool.SourceAccountIDs = "unrelated-source-removed", []int64{298}
	wrapper.ApplyAstraRoutingSettings(a, b)
	next := wrapper.current(t.Context())
	require.Equal(t, preferredAt, next.targetSourcePreferences[key])
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", cookie))
	changed := accountBorrowRequest(t, "gpt-6-astra", "")
	changed.Header.Set("Authorization", "Bearer changed-identity")
	require.Equal(t, []int64{298}, wrapper.automaticTargetSourceOrder(changed, "exit", 300, "gpt-6-astra", nil, next, astraTargetValidation{}, false))
	require.True(t, next.targetSourcePreferences[key].Equal(preferredAt), "changing a request scope never edits the old scope")
	c := b
	c.Revision, c.CookiePool.SourceAccountIDs = "preferred-source-removed", []int64{299}
	wrapper.ApplyAstraRoutingSettings(b, c)
	require.Empty(t, wrapper.current(t.Context()).targetSourcePreferences)
	d := b
	d.Revision = "readded"
	wrapper.ApplyAstraRoutingSettings(c, d)
	require.Empty(t, wrapper.current(t.Context()).targetSourcePreferences, "removal/readd never resurrects success preference")
	material := d
	material.Revision, material.CookiePool.TTLSeconds = "ttl-changed", 60
	wrapper.ApplyAstraRoutingSettings(d, material)
	require.Empty(t, wrapper.current(t.Context()).targetSourcePreferences, "material route option change cannot carry success preference")
	for i := range 4097 {
		other := key
		other.accountID = int64(1000 + i)
		next.targetMu.Lock()
		next.rememberTargetSourcePreferenceLocked(other, time.Now())
		next.targetMu.Unlock()
	}
	require.LessOrEqual(t, len(next.targetSourcePreferences), 4096)
	next.targetMu.Lock()
	next.rememberTargetSourcePreferenceLocked(key, time.Now().Add(2*time.Hour))
	next.targetMu.Unlock()
	require.Len(t, next.targetSourcePreferences, 1, "one-hour preference state is pruned and never persisted")
}
