package admin

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"strconv"
)

// MarkGatewayBorrowInitialReady acknowledges independently read-back defaults;
// the narrow CAS endpoint cannot change any defaults or allow service itself.
func (h *AccountHandler) MarkGatewayBorrowInitialReady(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var raw map[string]json.RawMessage
	if err = c.ShouldBindJSON(&raw); err != nil {
		response.BadRequest(c, "Invalid initial defaults observation")
		return
	}
	keys := []string{"observed_at", "expected_proxy_id", "credential_sha256", "expected_policy", "expected_config"}
	if len(raw) != len(keys) {
		response.BadRequest(c, "Invalid initial defaults observation")
		return
	}
	for _, key := range keys {
		if _, exists := raw[key]; !exists {
			response.BadRequest(c, "Invalid initial defaults observation")
			return
		}
	}
	body, err := json.Marshal(raw)
	if err != nil {
		response.BadRequest(c, "Invalid initial defaults observation")
		return
	}
	var o service.GatewayBorrowInitialReadyObservation
	if json.Unmarshal(body, &o) != nil {
		response.BadRequest(c, "Invalid initial defaults observation")
		return
	}
	result, err := h.rateLimitService.MarkGatewayBorrowInitialReady(c.Request.Context(), id, o)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "applied": result.Applied, "skipped": result.Skipped, "reason": result.Reason})
}
