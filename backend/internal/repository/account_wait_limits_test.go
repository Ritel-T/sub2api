package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Execute the application's existing Lua over independent client connections;
// limits belong to the account, not the caller's user or API-key identifier.
func TestAccountWaitDifferentCallerLimitsShareAtomicCount(t *testing.T) {
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr()})
	second := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer first.Close()
	defer second.Close()
	a, b := NewConcurrencyCache(first, 15, 120), NewConcurrencyCache(second, 15, 120)
	ctx := context.Background()
	var accepted atomic.Int64
	var failures atomic.Int64
	var workers sync.WaitGroup
	for n := 0; n < 24; n++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			cache := a
			if n%2 == 1 {
				cache = b
			}
			ok, err := cache.IncrementAccountWaitCount(ctx, 42, 1)
			if err != nil {
				failures.Add(1)
				return
			}
			if ok {
				accepted.Add(1)
			}
		}(n)
	}
	workers.Wait()
	require.Zero(t, failures.Load())
	require.EqualValues(t, 1, accepted.Load())
	for n := 0; n < 2; n++ {
		ok, err := b.IncrementAccountWaitCount(ctx, 42, 3)
		require.NoError(t, err)
		require.True(t, ok, "customers retain the original three-waiter threshold")
	}
	ok, err := a.IncrementAccountWaitCount(ctx, 42, 3)
	require.NoError(t, err)
	require.False(t, ok)
	other, err := a.IncrementAccountWaitCount(ctx, 43, 1)
	require.NoError(t, err)
	require.True(t, other, "a different account has independent capacity")
	for n := 0; n < 3; n++ {
		require.NoError(t, a.DecrementAccountWaitCount(ctx, 42))
	}
	ok, err = b.IncrementAccountWaitCount(ctx, 42, 1)
	require.NoError(t, err)
	require.True(t, ok, "released wait membership makes admission available again")
}

func TestAccountWaitFallbackCustomerThresholdRemains100(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	cache := NewConcurrencyCache(client, 15, 120)
	ctx := context.Background()
	for n := 0; n < 100; n++ {
		allowed, err := cache.IncrementAccountWaitCount(ctx, 21, 100)
		require.NoError(t, err)
		require.True(t, allowed)
		admin, err := cache.IncrementAccountWaitCount(ctx, 21, 1)
		require.NoError(t, err)
		require.False(t, admin)
	}
	allowed, err := cache.IncrementAccountWaitCount(ctx, 21, 100)
	require.NoError(t, err)
	require.False(t, allowed)
}
