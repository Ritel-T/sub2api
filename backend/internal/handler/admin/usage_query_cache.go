package admin

import (
	"context"
	"slices"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// 与 dashboard 查询缓存同款:30s TTL 进程内缓存,仅服务 /admin/usage/stats 读路径。
var usageStatsCache = newSnapshotCache(30 * time.Second)

type usageStatsCacheKeyData struct {
	StartTime             string `json:"start_time"`
	EndTime               string `json:"end_time"`
	UserID                int64  `json:"user_id"`
	APIKeyID              int64  `json:"api_key_id"`
	AccountID             int64  `json:"account_id"`
	GroupID               int64  `json:"group_id"`
	Model                 string `json:"model"`
	ModelFilterSource     string `json:"model_filter_source"`
	RequestID             string `json:"request_id"`
	ExactTotal            bool   `json:"exact_total"`
	BillingMode           string `json:"billing_mode"`
	RequestType           *int16 `json:"request_type"`
	Stream                *bool  `json:"stream"`
	NativeCompactionV2    *bool  `json:"native_compaction_v2"`
	BillingType           *int8  `json:"billing_type"`
	UpstreamModelMismatch *bool  `json:"upstream_model_mismatch"`
}

func usageStatsCacheKey(filters usagestats.UsageLogFilters) string {
	start := ""
	if filters.StartTime != nil {
		start = filters.StartTime.UTC().Format(time.RFC3339Nano)
	}
	end := ""
	if filters.EndTime != nil {
		end = filters.EndTime.UTC().Format(time.RFC3339Nano)
	}
	return mustMarshalDashboardCacheKey(usageStatsCacheKeyData{
		StartTime:             start,
		EndTime:               end,
		UserID:                filters.UserID,
		APIKeyID:              filters.APIKeyID,
		AccountID:             filters.AccountID,
		GroupID:               filters.GroupID,
		Model:                 filters.Model,
		ModelFilterSource:     filters.ModelFilterSource,
		RequestID:             filters.RequestID,
		ExactTotal:            filters.ExactTotal,
		BillingMode:           filters.BillingMode,
		RequestType:           filters.RequestType,
		Stream:                filters.Stream,
		NativeCompactionV2:    filters.NativeCompactionV2,
		BillingType:           filters.BillingType,
		UpstreamModelMismatch: filters.UpstreamModelMismatch,
	})
}

// getStatsCached 命中则返回缓存,未命中则回源 usageService 并写缓存。
func (h *UsageHandler) getStatsCached(ctx context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, bool, error) {
	key := usageStatsCacheKey(filters)
	return loadUsageStatsCached(usageStatsCache, ctx, key, func() (*usagestats.UsageStats, error) {
		return h.usageService.GetStatsWithFilters(ctx, filters)
	})
}

func loadUsageStatsCached(cache *snapshotCache, ctx context.Context, key string, load func() (*usagestats.UsageStats, error)) (*usagestats.UsageStats, bool, error) {
	entry, hit, err := cache.GetOrLoadContext(ctx, key, func() (any, error) {
		stats, err := load()
		if err != nil {
			return nil, err
		}
		return cloneUsageStats(stats), nil
	})
	if err != nil {
		return nil, hit, err
	}
	stats, err := snapshotPayloadAs[*usagestats.UsageStats](entry.Payload)
	return cloneUsageStats(stats), hit, err
}

// Preserve the existing numeric types and nil/empty distinction, while owning
// every mutable field both on publication and on each caller's return path.
func cloneUsageStats(stats *usagestats.UsageStats) *usagestats.UsageStats {
	if stats == nil {
		return nil
	}
	copy := *stats
	if stats.TotalAccountCost != nil {
		cost := *stats.TotalAccountCost
		copy.TotalAccountCost = &cost
	}
	copy.Endpoints = slices.Clone(stats.Endpoints)
	copy.UpstreamEndpoints = slices.Clone(stats.UpstreamEndpoints)
	copy.EndpointPaths = slices.Clone(stats.EndpointPaths)
	return &copy
}
