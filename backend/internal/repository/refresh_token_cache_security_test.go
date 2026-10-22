package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRefreshTokenConsume_ConcurrentSingleWinner(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	c := NewRefreshTokenCache(rdb)
	ctx := context.Background()
	require.NoError(t, c.StoreRefreshToken(ctx, "single", &service.RefreshTokenData{UserID: 42}, time.Hour))
	var winners atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	start := make(chan struct{})
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := c.ConsumeRefreshToken(ctx, "single")
			if err != nil {
				errs <- err
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), winners.Load())
	_, err := c.GetRefreshToken(ctx, "single")
	require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)
}

func TestRefreshTokenConsume_RedisFailureIsNotSuccess(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	c := NewRefreshTokenCache(rdb)
	server.SetError("redis unavailable")
	ok, err := c.ConsumeRefreshToken(context.Background(), "token")
	require.Error(t, err)
	require.False(t, ok)
}

func TestSessionEpochSettings_ArePersistedButExcludedFromExports(t *testing.T) {
	client := newSecuritySecretTestClient(t)
	c := NewSettingRepository(client)
	ctx := context.Background()
	key := service.AuthSessionEpochSettingPrefix + "42"
	require.NoError(t, c.Set(ctx, "site_name", "Retained name"))
	require.NoError(t, c.Set(ctx, key, "0123456789abcdef0123456789abcdef"))
	all, err := c.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, "Retained name", all["site_name"])
	require.NotContains(t, all, key)
	stored, err := NewSettingRepository(client).GetValue(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "0123456789abcdef0123456789abcdef", stored)
}
