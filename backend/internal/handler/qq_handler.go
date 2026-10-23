package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

func (h *UserHandler) QQStatus(c *gin.Context) {
	u, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	v, e := h.qq.Status(c.Request.Context(), u.UserID)
	if e != nil {
		response.Error(c, 500, "QQ unavailable")
		return
	}
	response.Success(c, v)
}
func (h *UserHandler) QQIssue(c *gin.Context) {
	u, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	v, e := h.qq.Issue(c.Request.Context(), u.UserID)
	if e != nil {
		response.BadRequest(c, "QQ binding unavailable or requested too frequently")
		return
	}
	response.Success(c, v)
}
func (h *UserHandler) QQConfirm(c *gin.Context) {
	u, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	var v struct {
		QQ        string `json:"qq"`
		BindingID string `json:"binding_id"`
	}
	if c.ShouldBindJSON(&v) != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	if h.qq.Confirm(c.Request.Context(), u.UserID, v.QQ, v.BindingID) != nil {
		response.BadRequest(c, "Binding expired or conflicting")
		return
	}
	response.Success(c, map[string]bool{"ok": true})
}
func (h *UserHandler) QQUnbind(c *gin.Context) {
	u, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	if h.qq.Unbind(c.Request.Context(), u.UserID) != nil {
		response.Error(c, 500, "QQ unavailable")
		return
	}
	response.Success(c, map[string]bool{"ok": true})
}
func (h *UserHandler) QQPreferences(c *gin.Context) {
	u, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	var v service.QQPreferences
	if c.ShouldBindJSON(&v) != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	if h.qq.Preferences(c.Request.Context(), u.UserID, v) != nil {
		response.BadRequest(c, "Bind QQ before selecting QQ notifications")
		return
	}
	response.Success(c, map[string]bool{"ok": true})
}
func (h *UserHandler) QQWebhook(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	body, e := io.ReadAll(c.Request.Body)
	if e != nil {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	if !h.qq.Authenticate(body, c.GetHeader("X-QQ-Signature")) {
		c.Status(http.StatusUnauthorized)
		return
	}
	if h.qq.Webhook(c.Request.Context(), body) != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	c.Status(http.StatusNoContent)
}

func ProvideUserHandlerWithQQ(user *service.UserService, auth *service.AuthService, email *service.EmailService, cache service.EmailCache, aff *service.AffiliateService, quotas service.UserPlatformQuotaRepository, qq *service.QQNotificationService) *UserHandler {
	h := NewUserHandler(user, auth, email, cache, aff, quotas)
	h.qq = qq
	return h
}
