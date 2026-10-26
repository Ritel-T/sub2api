//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newTotpConsumeTestCache(t *testing.T) (*TotpCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: 3})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewTotpCache(rdb).(*TotpCache), mr
}

func totpConsumeTestSession() *service.TotpLoginSession {
	return &service.TotpLoginSession{
		UserID: 42, Email: "user@example.com", TokenExpiry: time.Now().UTC().Add(time.Minute),
		Authentication:   service.NativeAuthenticationProof{Methods: []string{"pwd"}, AuthTime: 123, Generation: "generation", Epoch: "epoch"},
		PendingOAuthBind: &service.PendingOAuthBindLoginSession{PendingSessionToken: "pending", BrowserSessionKey: "browser"},
	}
}

func TestTotpCacheConsumeLoginSessionRoundTrip(t *testing.T) {
	cache, mr := newTotpConsumeTestCache(t)
	ctx := context.Background()
	want := totpConsumeTestSession()
	require.NoError(t, cache.SetLoginSession(ctx, "challenge", want, time.Minute))
	require.NoError(t, mr.Set(totpSetupKeyPrefix+"challenge", "untouched"))
	got, err := cache.ConsumeLoginSession(ctx, "challenge")
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.False(t, mr.Exists(totpLoginKeyPrefix+"challenge"))
	require.True(t, mr.Exists(totpSetupKeyPrefix+"challenge"))
	got, err = cache.ConsumeLoginSession(ctx, "challenge")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestTotpCacheConsumeLoginSessionConcurrentOneWinner(t *testing.T) {
	cache, mr := newTotpConsumeTestCache(t)
	ctx := context.Background()
	want := totpConsumeTestSession()
	require.NoError(t, cache.SetLoginSession(ctx, "challenge", want, time.Minute))
	var getdels, nonGetdels atomic.Int64
	mr.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		switch cmd {
		case "GETDEL":
			getdels.Add(1)
		case "GET", "DEL", "EVAL", "MULTI", "WATCH":
			nonGetdels.Add(1)
		}
		return false
	})
	const contenders = 64
	type result struct {
		session *service.TotpLoginSession
		err     error
	}
	results := make(chan result, contenders)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			session, err := cache.ConsumeLoginSession(ctx, "challenge")
			results <- result{session, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.session != nil {
			winners++
			require.Equal(t, want, result.session)
		}
	}
	require.Equal(t, 1, winners)
	require.EqualValues(t, contenders, getdels.Load())
	require.Zero(t, nonGetdels.Load())
	require.False(t, mr.Exists(totpLoginKeyPrefix+"challenge"))
}

func TestTotpCacheConsumeLoginSessionMalformedIsConsumedAndRedacted(t *testing.T) {
	valid, err := json.Marshal(totpConsumeTestSession())
	require.NoError(t, err)
	unknown := strings.TrimSuffix(string(valid), "}") + `,"private-secret-654321":"private-token"}`
	nestedUnknown := strings.Replace(string(valid), `"methods":["pwd"]`, `"methods":["pwd"],"private-secret-654321":"private-token"`, 1)
	for name, payload := range map[string]string{
		"broken": `{"UserID":"private-secret-654321"`,
		"type":   `{"UserID":"private-secret-654321"}`,
		"null":   `null`, "empty": `{}`, "array": `[]`,
		"unknown": unknown, "nested_unknown": nestedUnknown,
		"trailing_object":  string(valid) + ` {"private-secret-654321":"private-token"}`,
		"trailing_garbage": string(valid) + ` private-secret-654321`,
		"bad_time":         `{"UserID":42,"TokenExpiry":"private-secret-654321"}`,
		"zero_id":          `{"UserID":0,"TokenExpiry":"2099-01-01T00:00:00Z"}`,
		"negative_id":      `{"UserID":-1,"TokenExpiry":"2099-01-01T00:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			cache, mr := newTotpConsumeTestCache(t)
			require.NoError(t, mr.Set(totpLoginKeyPrefix+"private-token", payload))
			got, err := cache.ConsumeLoginSession(context.Background(), "private-token")
			require.Error(t, err)
			require.Nil(t, got)
			require.NotContains(t, err.Error(), "private-token")
			require.NotContains(t, err.Error(), "private-secret")
			require.NotContains(t, err.Error(), "654321")
			require.False(t, mr.Exists(totpLoginKeyPrefix+"private-token"))
			got, err = cache.ConsumeLoginSession(context.Background(), "private-token")
			require.NoError(t, err)
			require.Nil(t, got)
		})
	}
}

func TestTotpCacheConsumeLoginSessionMissingAndExpired(t *testing.T) {
	cache, mr := newTotpConsumeTestCache(t)
	ctx := context.Background()
	got, err := cache.ConsumeLoginSession(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, cache.SetLoginSession(ctx, "expired", totpConsumeTestSession(), time.Minute))
	mr.FastForward(2 * time.Minute)
	got, err = cache.ConsumeLoginSession(ctx, "expired")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestTotpCacheConsumeLoginSessionUnavailable(t *testing.T) {
	for _, cache := range []*TotpCache{nil, {}} {
		got, err := cache.ConsumeLoginSession(context.Background(), "private-token")
		require.Nil(t, got)
		require.ErrorIs(t, err, service.ErrServiceUnavailable)
	}
}

func TestTotpCacheConsumeLoginSessionUnknownOutcomeDoesNotRetry(t *testing.T) {
	cache, mr := newTotpConsumeTestCache(t)
	ctx := context.Background()
	require.NoError(t, cache.SetLoginSession(ctx, "private-token", totpConsumeTestSession(), time.Minute))
	var calls atomic.Int64
	mr.Server().SetPreHook(func(peer *server.Peer, cmd string, args ...string) bool {
		if cmd != "GETDEL" {
			return false
		}
		calls.Add(1)
		// Simulate a server-side consume followed by losing the reply.
		mr.Del(args[0])
		peer.Close()
		return true
	})
	got, err := cache.ConsumeLoginSession(ctx, "private-token")
	require.Nil(t, got)
	require.ErrorIs(t, err, service.ErrServiceUnavailable)
	require.EqualValues(t, 1, calls.Load())
	require.False(t, mr.Exists(totpLoginKeyPrefix+"private-token"))
	require.NotContains(t, err.Error(), "private-token")
}

func TestTotpCacheConsumeLoginSessionUnsupportedCommandIsRedacted(t *testing.T) {
	cache, mr := newTotpConsumeTestCache(t)
	ctx := context.Background()
	require.NoError(t, cache.SetLoginSession(ctx, "private-token", totpConsumeTestSession(), time.Minute))
	var calls atomic.Int64
	mr.Server().SetPreHook(func(peer *server.Peer, cmd string, _ ...string) bool {
		if cmd != "GETDEL" {
			return false
		}
		calls.Add(1)
		peer.WriteError("ERR unknown command GETDEL private-token private-secret 654321")
		return true
	})
	got, err := cache.ConsumeLoginSession(ctx, "private-token")
	require.Nil(t, got)
	require.ErrorIs(t, err, service.ErrServiceUnavailable)
	require.EqualValues(t, 1, calls.Load())
	require.True(t, mr.Exists(totpLoginKeyPrefix+"private-token"))
	for _, sensitive := range []string{"private-token", "private-secret", "654321"} {
		require.NotContains(t, err.Error(), sensitive)
	}
}
