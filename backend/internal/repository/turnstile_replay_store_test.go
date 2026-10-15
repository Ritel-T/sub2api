package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type replayTestVerifier struct {
	calls  atomic.Int32
	result *service.TurnstileVerifyResponse
	err    error
}

func (v *replayTestVerifier) VerifyToken(context.Context, string, string, string) (*service.TurnstileVerifyResponse, error) {
	v.calls.Add(1)
	return v.result, v.err
}

func newReplayTestService(t *testing.T, server *miniredis.Miniredis, verifier service.TurnstileVerifier) (*service.TurnstileService, *redis.Client) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	settings := service.NewSettingService(nil, &config.Config{Turnstile: config.TurnstileConfig{
		StrictContext: true, ExpectedHostnames: []string{"app.example.com"},
	}})
	return service.NewTurnstileServiceWithReplayStore(settings, verifier, NewTurnstileReplayStore(client)), client
}

func acceptedReplayTestVerifier() *replayTestVerifier {
	return &replayTestVerifier{result: &service.TurnstileVerifyResponse{
		Success: true, Hostname: "app.example.com", Action: "auth", ChallengeTS: time.Now().UTC().Format(time.RFC3339),
	}}
}

func TestTurnstileReplay_FirstUseReplayHashAndTTL(t *testing.T) {
	server := miniredis.RunT(t)
	verifier := acceptedReplayTestVerifier()
	svc, _ := newReplayTestService(t, server, verifier)
	const token = "private-turnstile-test-token"
	require.NoError(t, svc.VerifyTokenWithSecret(context.Background(), "secret", token, "192.0.2.1"))
	digest := sha256.Sum256([]byte(token))
	key := turnstileReplayKeyPrefix + hex.EncodeToString(digest[:])
	require.Equal(t, []string{key}, server.Keys())
	value, err := server.Get(key)
	require.NoError(t, err)
	require.Equal(t, "1", value)
	require.Equal(t, 10*time.Minute, server.TTL(key))
	require.NotContains(t, key+value, token)

	server.FastForward(time.Minute)
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", token, "192.0.2.2"), service.ErrTurnstileVerificationFailed)
	require.Equal(t, int32(1), verifier.calls.Load(), "replay must be rejected before the vendor call")
	require.Equal(t, 9*time.Minute, server.TTL(key), "replay must not extend or release the original claim")
	server.FastForward(4 * time.Minute)
	require.True(t, server.Exists(key), "claim must outlive the full five-minute token lifetime")
	server.FastForward(5 * time.Minute)
	require.False(t, server.Exists(key), "claims must expire to bound storage")
}

func TestTurnstileReplay_ConcurrentAcrossInstancesOnlyOneClaim(t *testing.T) {
	server := miniredis.RunT(t)
	verifier := acceptedReplayTestVerifier()
	first, _ := newReplayTestService(t, server, verifier)
	second, _ := newReplayTestService(t, server, verifier)
	services := []*service.TurnstileService{first, second}
	const attempts = 32
	start := make(chan struct{})
	results := make(chan error, attempts)
	var workers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func(instance int) {
			defer workers.Done()
			<-start
			results <- services[instance].VerifyTokenWithSecret(context.Background(), "secret", "same-token", "192.0.2.1")
		}(i % len(services))
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrTurnstileVerificationFailed)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, int32(1), verifier.calls.Load())
	require.Len(t, server.Keys(), 1)
}

func TestTurnstileReplay_ExpiredTokenRejectedAfterReplayTTL(t *testing.T) {
	server := miniredis.RunT(t)
	verifier := acceptedReplayTestVerifier()
	svc, _ := newReplayTestService(t, server, verifier)
	require.NoError(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""))
	server.FastForward(10 * time.Minute)
	require.Empty(t, server.Keys())

	// Even if the vendor reports success after the replay entry expires, the
	// independent challenge-time check must reject an expired token.
	verifier.result.ChallengeTS = time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339)
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""), service.ErrTurnstileVerificationFailed)
	require.Equal(t, int32(2), verifier.calls.Load())
}

func TestTurnstileReplay_VerificationFailureNeverReleasesClaim(t *testing.T) {
	for _, test := range []struct {
		name   string
		result *service.TurnstileVerifyResponse
		err    error
	}{
		{name: "outcome unknown network failure", err: errors.New("siteverify request timed out")},
		{name: "vendor rejection", result: &service.TurnstileVerifyResponse{Success: false}},
		{name: "missing response"},
		{name: "wrong context", result: &service.TurnstileVerifyResponse{Success: true, Hostname: "attacker.example", Action: "auth"}},
		{name: "expired proof", result: &service.TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth", ChallengeTS: time.Now().Add(-6 * time.Minute).UTC().Format(time.RFC3339)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			verifier := &replayTestVerifier{result: test.result, err: test.err}
			svc, _ := newReplayTestService(t, server, verifier)
			require.Error(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""))
			require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""), service.ErrTurnstileVerificationFailed)
			require.Equal(t, int32(1), verifier.calls.Load())
			require.Len(t, server.Keys(), 1)
			require.Equal(t, 10*time.Minute, server.TTL(server.Keys()[0]))
		})
	}
}

func TestTurnstileReplay_RedisFailureFailsClosed(t *testing.T) {
	server := miniredis.RunT(t)
	verifier := acceptedReplayTestVerifier()
	svc, client := newReplayTestService(t, server, verifier)
	server.SetError("ERR replay store unavailable")
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""), service.ErrServiceUnavailable)
	require.Zero(t, verifier.calls.Load())
	server.SetError("")
	require.NoError(t, client.Close())
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "other-token", ""), service.ErrServiceUnavailable)
	require.Zero(t, verifier.calls.Load())
}

func TestTurnstileReplay_MissingRedisClientFailsClosed(t *testing.T) {
	claimed, err := NewTurnstileReplayStore(nil).ClaimToken(context.Background(), "hash")
	require.Error(t, err)
	require.False(t, claimed)
}
