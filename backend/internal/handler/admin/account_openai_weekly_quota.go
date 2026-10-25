package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetOpenAIWeeklyQuota returns aggregate persisted quota for the entire OAuth
// pool in a single bounded scan, not a transactional point-in-time snapshot.
// Query parameters intentionally have no effect. No upstream or model call
// and no credential/account identity is included, even when a list call fails.
// GET /api/v1/admin/accounts/openai-weekly-quota
func (h *AccountHandler) GetOpenAIWeeklyQuota(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.adminService == nil {
		response.Error(c, http.StatusServiceUnavailable, "weekly quota unavailable; account service unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	summary, err := service.CollectOpenAIWeeklyQuota(ctx, h.adminService, time.Now().UTC())
	if err != nil {
		// List errors may contain account metadata; keep the public error generic.
		response.Error(c, http.StatusServiceUnavailable, "weekly quota unavailable; account list failed or changed, retry later")
		return
	}
	response.Success(c, summary)
}
