package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetAffiliateMigration handles GET /api/v1/user/aff/migration.
func (h *UserHandler) GetAffiliateMigration(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, err := h.affiliateService.GetAffiliateMigration(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// BindMigrationInviter handles POST /api/v1/user/aff/migration.
func (h *UserHandler) BindMigrationInviter(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req struct {
		InviterKey string `json:"inviter_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrAffiliateInviterInvalid)
		return
	}
	result, err := h.affiliateService.BindMigrationInviter(c.Request.Context(), subject.UserID, req.InviterKey, h.userService)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
