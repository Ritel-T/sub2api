package admin

import (
	"context"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) PreviewBalanceRecovery(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	capability, ok := h.adminService.(service.AccountBalanceRecoveryAdmin)
	if !ok {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("BALANCE_RECOVERY_UNAVAILABLE", "balance recovery unavailable"))
		return
	}
	preview, err := capability.PreviewAccountBalanceRecovery(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *AccountHandler) RecoverBalance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req service.AccountBalanceRecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if err := service.ValidateAccountBalanceRecoveryRequest(req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if key == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	if service.DefaultIdempotencyCoordinator() == nil {
		response.ErrorFrom(c, service.ErrIdempotencyStoreUnavail)
		return
	}
	capability, ok := h.adminService.(service.AccountBalanceRecoveryAdmin)
	if !ok {
		response.ErrorFrom(c, infraerrors.ServiceUnavailable("BALANCE_RECOVERY_UNAVAILABLE", "balance recovery unavailable"))
		return
	}
	// Native admin authentication is the authority. confirm is a review contract,
	// not a substitute for the caller's admin identity or a TG owner callback.
	middleware.SetAuditAction(c, "admin.accounts.balance_recover")
	middleware.SetAuditExtra(c, map[string]any{"source": "balance_recovery", "reason_code": "explicit_balance_error", "enabled": *req.TargetSchedulable})
	payload := struct {
		AccountID int64                                 `json:"account_id"`
		Body      service.AccountBalanceRecoveryRequest `json:"body"`
	}{id, req}
	executeAdminIdempotentJSON(c, "admin.accounts.balance_recover", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) { return capability.RecoverAccountBalance(ctx, id, req) })
}
