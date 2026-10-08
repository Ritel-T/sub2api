package repository

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountBorrowCooldownSurvivesNewCookieAndSuppressesSourceRenewal(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	probes := 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		probes++
		return accountBorrowResponse("gpt-6-astra", "29"), nil
	}}
	accountBorrowAdd(t, pool, 299, "first", 220*time.Second)
	accountBorrowAdd(t, pool, 298, "other", 220*time.Second)
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 2), "target_quality_failed")
	require.Equal(t, 8, probes, "each donor emits its complete first round")
	accountBorrowAdd(t, pool, 299, "replacement", 80*time.Second)
	accountBorrowAdd(t, pool, 298, "other-replacement", 80*time.Second)
	snapshot := wrapper.AstraGatewaySnapshot(t.Context())
	require.Len(t, snapshot.Targets, 2)
	for _, row := range snapshot.Targets {
		require.Equal(t, "target_quality_failed", row.Reason)
		require.NotNil(t, row.RetryAt)
		require.True(t, time.Now().Before(*row.RetryAt))
		require.Equal(t, 4, row.Attempts)
		require.Zero(t, row.Correct)
		require.Equal(t, "29", row.Answer)
		require.Equal(t, "quality_failed", row.FailureCode)
	}
	calls := 0
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { calls++; return nil })
	require.NoError(t, wrapper.PrepareAstraGateway(t.Context()))
	require.Zero(t, calls, "known cooling target/source pairs cannot consume a new source request")
	require.Equal(t, 8, probes, "state reads and source preparation cannot retry sugar tests")
}

func TestAccountBorrowCooldownUnknownDonorRemainsBoundedCandidate(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return accountBorrowResponse("gpt-6-astra", "29"), nil
	}}
	accountBorrowAdd(t, pool, 299, "failed", 220*time.Second)
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), req, "exit", 300, 2), "target_quality_failed")
	accountBorrowAdd(t, pool, 299, "replacement", 80*time.Second)
	snapshot := wrapper.AstraGatewaySnapshot(t.Context())
	require.Nil(t, snapshot.Targets[0].RetryAt, "one never-probed source can still be explored")
	var calls []int64
	wrapper.SetAstraGatewayPreparer(func(_ context.Context, id int64) error {
		calls = append(calls, id)
		accountBorrowAdd(t, pool, id, "unknown-fresh", 220*time.Second)
		return nil
	})
	require.NoError(t, wrapper.PrepareAstraGateway(t.Context()))
	require.Equal(t, []int64{298}, calls, "cooling donor skipped without consuming exploration budget")
}

func TestAccountBorrowCooldownPreservesCurrentCookieTransportFailure(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Header: http.Header{}, Body: http.NoBody}, nil
	}}
	accountBorrowAdd(t, pool, 299, "first", 220*time.Second)
	accountBorrowAdd(t, pool, 298, "other", 220*time.Second)
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	snapshot := wrapper.AstraGatewaySnapshot(t.Context())
	require.NotNil(t, snapshot.Targets[0].RetryAt)
	require.Equal(t, "upstream_error", snapshot.Targets[0].FailureCode)
	require.Equal(t, 1, snapshot.Targets[0].Attempts)
	require.Empty(t, snapshot.Targets[0].Answer)
	calls := 0
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { calls++; return nil })
	// Simulate source expiration requiring a new ticket; current-Cookie failures
	// remain a short wait and unknown actual exits are eligible for exploration.
	require.NoError(t, wrapper.PrepareAstraGateway(t.Context()))
	require.Zero(t, calls, "fresh sources need no renewal even during a short transport cooldown")
}

func TestAccountBorrowIntegerAnswerOnlyAndSharedCounts(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return accountBorrowResponse("gpt-6-astra", "token-or-other-private-text"), nil
	}}
	accountBorrowAdd(t, pool, 299, "first", 220*time.Second)
	accountBorrowAdd(t, pool, 298, "other", 220*time.Second)
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	snapshot := wrapper.AstraGatewaySnapshot(t.Context())
	for _, row := range snapshot.Targets {
		require.Equal(t, 4, row.Attempts)
		require.Zero(t, row.Correct)
		require.Empty(t, row.Answer, "arbitrary provider content cannot enter status")
	}
	require.Equal(t, snapshot.Targets[0].Attempts, snapshot.Targets[1].Attempts, "model rows describe the same round")
}

