package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"strings"
	"time"
)

func nativeAccountBearer(c *gin.Context) string {
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}
func (h *AuthHandler) AccountRecoveryRemaining(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	n, err := h.authService.RemainingAccountRecoveryCodes(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"remaining": n})
}
func (h *AuthHandler) AccountRecoveryReplace(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if _, err := h.authService.RequireRecentAccountAuthentication(c.Request.Context(), nativeAccountBearer(c), subject.UserID, true); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	codes, err := h.authService.ReplaceAccountRecoveryCodes(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"recovery_codes": codes, "remaining": len(codes)})
}
func (h *AuthHandler) AccountRecoveryLogin(c *gin.Context) {
	if !h.authService.AccountCenterEnabled() || h.totpService == nil {
		response.NotFound(c, "Account center disabled")
		return
	}
	var input struct {
		TempToken string `json:"temp_token" binding:"required,max=128"`
		Code      string `json:"recovery_code" binding:"required,max=128"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid recovery request")
		return
	}
	session, err := h.totpService.GetLoginSession(c.Request.Context(), input.TempToken)
	if err != nil || session == nil || session.PendingOAuthBind != nil || session.TokenExpiry.Before(time.Now()) || session.Authentication.Generation == "" {
		response.Unauthorized(c, "Password authentication is required")
		return
	}
	passwordVerified := false
	for _, method := range session.Authentication.Methods {
		if method == "pwd" {
			passwordVerified = true
		}
	}
	now := time.Now().Unix()
	if !passwordVerified || session.Authentication.AuthTime <= 0 || session.Authentication.AuthTime > now+5 || now-session.Authentication.AuthTime > 300 {
		response.Unauthorized(c, "A recent password challenge is required")
		return
	}
	if err = h.totpService.CheckAccountRecoveryAttempt(c.Request.Context(), session.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err = h.authService.ConsumeAccountRecoveryCode(c.Request.Context(), session.UserID, session.Authentication.Generation, input.Code); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	consumed, err := h.totpService.ConsumeLoginSession(c.Request.Context(), input.TempToken)
	if err != nil || consumed == nil || consumed.UserID != session.UserID || consumed.Authentication.Generation != session.Authentication.Generation {
		response.Unauthorized(c, "Password authentication was already consumed")
		return
	}
	_ = h.totpService.ClearAccountRecoveryAttempts(c.Request.Context(), session.UserID)
	user, err := h.userService.GetByID(c.Request.Context(), session.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err = ensureLoginUserActive(user); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err = h.ensureBackendModeAllowsUser(c.Request.Context(), user); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	proof := session.Authentication
	proof.Methods = []string{"pwd", "recovery"}
	proof.AuthTime = time.Now().Unix()
	c.Request = c.Request.WithContext(service.WithNativeAuthenticationProof(c.Request.Context(), proof))
	middleware2.SetAuditActor(c, user.ID, user.Email)
	c.Set("auth_method", "recovery")
	h.authService.RecordSuccessfulLogin(c.Request.Context(), user.ID)
	respondWithTokenPair(c, h.authService, user)
}
