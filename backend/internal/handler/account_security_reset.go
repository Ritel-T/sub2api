package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountSecurityAdminReset is wired by the root under AdminAuth and the
// existing request audit. It accepts only a manual confirmation and note.
func (h *AuthHandler) AccountSecurityAdminReset(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.authService == nil || !h.authService.AccountCenterEnabled() {
		response.NotFound(c, "Account center disabled")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	middleware2.SetAuditActor(c, subject.UserID, c.GetString(middleware2.ContextKeyAuthEmail))
	middleware2.SetAuditAction(c, "admin.users.account_security.reset")
	rawID := c.Param("id")
	targetID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || targetID <= 0 || rawID != strconv.FormatInt(targetID, 10) {
		response.BadRequest(c, "Invalid target user ID")
		return
	}
	middleware2.SetAuditExtra(c, map[string]any{"target_user_id": targetID})
	var input struct {
		Confirmed        bool   `json:"confirmed"`
		VerificationNote string `json:"verification_note"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.BadRequest(c, "Invalid manual reset request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Invalid manual reset request")
		return
	}
	authorizations := c.Request.Header.Values("Authorization")
	if len(authorizations) != 1 || !strings.HasPrefix(authorizations[0], "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorizations[0], "Bearer ")) == "" {
		response.Unauthorized(c, "Native bearer authentication required")
		return
	}
	token := nativeAccountBearer(c)
	if _, err := h.authService.RequireRecentAccountAuthentication(c.Request.Context(), token, subject.UserID, false); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.authService.ResetAccountSecurity(c.Request.Context(), subject.UserID, targetID, token, input.Confirmed, input.VerificationNote); err != nil {
		result := "failed"
		if errors.Is(err, service.ErrAccountSecurityResetUnknown) {
			result = "unknown"
		}
		middleware2.SetAuditExtra(c, map[string]any{"result": result, "verification_note": service.AccountSecurityResetAuditNote(input.VerificationNote)})
		response.ErrorFrom(c, err)
		return
	}
	middleware2.SetAuditExtra(c, map[string]any{"result": "applied", "confirm": true, "verification_note": service.AccountSecurityResetAuditNote(input.VerificationNote)})
	response.Success(c, gin.H{"applied": true})
}