func TestAccountBorrowQuotaRetryAllowsUnknownExitButNotSameExit(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{}, Body: http.NoBody}, nil
	}}
	accountBorrowAdd(t, pool, 299, "first", 220*time.Second)
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2), "target_probe_rate_limited")
	require.NotNil(t, wrapper.AstraGatewaySnapshot(t.Context()).Targets[0].RetryAt, "without IP affinity every source uses the known target exit")
	// A real request with refreshed credentials owns a fresh quota scope.
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	request.Header.Set("Authorization", "Bearer replacement")
	_, _, _, release, err := wrapper.targetRouteOnce(request, "exit", 300, 2, nil)
	release()
	require.EqualError(t, err, "target_probe_rate_limited")
	require.True(t, strings.Contains(wrapper.AstraGatewaySnapshot(t.Context()).Targets[0].Reason, "rate_limited"))
}
func TestAccountBorrowEffectiveRetryUsesLongestConstraintAndEarliestDonor(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	now := time.Now()
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	quota, negative := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 299, nil)
	check := astraTargetValidation{sourceID: 299, quotaKey: quota, negativeKey: negative}
	wrapper.rememberTargetQualityFailure(negative, "target_quality_failed", now)
	wrapper.rememberTargetQuota(quota, "target_probe_rate_limited", now.Add(5*time.Minute))
	pool.targetMu.Lock()
	pool.mu.Lock()
	retry := wrapper.targetRetryDeadline(pool, check, 300, "gpt-6-astra", now)
	pool.mu.Unlock()
	pool.targetMu.Unlock()
	require.Equal(t, now.Add(5*time.Minute), retry.until, "quota outlasts source-specific quality wait")
	require.Equal(t, "target_probe_rate_limited", retry.reason)
	wrapper.resetTargetQualityFailure(quota)
	_, other := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "exit", 298, nil)
	wrapper.rememberTargetQualityFailure(other, "target_quality_failed", now.Add(time.Minute))
	pool.targetMu.Lock()
	pool.mu.Lock()
	retry = wrapper.targetRetryDeadline(pool, check, 300, "gpt-6-astra", now)
	pool.mu.Unlock()
	pool.targetMu.Unlock()
	require.Equal(t, now.Add(3*time.Minute), retry.until, "the first donor to leave its complete quiet period can resume")
	require.Equal(t, "target_quality_failed", retry.reason)
}

func TestAccountBorrowIPAffinityUnknownExitRemainsExplorable(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	pool.config.IPAffinity = true
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	quota, negative := targetBorrowQuietKeys(req, 300, "gpt-6-astra", "donor-exit", 299, nil)
	wrapper.rememberTargetQuota(quota, "target_probe_rate_limited", time.Now().Add(5*time.Minute))
	check := astraTargetValidation{sourceID: 299, quotaKey: quota, negativeKey: negative, route: codexGatewayRoute{cookie: http.Cookie{Value: "known"}, proxy: "donor-exit"}}
	pool.targetMu.Lock()
	pool.mu.Lock()
	retry := wrapper.targetRetryDeadline(pool, check, 300, "gpt-6-astra", time.Now())
	pool.mu.Unlock()
	pool.targetMu.Unlock()
	require.True(t, retry.until.IsZero(), "unknown donor actual exit must not inherit another exit's quota")
}

func TestAccountBorrowOldValidProofStillRenewsAfterBadReplacement(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		cookie, _ := req.Cookie("__oailb")
		answer := "21"
		if cookie.Value == "bad" {
			answer = "29"
		}
		return accountBorrowResponse("gpt-6-astra", answer), nil
	}}
	accountBorrowAdd(t, pool, 299, "old", 110*time.Second)
	accountBorrowAdd(t, pool, 298, "bad", 220*time.Second)
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	accountBorrowAdd(t, pool, 299, "bad", 80*time.Second)
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
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
	require.Equal(t, []int64{299}, calls, "retained valid source may renew despite its failed replacement")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"))
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "old"))
	snapshot := wrapper.AstraGatewaySnapshot(t.Context())
	require.Equal(t, "ready", snapshot.Targets[0].State)
	require.Nil(t, snapshot.Targets[0].RetryAt, "replacement wait cannot hide a still-valid shared proof")
}

