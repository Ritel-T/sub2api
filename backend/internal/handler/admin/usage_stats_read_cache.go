package admin

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// UsageStatsReadCache exposes the existing bounded stats cache to the user
// handler without duplicating eviction, singleflight, key or cloning logic.
// Each instance owns its entries; callers must authenticate and resolve all
// authorization filters before lookup. It never stores balances or billing writes.
type UsageStatsReadCache struct {
	cache *snapshotCache
}

func NewUsageStatsReadCache() *UsageStatsReadCache {
	return &UsageStatsReadCache{cache: newSnapshotCache(30 * time.Second)}
}

func (c *UsageStatsReadCache) GetOrLoad(ctx context.Context, scope string, filters usagestats.UsageLogFilters, load func() (*usagestats.UsageStats, error)) (*usagestats.UsageStats, bool, error) {
	if c == nil {
		stats, err := load()
		return cloneUsageStats(stats), false, err
	}
	key := mustMarshalDashboardCacheKey(struct {
		Scope   string `json:"scope"`
		Filters string `json:"filters"`
	}{scope, usageStatsCacheKey(filters)})
	return loadUsageStatsCached(c.cache, ctx, key, load)
}
