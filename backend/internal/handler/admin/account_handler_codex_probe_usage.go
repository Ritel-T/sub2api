package admin

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UpdateCodexUsageSnapshot persists an external probe's upstream quota headers
// without a second upstream query. Stale snapshots and changed identities are
// successful no-ops; only quota fields can be changed.
// POST /api/v1/admin/accounts/:id/codex-usage-snapshot
func (h *AccountHandler) UpdateCodexUsageSnapshot(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req struct {
		ObservedAt        time.Time         `json:"observed_at" binding:"required"`
		Headers           map[string]string `json:"headers" binding:"required"`
		ProxyID           json.RawMessage   `json:"observed_proxy_id"`
		AccessTokenSHA256 string            `json:"observed_access_token_sha256" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ProxyID) == 0 {
		response.BadRequest(c, "Invalid Codex usage observation")
		return
	}
	observed := service.CodexProbeUsageObservation{
		ObservedAt: req.ObservedAt, Headers: req.Headers, AccessTokenSHA256: req.AccessTokenSHA256,
	}
	if err := json.Unmarshal(req.ProxyID, &observed.ProxyID); err != nil {
		response.BadRequest(c, "Invalid observed proxy ID")
		return
	}
	result, err := h.rateLimitService.UpdateCodexUsageSnapshotFromProbe(c.Request.Context(), accountID, observed)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": accountID, "updated": result.Updated, "quota": result.Quota})
}
