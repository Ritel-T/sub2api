package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// accountWaitLimitForCaller makes authenticated administrators yield earlier
// only when a selected account needs waiting. Immediate slot acquisition and
// customer limits are unchanged. The limit is compared with the account-wide
// wait count, so separate API keys cannot create separate waiting allowances.
func accountWaitLimitForCaller(c *gin.Context, configured int) int {
	if configured > 1 && isAdminAccountWaitCaller(c) {
		return 1
	}
	return configured
}

func isAdminAccountWaitCaller(c *gin.Context) bool {
	if c == nil {
		return false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	return ok && role == service.RoleAdmin
}
