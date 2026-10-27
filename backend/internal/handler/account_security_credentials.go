package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// accountCredentialRequest accepts the authenticated native subject only. The
// bounded strict decoder rejects caller UID/generation/proof and trailing JSON.
func (h *AuthHandler) accountCredentialRequest(c *gin.Context, input any) (int64, string, bool) {
	c.Header("Cache-Control", "no-store")
	if h.authService == nil || !h.authService.AccountCenterEnabled() {
		response.NotFound(c, "Account center disabled")
		return 0, "", false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, "", false
	}
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || strings.TrimSpace(strings.TrimPrefix(values[0], "Bearer ")) == "" {
		response.Unauthorized(c, "Native bearer authentication required")
		return 0, "", false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		response.BadRequest(c, "Invalid credential change request")
		return 0, "", false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Invalid credential change request")
		return 0, "", false
	}
	if err := binding.Validator.ValidateStruct(input); err != nil {
		response.BadRequest(c, "Invalid credential change request")
		return 0, "", false
	}
	// HTTP-observed IP/UA, never JSON or client proof fields; includes the fixture
	// which intentionally mounts the handlers without global session middleware.
	if service.SessionBindingFromContext(c.Request.Context()) == nil {
		c.Request = c.Request.WithContext(service.WithSessionBinding(c.Request.Context(), &service.SessionBinding{IP: middleware2.SecurityClientIP(c), UserAgent: c.Request.UserAgent()}))
	}
	middleware2.SetAuditActor(c, subject.UserID, c.GetString(middleware2.ContextKeyAuthEmail))
	return subject.UserID, strings.TrimPrefix(values[0], "Bearer "), true
}

func (h *AuthHandler) AccountSecurityPassword(c *gin.Context) {
	var input ChangePasswordRequest
	userID, token, ok := h.accountCredentialRequest(c, &input)
	if !ok {
		return
	}
	if input.OldPassword == "" || input.NewPassword == "" {
		response.BadRequest(c, "Invalid credential change request")
		return
	}
	middleware2.SetAuditAction(c, "user.account_security.password")
	result, err := h.authService.ChangeAccountSecurityPassword(c.Request.Context(), userID, token, input.OldPassword, input.NewPassword)
	h.respondAccountCredentialChange(c, result, err)
}

func (h *AuthHandler) AccountSecurityEmail(c *gin.Context) {
	var input BindEmailIdentityRequest
	userID, token, ok := h.accountCredentialRequest(c, &input)
	if !ok {
		return
	}
	if input.Email == "" || input.VerifyCode == "" || input.Password == "" {
		response.BadRequest(c, "Invalid credential change request")
		return
	}
	middleware2.SetAuditAction(c, "user.account_security.email")
	result, err := h.authService.ChangeAccountSecurityEmail(c.Request.Context(), userID, token, input.Email, input.VerifyCode, input.Password)
	h.respondAccountCredentialChange(c, result, err)
}

func (h *AuthHandler) respondAccountCredentialChange(c *gin.Context, result *service.AccountCredentialChange, err error) {
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// No legacy AT-only fallback and no second token mint from a reloaded user.
	response.Success(c, AuthResponse{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresIn: result.ExpiresIn, RefreshExpiresAt: result.RefreshExpiresAt, TokenType: "Bearer", User: dto.UserFromService(result.User)})
}