func TestAccountBorrowRefreshedIdentityCanAcquireSource(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return accountBorrowResponse("gpt-6-astra", "29"), nil
	}}
	accountBorrowAdd(t, pool, 299, "failed", 220*time.Second)
	accountBorrowAdd(t, pool, 298, "other-failed", 220*time.Second)
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	pool.mu.Lock()
	pool.routes = nil
	for id, status := range pool.statuses {
		at := time.Now().Add(-time.Minute)
		status.CheckedAt = &at
		pool.statuses[id] = status
	}
	pool.mu.Unlock()
	var calls []int64
	wrapper.SetAstraGatewayPreparer(func(_ context.Context, id int64) error {
		calls = append(calls, id)
		accountBorrowAdd(t, pool, id, "fresh", 220*time.Second)
		return nil
	})
	require.EqualError(t, wrapper.PrepareAstraGateway(t.Context()), "source_probe_cooldown")
	require.Empty(t, calls, "old known identity remains quiet without a source request")
	refreshed := accountBorrowRequest(t, "gpt-6-astra", "")
	refreshed.Header.Set("Authorization", "Bearer refreshed")
	require.NoError(t, wrapper.PrepareAstraGateway(targetPreparationContext(refreshed, "exit", 300, "gpt-6-astra", nil)))
	require.Len(t, calls, 2, "a refreshed caller is eligible under its own identity scope")
}

func TestAccountBorrowNewProbeQuotaDoesNotBlockOtherModelOldProof(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		cookie, _ := req.Cookie("__oailb")
		if cookie.Value == "limited" {
			return &http.Response{StatusCode: 429, Header: http.Header{}, Body: http.NoBody}, nil
		}
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	accountBorrowAdd(t, pool, 299, "old", 220*time.Second)
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	accountBorrowAdd(t, pool, 299, "limited", 220*time.Second)
	require.EqualError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2), "target_probe_rate_limited")
	snapshot := wrapper.AstraGatewaySnapshot(t.Context())
	require.Equal(t, "blocked", snapshot.Targets[0].State)
	require.Equal(t, "target_probe_rate_limited", snapshot.Targets[0].Reason)
	require.NotNil(t, snapshot.Targets[0].RetryAt)
	require.Equal(t, "ready", snapshot.Targets[1].State, "Sol keeps its independent business quota and the shared old quality proof")
	require.Nil(t, snapshot.Targets[1].RetryAt)
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "old"), "IQ proof is retained separately from model quota")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "old"))
	_, err := wrapper.Do(accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2)
	require.EqualError(t, err, "target_probe_rate_limited")
	response, err := wrapper.Do(accountBorrowRequest(t, "gpt-6.1-sol", ""), "exit", 300, 2)
	require.NoError(t, err)
	_ = response.Body.Close()
}

func TestAccountBorrowMixedBlockedUnknownTargetCannotDriveSourcePreparation(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return accountBorrowResponse("gpt-6-astra", "29"), nil
	}}
	accountBorrowAdd(t, pool, 299, "failed", 220*time.Second)
	accountBorrowAdd(t, pool, 298, "other-failed", 220*time.Second)
	require.Error(t, wrapper.VerifyAstraGatewayTarget(t.Context(), accountBorrowRequest(t, "gpt-6-astra", ""), "exit", 300, 2))
	// This unseen target is blocked in the service snapshot. It remains configured
	// but cannot qualify a source solely because it has never run a target probe.
	pool.config.TargetAccountIDs = append(pool.config.TargetAccountIDs, 140)
	pool.mu.Lock()
	pool.routes = nil
	for id, status := range pool.statuses {
		at := time.Now().Add(-time.Minute)
		status.CheckedAt = &at
		pool.statuses[id] = status
	}
	pool.mu.Unlock()
	calls := 0
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { calls++; return nil })
	ctx := service.WithAstraSourceTargetDemand(t.Context(), []int64{300})
	require.EqualError(t, wrapper.prepareAutomaticSources(ctx, pool), "source_probe_cooldown")
	require.Zero(t, calls, "the unseen blocked target must not bypass every cooled donor")
	req := accountBorrowRequest(t, "gpt-6-astra", "")
	ctx = targetPreparationContext(req, "exit", 300, "gpt-6-astra", nil)
	require.EqualError(t, wrapper.prepareAutomaticSources(ctx, pool), "source_probe_cooldown")
	require.Zero(t, calls, "on-demand preparation only considers its caller")
	// Manual unscoped preparation retains its existing all-target exploration.
	require.EqualError(t, wrapper.prepareAutomaticSources(t.Context(), pool), "no_qualified_source_route")
	require.Equal(t, 2, calls)
}
