package service

import (
	"context"
	"math/rand/v2"
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

// [OpusClaw Patch] simulated cache billing
type SimCacheService struct {
	repo SimCacheRepository
	cfg  config.SimulatedCacheConfig
}

// [OpusClaw Patch] simulated cache billing
func NewSimCacheService(repo SimCacheRepository, cfg *config.Config) *SimCacheService {
	serviceCfg := config.SimulatedCacheConfig{}
	if cfg != nil {
		serviceCfg = cfg.Gateway.SimulatedCache
	}
	return &SimCacheService{repo: repo, cfg: serviceCfg}
}

// [OpusClaw Patch] simulated cache billing
func (s *SimCacheService) ComputeOverride(ctx context.Context, groupID int64, sessionHash string) (*antigravity.SimCacheOverride, error) {
	if s == nil || !s.cfg.Enabled || sessionHash == "" || s.repo == nil {
		return nil, nil
	}

	state, err := s.repo.GetSessionCacheState(ctx, groupID, sessionHash)
	if err != nil {
		return nil, err
	}
	if state == nil || state.TurnCount <= 0 {
		return &antigravity.SimCacheOverride{IsFirstTurn: true}, nil
	}

	return &antigravity.SimCacheOverride{
		HistoryCachedTokenCount: state.CachedTokenCount,
		IsMiss:                  rand.Float64() < s.cfg.MissProbability,
		IsFirstTurn:             false,
	}, nil
}

// [OpusClaw Patch] simulated cache billing
func (s *SimCacheService) UpdateState(ctx context.Context, groupID int64, sessionHash string, totalPromptTokens int) error {
	if s == nil || !s.cfg.Enabled || sessionHash == "" || s.repo == nil {
		return nil
	}
	if totalPromptTokens < 0 {
		totalPromptTokens = 0
	}

	state, err := s.repo.GetSessionCacheState(ctx, groupID, sessionHash)
	if err != nil {
		return err
	}
	turnCount := 1
	if state != nil && state.TurnCount > 0 {
		turnCount = state.TurnCount + 1
	}

	ttlSeconds := s.cfg.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}

	return s.repo.SetSessionCacheState(ctx, groupID, sessionHash, &SimCacheState{
		CachedTokenCount: totalPromptTokens,
		TurnCount:        turnCount,
	}, time.Duration(ttlSeconds)*time.Second)
}
