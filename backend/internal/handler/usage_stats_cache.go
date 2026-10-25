package handler

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/gin-gonic/gin"
)

// Called only after parseUserUsageFilters has authenticated the subject,
// checked key ownership and applied observer restrictions. Keep rolling ranges
// uncached: rounding now or explicit boundaries would change the time contract.
func (h *UsageHandler) historicalStats(c *gin.Context, parsed *userUsageFilters) (*usagestats.UsageStats, error) {
	load := func() (*usagestats.UsageStats, error) {
		return h.usageService.GetStatsWithFilters(c.Request.Context(), parsed.Filters)
	}
	nocache := false
	switch strings.ToLower(strings.TrimSpace(c.Query("nocache"))) {
	case "1", "true", "yes", "on":
		nocache = true
	}
	if nocache || strings.TrimSpace(c.Query("start_date")) == "" || strings.TrimSpace(c.Query("end_date")) == "" || parsed.EndTime.After(time.Now()) {
		c.Header("X-Usage-Stats-Cache", "bypass")
		return load()
	}
	scope := "user"
	if isObserverUsageRequest(c) {
		scope = "observer"
	}
	stats, hit, err := h.statsCache.GetOrLoad(c.Request.Context(), scope, parsed.Filters, load)
	status := "miss"
	if hit {
		status = "hit"
	}
	c.Header("X-Usage-Stats-Cache", status)
	return stats, err
}
