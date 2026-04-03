package service

import (
	"context"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

type simCacheOverrideContextKey struct{}

// [OpusClaw Patch] simulated cache billing
var SimCacheOverrideContextKey = simCacheOverrideContextKey{}

// [OpusClaw Patch] simulated cache billing
func WithSimCacheOverride(ctx context.Context, override *antigravity.SimCacheOverride) context.Context {
	if override == nil {
		return ctx
	}
	return context.WithValue(ctx, SimCacheOverrideContextKey, override)
}

// [OpusClaw Patch] simulated cache billing
func GetSimCacheOverride(ctx context.Context) *antigravity.SimCacheOverride {
	override, _ := ctx.Value(SimCacheOverrideContextKey).(*antigravity.SimCacheOverride)
	return override
}

const (
	// [OpusClaw Patch] simulated cache resilience
	simCacheRedisTimeout      = 50 * time.Millisecond
	simCacheBreakerThreshold  = 5
	simCacheBreakerCooldownNs = int64(30 * time.Second)
)

// [OpusClaw Patch] simulated cache billing + resilience
type SimCacheService struct {
	repo SimCacheRepository

	// atomic.Value holds config.SimulatedCacheConfig — race-free hot-reload
	cfgSnapshot atomic.Value

	// circuit breaker: consecutive failures and disable-until timestamp (UnixNano)
	consecutiveFailures atomic.Int64
	disableUntil        atomic.Int64
}

// [OpusClaw Patch] simulated cache billing
func NewSimCacheService(repo SimCacheRepository, cfg *config.Config) *SimCacheService {
	s := &SimCacheService{repo: repo}
	if cfg != nil {
		s.cfgSnapshot.Store(cfg.Gateway.SimulatedCache)
	}
	return s
}

// UpdateConfig atomically replaces the runtime simcache config snapshot.
// Called by SettingService.SetSimCacheSettings after DB persistence.
// [OpusClaw Patch] simulated cache resilience
func (s *SimCacheService) UpdateConfig(cfg config.SimulatedCacheConfig) {
	if s == nil {
		return
	}
	s.cfgSnapshot.Store(cfg)
}

func (s *SimCacheService) loadConfig() config.SimulatedCacheConfig {
	if s == nil {
		return config.SimulatedCacheConfig{}
	}
	cfg, _ := s.cfgSnapshot.Load().(config.SimulatedCacheConfig)
	return cfg
}

// [OpusClaw Patch] simulated cache resilience — circuit breaker
func (s *SimCacheService) isBreakerOpen() bool {
	until := s.disableUntil.Load()
	return until > 0 && time.Now().UnixNano() < until
}

func (s *SimCacheService) recordSuccess() {
	s.consecutiveFailures.Store(0)
}

func (s *SimCacheService) recordFailure() {
	n := s.consecutiveFailures.Add(1)
	if n >= simCacheBreakerThreshold {
		s.disableUntil.Store(time.Now().UnixNano() + simCacheBreakerCooldownNs)
		s.consecutiveFailures.Store(0)
	}
}

// [OpusClaw Patch] simulated cache billing
func (s *SimCacheService) ComputeOverride(ctx context.Context, groupID int64, sessionHash string) (*antigravity.SimCacheOverride, error) {
	cfg := s.loadConfig()
	if s == nil || !cfg.Enabled || sessionHash == "" || s.repo == nil {
		return nil, nil
	}
	if s.isBreakerOpen() {
		return nil, nil
	}

	opCtx, cancel := context.WithTimeout(ctx, simCacheRedisTimeout)
	defer cancel()

	state, err := s.repo.GetSessionCacheState(opCtx, groupID, sessionHash)
	if err != nil {
		s.recordFailure()
		return nil, err
	}
	s.recordSuccess()

	if state == nil || state.TurnCount <= 0 {
		return &antigravity.SimCacheOverride{IsFirstTurn: true}, nil
	}

	return &antigravity.SimCacheOverride{
		HistoryCachedTokenCount: state.CachedTokenCount,
		IsMiss:                  rand.Float64() < cfg.MissProbability,
		IsFirstTurn:             false,
	}, nil
}

// [OpusClaw Patch] simulated cache billing + resilience
func (s *SimCacheService) UpdateState(ctx context.Context, groupID int64, sessionHash string, totalPromptTokens int) error {
	cfg := s.loadConfig()
	if s == nil || !cfg.Enabled || sessionHash == "" || s.repo == nil {
		return nil
	}
	if s.isBreakerOpen() {
		return nil
	}
	if totalPromptTokens < 0 {
		totalPromptTokens = 0
	}

	ttlSeconds := cfg.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}

	// Use context.WithoutCancel so client disconnect doesn't abort the post-response state update,
	// but cap with a short timeout to prevent lingering Redis ops.
	baseCtx := context.WithoutCancel(ctx)
	opCtx, cancel := context.WithTimeout(baseCtx, simCacheRedisTimeout)
	defer cancel()

	err := s.repo.AtomicUpdateSessionCacheState(opCtx, groupID, sessionHash, totalPromptTokens, time.Duration(ttlSeconds)*time.Second)
	if err != nil {
		s.recordFailure()
		return err
	}
	s.recordSuccess()
	return nil
}
