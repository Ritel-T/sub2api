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

// [OpusClaw Patch] simulated cache billing
type simCacheRepo struct {
	rdb *redis.Client
}

// NewSimCacheRepo creates a Redis-backed SimCacheRepository.
// [OpusClaw Patch] simulated cache billing
func NewSimCacheRepo(rdb *redis.Client) service.SimCacheRepository {
	return &simCacheRepo{rdb: rdb}
}

func buildSimCacheKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", simCachePrefix, groupID, sessionHash)
}

func (r *simCacheRepo) GetSessionCacheState(ctx context.Context, groupID int64, sessionHash string) (*service.SimCacheState, error) {
	key := buildSimCacheKey(groupID, sessionHash)
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
