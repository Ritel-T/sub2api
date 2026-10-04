package admin

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"strconv"
)

// UpdateGatewayBorrowPolicy is a narrow, identity-bound quality reconciliation;
// it cannot enable scheduling, replace credentials, or clear runtime limits.
// POST /api/v1/admin/accounts/:id/gateway-borrow-policy
func (h *AccountHandler) UpdateGatewayBorrowPolicy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var raw map[string]json.RawMessage
	if err = c.ShouldBindJSON(&raw); err != nil {
		response.BadRequest(c, "Invalid gateway borrow policy observation")
		return
	}
	for key := range raw {
		switch key {
		case "observed_at", "expected_proxy_id", "credential_sha256", "expected_policy", "borrow_models", "model_results", "retire_bps":
		default:
			response.BadRequest(c, "Invalid gateway borrow policy observation")
			return
		}
	}
	for _, key := range []string{"observed_at", "expected_proxy_id", "credential_sha256", "expected_policy", "borrow_models"} {
		if _, exists := raw[key]; !exists {
			response.BadRequest(c, "Invalid gateway borrow policy observation")
			return
		}
	}
	body, err := json.Marshal(raw)
	if err != nil {
		response.BadRequest(c, "Invalid gateway borrow policy observation")
		return
	}
	var observed service.GatewayBorrowPolicyObservation
	if err = json.Unmarshal(body, &observed); err != nil {
		response.BadRequest(c, "Invalid gateway borrow policy observation")
		return
	}
	result, err := h.rateLimitService.UpdateGatewayBorrowPolicyFromProbe(c.Request.Context(), id, observed)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "applied": result.Applied, "skipped": result.Skipped, "reason": result.Reason})
}
