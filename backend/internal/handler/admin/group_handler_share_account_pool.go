package admin

import (
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *GroupHandler) ShareAccountPool(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid group ID")
		return
	}
	var raw map[string]json.RawMessage
	if err = c.ShouldBindJSON(&raw); err != nil {
		response.BadRequest(c, "Invalid account pool observation")
		return
	}
	for _, key := range []string{"source_group_id", "expected_source_account_ids", "expected_target_account_ids"} {
		if _, ok := raw[key]; !ok {
			response.BadRequest(c, "Invalid account pool observation")
			return
		}
	}
	if len(raw) != 3 {
		response.BadRequest(c, "Invalid account pool observation")
		return
	}
	data, err := json.Marshal(raw)
	if err != nil {
		response.BadRequest(c, "Invalid account pool observation")
		return
	}
	var req service.ShareAccountPoolInput
	if err = json.Unmarshal(data, &req); err != nil {
		response.BadRequest(c, "Invalid account pool observation")
		return
	}
	if err = service.ValidateShareAccountPoolInput(id, req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	capability, ok := h.adminService.(service.AccountPoolSharingAdmin)
	if !ok {
		response.ErrorFrom(c, infraerrors.New(http.StatusServiceUnavailable, "ACCOUNT_POOL_SHARE_UNAVAILABLE", "Account pool sharing unavailable"))
		return
	}
	result, err := capability.ShareAccountPool(c.Request.Context(), id, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "source_group_id": req.SourceGroupID, "applied": result.Applied, "skipped": result.Skipped, "reason": result.Reason, "added_account_ids": result.AddedAccountIDs, "total_accounts": result.TotalAccounts})
}
