package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"time"
)

func (h *PasskeyHandler) BeginAccountReauthentication(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !h.authService.AccountCenterEnabled() {
		response.NotFound(c, "Account center disabled")
		return
	}
	if !h.requirePasskeysEnabled(c) {
		return
	}
	assertion, token, err := h.passkeys.BeginAccountReauthentication(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, passkeyOptionsResponse{SessionToken: token, Options: assertion})
}
func (h *PasskeyHandler) FinishAccountReauthentication(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !h.authService.AccountCenterEnabled() {
		response.NotFound(c, "Account center disabled")
		return
	}
	if !h.requirePasskeysEnabled(c) {
		return
	}
	req, ok := bindPasskeyFinishRequest(c)
	if !ok {
		return
	}
	user, err := h.passkeys.FinishAccountReauthentication(c.Request.Context(), subject.UserID, req.SessionToken, cloneRequestWithJSON(c.Request, req.Credential))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if user.ID != subject.UserID {
		response.Unauthorized(c, "Passkey belongs to a different account")
		return
	}
	if err := h.ensureBackendModeAllowsUser(c.Request.Context(), user); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Request = c.Request.WithContext(service.WithNativeAuthenticationProof(c.Request.Context(), service.NativeAuthenticationProof{Methods: []string{"passkey"}, AuthTime: time.Now().Unix()}))
	respondWithTokenPair(c, h.authService, user)
}

func (h *PasskeyHandler) requireAccountSecurityProof(c *gin.Context, userID int64) bool {
	if !h.authService.AccountCenterEnabled() {
		return true
	}
	identity, err := h.authService.AccountCenterIdentity(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	claims, err := h.authService.RequireRecentAccountAuthentication(c.Request.Context(), nativeAccountBearer(c), userID, identity.TotpEnabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	c.Request = c.Request.WithContext(service.WithNativeAuthenticationProof(c.Request.Context(), service.NativeAuthenticationFromClaims(claims)))
	return true
}
