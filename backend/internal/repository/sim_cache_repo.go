package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const simCachePrefix = "simcache:"

// [OpusClaw Patch] simulated cache resilience — Lua script for atomic state update.
// KEYS[1] = simcache key, ARGV[1] = cached_token_count, ARGV[2] = TTL seconds.
// Sets cached_token_count, increments turn_count, refreshes TTL — all atomically.
var simCacheAtomicUpdateScript = redis.NewScript(`
local key = KEYS[1]
local tokens = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local tc = redis.call('HINCRBY', key, 'turn_count', 1)
redis.call('HSET', key, 'cached_token_count', tokens)
redis.call('EXPIRE', key, ttl)
return tc
`)

// [OpusClaw Patch] simulated cache billing
type simCacheRepo struct {
	rdb *redis.Client
}

// [OpusClaw Patch] simulated cache billing
func NewSimCacheRepo(rdb *redis.Client) service.SimCacheRepository {
	return &simCacheRepo{rdb: rdb}
}

func buildSimCacheKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", simCachePrefix, groupID, sessionHash)
}

func (r *simCacheRepo) GetSessionCacheState(ctx context.Context, groupID int64, sessionHash string) (*service.SimCacheState, error) {
	key := buildSimCacheKey(groupID, sessionHash)

	// Try hash format first (new atomic format)
	vals, err := r.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) > 0 {
		state := &service.SimCacheState{}
		if v, ok := vals["cached_token_count"]; ok {
			fmt.Sscanf(v, "%d", &state.CachedTokenCount)
		}
		if v, ok := vals["turn_count"]; ok {
			fmt.Sscanf(v, "%d", &state.TurnCount)
		}
		return state, nil
	}

	// Fall back to legacy JSON string format (backward compat during rollout)
	val, err := r.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var state service.SimCacheState
	if err := json.Unmarshal(val, &state); err != nil {
		return nil, fmt.Errorf("simcache: unmarshal state: %w", err)
	}
	return &state, nil
}

func (r *simCacheRepo) SetSessionCacheState(ctx context.Context, groupID int64, sessionHash string, state *service.SimCacheState, ttl time.Duration) error {
	if state == nil {
		return nil
	}
	key := buildSimCacheKey(groupID, sessionHash)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("simcache: marshal state: %w", err)
	}
	return r.rdb.Set(ctx, key, data, ttl).Err()
}

// [OpusClaw Patch] simulated cache resilience
func (r *simCacheRepo) AtomicUpdateSessionCacheState(ctx context.Context, groupID int64, sessionHash string, cachedTokenCount int, ttl time.Duration) error {
	key := buildSimCacheKey(groupID, sessionHash)
	ttlSec := int(ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 300
	}
	return simCacheAtomicUpdateScript.Run(ctx, r.rdb, []string{key}, cachedTokenCount, ttlSec).Err()
}
