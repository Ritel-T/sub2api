//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGeminiTokenCache_MissUsesServicePortAndPreservesLegacyError(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewGeminiTokenCache(rdb)
	token, err := cache.GetAccessToken(context.Background(), "absent")
	require.Empty(t, token)
	require.ErrorIs(t, err, service.ErrTokenCacheMiss)
	require.ErrorIs(t, err, redis.Nil)
	require.NoError(t, cache.SetAccessToken(context.Background(), "present", "test-token", time.Minute))
	token, err = cache.GetAccessToken(context.Background(), "present")
	require.NoError(t, err)
	require.Equal(t, "test-token", token)
}

func TestGeminiTokenCache_DeleteAccessToken_RedisError(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	cache := NewGeminiTokenCache(rdb)
	err := cache.DeleteAccessToken(context.Background(), "broken")
	require.Error(t, err)
}
