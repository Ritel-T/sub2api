package handler

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountStepUp accepts only the authenticated native Bearer session. UID,
// session and authentication time are never read from the request body.
func (h *AuthHandler) AccountStepUp(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
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
		response.ErrorWithDetails(c, 403, "Admin API key cannot grant step-up", "STEP_UP_ADMIN_API_KEY_FORBIDDEN", nil)
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
	ttl, err := h.totpService.GrantAccountStepUp(c.Request.Context(), h.authService, subject.UserID,
		c.GetString(middleware2.ContextKeySessionID), strings.TrimSpace(parts[1]))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"success": true, "expires_in": int64(ttl / time.Second)})
}
