package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *PaymentHandler) RequestSquarespaceClaimChallenge(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	var req struct {
		LocalOrderID       int64  `json:"local_order_id" binding:"required,gt=0"`
		ReceiptOrderNumber string `json:"receipt_order_number" binding:"required,max=64"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid receipt claim request")
		return
	}
	result, err := h.paymentService.RequestSquarespaceClaimChallenge(c.Request.Context(), subject.UserID, req.LocalOrderID, req.ReceiptOrderNumber)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *PaymentHandler) ClaimSquarespaceReceipt(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	var req struct {
		ChallengeToken string `json:"challenge_token" binding:"required,max=128"`
		Code           string `json:"code" binding:"required,len=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid receipt claim verification")
		return
	}
	order, err := h.paymentService.ClaimSquarespaceReceipt(c.Request.Context(), subject.UserID, req.ChallengeToken, req.Code)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sanitizePaymentOrderForResponse(order))
}
