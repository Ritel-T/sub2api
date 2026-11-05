package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewDashboardCacheKeyPrefix(t *testing.T) {
	cache := NewDashboardCache(nil, &config.Config{
		Dashboard: config.DashboardCacheConfig{
			KeyPrefix: "prod",
		},
	})
	impl, ok := cache.(*dashboardCache)
	require.True(t, ok)
	require.Equal(t, "prod:", impl.keyPrefix)

	cache = NewDashboardCache(nil, &config.Config{
		Dashboard: config.DashboardCacheConfig{
			KeyPrefix: "staging:",
		},
	})
	impl, ok = cache.(*dashboardCache)
	require.True(t, ok)
	require.Equal(t, "staging:", impl.keyPrefix)
}

func TestDashboardCache_RequestDayScope(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, ok := NewDashboardCache(client, nil).(*dashboardCache)
	require.True(t, ok)
	utc := timezone.WithUserDay(context.Background(), "UTC")
	same := timezone.WithUserDay(context.Background(), "Etc/UTC")
	different := timezone.WithUserDay(context.Background(), "Pacific/Honolulu")
	require.Equal(t, cache.buildKey(utc), cache.buildKey(same))
	require.NotEqual(t, cache.buildKey(utc), cache.buildKey(different))
	require.Contains(t, cache.buildKey(utc), "dashboard:stats:v2:")
	// A pre-fix global value must never populate the newly scoped response.
	require.NoError(t, server.Set("sub2api:dashboard:stats:v1", "old-server-day"))
	_, err := cache.GetDashboardStats(utc)
	require.Error(t, err)
	require.NoError(t, cache.SetDashboardStats(utc, "utc", time.Minute))
	require.NoError(t, cache.SetDashboardStats(different, "honolulu", time.Minute))
	got, err := cache.GetDashboardStats(same)
	require.NoError(t, err)
	require.Equal(t, "utc", got)
	got, err = cache.GetDashboardStats(different)
	require.NoError(t, err)
	require.Equal(t, "honolulu", got)
	require.NoError(t, cache.DeleteDashboardStats(same))
	_, err = cache.GetDashboardStats(utc)
	require.Error(t, err)
	got, err = cache.GetDashboardStats(different)
	require.NoError(t, err)
	require.Equal(t, "honolulu", got)
}
