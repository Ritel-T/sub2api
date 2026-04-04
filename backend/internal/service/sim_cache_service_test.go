package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSimCacheServiceFlow(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
	}}})
	ctx := context.Background()

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.True(t, override.IsFirstTurn)
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 100))

	override, err = svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.False(t, override.IsFirstTurn)
	require.False(t, override.IsMiss)
	require.Equal(t, 100, override.HistoryCachedTokenCount)
	require.Equal(t, 300, override.TTLSeconds)
}

func TestSimCacheServiceMissProbabilityOne(t *testing.T) {
	repo := NewStubSimCacheRepository()
	require.NoError(t, repo.SetSessionCacheState(context.Background(), 1, "sess", &SimCacheState{CachedTokenCount: 50, TurnCount: 1}, 0))
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 1,
		TTLSeconds:      300,
	}}})
	override, err := svc.ComputeOverride(context.Background(), 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.True(t, override.IsMiss)
	require.Equal(t, 300, override.TTLSeconds)
}

func TestSimCacheServiceDisabledOrEmptySession(t *testing.T) {
	svc := NewSimCacheService(NewStubSimCacheRepository(), &config.Config{})
	override, err := svc.ComputeOverride(context.Background(), 1, "")
	require.NoError(t, err)
	require.Nil(t, override)
}

func TestSimCacheServiceDefaultTTLIs3600(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      0,
	}}})

	ctx := context.Background()
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 42))

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.False(t, override.IsFirstTurn)
	require.Equal(t, 3600, override.TTLSeconds)
	require.Equal(t, 42, override.HistoryCachedTokenCount)
}

func TestSimCacheTTLContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	require.Equal(t, 0, GetSimCacheTTL(ctx))

	ctx = WithSimCacheTTL(ctx, 1800)
	require.Equal(t, 1800, GetSimCacheTTL(ctx))

	unchanged := WithSimCacheTTL(ctx, 0)
	require.Equal(t, 1800, GetSimCacheTTL(unchanged))
}
