package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountCenterIdentity is a service-to-service read-only authority endpoint.
// Public JWTs and admin credentials cannot substitute its dedicated credential.
func (h *AuthHandler) AccountCenterIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	if h.cfg == nil || !h.cfg.AccountCenter.Enabled || h.cfg.AccountCenter.Validate() != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	headers := c.Request.Header.Values("Authorization")
	expected := sha256.Sum256([]byte("Bearer " + h.cfg.AccountCenter.BridgeSecret))
	given := ""
	if len(headers) == 1 {
		given = headers[0]
	}
	actual := sha256.Sum256([]byte(given))
	if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
		response.Error(c, http.StatusUnauthorized, "Account service authentication required")
		return
	}
	subject := c.Param("subject")
	rawID := strings.TrimPrefix(subject, service.AccountCenterSubjectPrefix)
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 || subject != service.AccountCenterSubjectPrefix+strconv.FormatInt(id, 10) {
		response.Error(c, http.StatusBadRequest, "Invalid account subject")
		return
	}
	state, err := h.authService.AccountCenterIdentity(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.Error(c, http.StatusNotFound, "Account not found")
		} else {
			response.Error(c, http.StatusServiceUnavailable, "Account state unavailable")
		}
		return
	}
	response.Success(c, state)
}
