package service

import (
	"context"
	"time"
)

// SimCacheState 每会话的模拟缓存状态，存储在 Redis 中。
// [OpusClaw Patch] simulated cache billing
type SimCacheState struct {
	CachedTokenCount int `json:"cached_token_count"` // 上一轮的 promptTokenCount
	TurnCount        int `json:"turn_count"`         // 已完成的轮数
}

// SimCacheRepository 模拟缓存状态仓库接口。
// 提供 per-session 的 token 累积状态存储，用于模拟 prompt cache 命中率。
// Key 格式: simcache:{groupID}:{sessionHash}
// [OpusClaw Patch] simulated cache billing
type SimCacheRepository interface {
	// GetSessionCacheState 获取会话的模拟缓存状态。
	// 当 key 不存在时返回 (nil, nil)，表示第 1 轮对话。
	GetSessionCacheState(ctx context.Context, groupID int64, sessionHash string) (*SimCacheState, error)
	// SetSessionCacheState 设置会话的模拟缓存状态，带 TTL。
	// state 序列化为 JSON 存储到 Redis。
	SetSessionCacheState(ctx context.Context, groupID int64, sessionHash string, state *SimCacheState, ttl time.Duration) error
}
