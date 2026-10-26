package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountSessionLogout revokes only the native Bearer session presented by the
// account center. No UID, family, password or recent-auth proof is accepted.
func (h *AuthHandler) AccountSessionLogout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	if !h.authService.AccountCenterEnabled() {
		response.ErrorWithDetails(c, 404, "Account center disabled", "ACCOUNT_CENTER_DISABLED", nil)
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if c.GetString("auth_method") == service.AuditAuthMethodAdminAPIKey {
		response.ErrorFrom(c, service.ErrInvalidToken)
		return
	}
	headers := c.Request.Header.Values("Authorization")
	if len(headers) != 1 {
		response.Unauthorized(c, "Native Bearer authentication required")
		return
	}
	parts := strings.SplitN(headers[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		response.Unauthorized(c, "Native Bearer authentication required")
		return
	}
	if err := h.authService.RevokeAccountSessionFamily(c.Request.Context(), subject.UserID,
		c.GetString(middleware2.ContextKeySessionID), strings.TrimSpace(parts[1])); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}
