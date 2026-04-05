package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type simCacheSettingsRepoStub struct {
	mu   sync.Mutex
	data map[string]string
}

func newSimCacheSettingsRepoStub() *simCacheSettingsRepoStub {
	return &simCacheSettingsRepoStub{data: make(map[string]string)}
}

func (m *simCacheSettingsRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v}, nil
}

func (m *simCacheSettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (m *simCacheSettingsRepoStub) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *simCacheSettingsRepoStub) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *simCacheSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			result[k] = v
		}
	}
	return result, nil
}

func (m *simCacheSettingsRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range settings {
		m.data[k] = v
	}
	return nil
}

func (m *simCacheSettingsRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string, len(m.data))
	for k, v := range m.data {
		result[k] = v
	}
	return result, nil
}

func TestSimCacheServiceFlow(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  1,
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
		RetentionRatio:  1,
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

func TestSimCacheRetentionRatioDefault(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  1,
	}}})

	ctx := context.Background()
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 100))

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.Equal(t, 100, override.HistoryCachedTokenCount)
}

func TestSimCacheRetentionRatioApplied(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  0.7,
	}}})

	ctx := context.Background()
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 100))

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.Equal(t, 70, override.HistoryCachedTokenCount)
}

func TestSimCacheRetentionRatioZeroStoresNoHistory(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  0,
	}}})

	ctx := context.Background()
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 100))

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.Equal(t, 0, override.HistoryCachedTokenCount)
}

func TestSimCacheRetentionRatioOne(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  1,
	}}})

	ctx := context.Background()
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 100))

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.Equal(t, 100, override.HistoryCachedTokenCount)
}

func TestSimCacheRetentionRatioHalf(t *testing.T) {
	repo := NewStubSimCacheRepository()
	svc := NewSimCacheService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  0.5,
	}}})

	ctx := context.Background()
	require.NoError(t, svc.UpdateState(ctx, 1, "sess", 200))

	override, err := svc.ComputeOverride(ctx, 1, "sess")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.Equal(t, 100, override.HistoryCachedTokenCount)
}

func TestGetSimCacheSettingsDefaultsRetentionRatioFromConfig(t *testing.T) {
	repo := newSimCacheSettingsRepoStub()
	svc := NewSettingService(repo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0.2,
		TTLSeconds:      600,
		RetentionRatio:  0.7,
	}}})

	settings, err := svc.GetSimCacheSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0.7, settings.RetentionRatio)
}

func TestGetSimCacheSettingsLegacyJSONNormalizesRetentionRatio(t *testing.T) {
	repo := newSimCacheSettingsRepoStub()
	legacyPayload, err := json.Marshal(map[string]any{
		"enabled":          true,
		"miss_probability": 0.2,
		"ttl_seconds":      600,
	})
	require.NoError(t, err)
	repo.data[SettingKeySimCacheSettings] = string(legacyPayload)

	svc := NewSettingService(repo, &config.Config{})
	settings, err := svc.GetSimCacheSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1.0, settings.RetentionRatio)
}

func TestGetSimCacheSettingsExplicitZeroRetentionRatioRoundTrips(t *testing.T) {
	repo := newSimCacheSettingsRepoStub()
	payload, err := json.Marshal(SimCacheSettings{
		Enabled:         true,
		MissProbability: 0.2,
		TTLSeconds:      600,
		RetentionRatio:  0,
	})
	require.NoError(t, err)
	repo.data[SettingKeySimCacheSettings] = string(payload)

	svc := NewSettingService(repo, &config.Config{})
	settings, err := svc.GetSimCacheSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0.0, settings.RetentionRatio)
}

func TestSetSimCacheSettingsRejectsInvalidRetentionRatio(t *testing.T) {
	svc := NewSettingService(newSimCacheSettingsRepoStub(), &config.Config{})

	for _, ratio := range []float64{-0.1, 1.1} {
		err := svc.SetSimCacheSettings(context.Background(), &SimCacheSettings{
			Enabled:         true,
			MissProbability: 0,
			TTLSeconds:      300,
			RetentionRatio:  ratio,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "retention_ratio")
	}
}

func TestSetSimCacheSettingsUpdatesRuntimeRetentionRatio(t *testing.T) {
	repo := newSimCacheSettingsRepoStub()
	simCacheRepo := NewStubSimCacheRepository()
	simCacheSvc := NewSimCacheService(simCacheRepo, &config.Config{Gateway: config.GatewayConfig{SimulatedCache: config.SimulatedCacheConfig{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  1,
	}}})

	svc := NewSettingService(repo, &config.Config{})
	svc.SetSimCacheService(simCacheSvc)

	err := svc.SetSimCacheSettings(context.Background(), &SimCacheSettings{
		Enabled:         true,
		MissProbability: 0,
		TTLSeconds:      300,
		RetentionRatio:  0.7,
	})
	require.NoError(t, err)

	require.NoError(t, simCacheSvc.UpdateState(context.Background(), 1, "runtime", 100))
	override, err := simCacheSvc.ComputeOverride(context.Background(), 1, "runtime")
	require.NoError(t, err)
	require.NotNil(t, override)
	require.Equal(t, 70, override.HistoryCachedTokenCount)
}
