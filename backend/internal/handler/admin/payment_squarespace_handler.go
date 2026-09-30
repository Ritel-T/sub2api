package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"net/http"
)

// SquarespaceOAuthImport accepts a bootstrap pair on the existing AdminAuth
// payment group. The entire credential-bearing body is omitted from audit logs.
func (h *PaymentHandler) SquarespaceOAuthImport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var req struct {
		WebsiteID       string                             `json:"website_id" binding:"required"`
		ClientID        string                             `json:"client_id" binding:"required"`
		ClientSecret    string                             `json:"client_secret" binding:"required"`
		ExpectedVersion *int64                             `json:"expected_version" binding:"required"`
		Tokens          provider.SquarespaceOAuthTokenPair `json:"tokens" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid OAuth import request")
		return
	}
	if err := h.configService.ImportSquarespaceOAuth(c.Request.Context(), req.WebsiteID, req.ClientID, req.ClientSecret, *req.ExpectedVersion, &req.Tokens); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	status, err := h.configService.SquarespaceOAuthStatus(c.Request.Context(), req.WebsiteID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}
func (h *PaymentHandler) SquarespaceOAuthStatus(c *gin.Context) {
	websiteID := c.Query("website_id")
	if websiteID == "" {
		response.BadRequest(c, "website_id is required")
		return
	}
	status, err := h.configService.SquarespaceOAuthStatus(c.Request.Context(), websiteID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}
