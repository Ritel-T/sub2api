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
}

func TestSimCacheServiceDisabledOrEmptySession(t *testing.T) {
	svc := NewSimCacheService(NewStubSimCacheRepository(), &config.Config{})
	override, err := svc.ComputeOverride(context.Background(), 1, "")
	require.NoError(t, err)
	require.Nil(t, override)
}
