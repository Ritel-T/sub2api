package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *PaymentHandler) BuySubscriptionWithBalance(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	var req service.BalanceSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid subscription purchase")
		return
	}
	result, err := h.paymentService.BuySubscriptionWithBalance(c.Request.Context(), subject.UserID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
