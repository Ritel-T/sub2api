package repository

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAstraTargetProbeDeadlineHonorsAccountRoundSourceLeaseAndCaller(t *testing.T) {
	for _, tc := range []struct {
		name, mode          string
		caller, lease, want time.Duration
	}{
		{"account_four_shots", config.GatewayBorrowAccountQualityMode, 300 * time.Second, 230 * time.Second, 180 * time.Second},
		{"legacy_two_shots", "", 300 * time.Second, 230 * time.Second, 90 * time.Second},
		{"short_actual_cookie", config.GatewayBorrowAccountQualityMode, 300 * time.Second, 70 * time.Second, 70 * time.Second},
		{"short_caller", config.GatewayBorrowAccountQualityMode, 25 * time.Second, 230 * time.Second, 25 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			caller, stopCaller := context.WithDeadline(t.Context(), now.Add(tc.caller))
			defer stopCaller()
			round, stopRound := astraTargetProbeContext(caller, now.Add(tc.lease), tc.mode)
			defer stopRound()
			deadline, exists := round.Deadline()
			require.True(t, exists)
			require.WithinDuration(t, now.Add(tc.want), deadline, 100*time.Millisecond)
			require.False(t, deadline.After(now.Add(tc.lease)), "new budget cannot extend the source Cookie")
			stopCaller()
			require.ErrorIs(t, round.Err(), context.Canceled, "caller cancellation remains immediate")
		})
	}
}

func TestAccountBorrowFourShotsKeepPerShotDeadlineAndFixedRequestCount(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	accountBorrowAdd(t, pool, 299, "budget", 220*time.Second)
	shots, business := 0, 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		if !service.IsOpenAICodexStateProbeRequest(req.Context()) {
			business++
			return accountBorrowResponse("gpt-6.1-sol", "OK"), nil
		}
		shots++
		deadline, exists := req.Context().Deadline()
		require.True(t, exists)
		require.InDelta(t, 45, time.Until(deadline).Seconds(), 0.5, "round extension does not change per-shot timeout")
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, "gpt-6-astra", gjson.GetBytes(raw, "model").String())
		require.Equal(t, "medium", gjson.GetBytes(raw, "reasoning.effort").String())
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	response, err := wrapper.Do(accountBorrowRequest(t, "gpt-6.1-sol", ""), "exit", 300, 1)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 4, shots)
	require.Equal(t, 1, business)
}

func TestAccountBorrowCallerCancellationCannotPublishPartialCorrectAnswers(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	accountBorrowAdd(t, pool, 299, "cancelled-budget", 220*time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	shots, business := 0, 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		if !service.IsOpenAICodexStateProbeRequest(req.Context()) {
			business++
			return accountBorrowResponse("gpt-6.1-sol", "OK"), nil
		}
		shots++
		if shots == 3 {
			cancel()
			return nil, req.Context().Err()
		}
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	request := accountBorrowRequest(t, "gpt-6.1-sol", "").WithContext(ctx)
	_, err := wrapper.Do(request, "exit", 300, 1)
	require.Error(t, err)
	require.Equal(t, 3, shots)
	require.Zero(t, business, "two correct answers before cancellation never authorize a send")
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", "cancelled-budget"))
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6.1-sol", "cancelled-budget"))
}

func TestAccountBorrowEveryShotInheritsShortSourceCookieDeadline(t *testing.T) {
	wrapper, pool := accountBorrowFixture(t)
	accountBorrowAdd(t, pool, 299, "short-cookie", 220*time.Second)
	pool.mu.Lock()
	route := pool.routes[299]
	route.expires = time.Now().Add(2 * time.Second)
	pool.routes[299] = route
	pool.mu.Unlock()
	shots := 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		shots++
		deadline, exists := req.Context().Deadline()
		require.True(t, exists)
		require.Equal(t, route.expires, deadline, "per-shot inherits the actual route's shorter deadline")
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	selected := autoBorrowRouteSelection{299, route}
	request = request.Clone(context.WithValue(request.Context(), autoBorrowRouteKey{}, selected))
	_, _, _, release, err := wrapper.targetRouteOnce(request, "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, 4, shots)
}
