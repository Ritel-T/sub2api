package handler

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSquarespacePauseUserResponseWithholdsCheckoutURL(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	url := "https://synthetic.test/checkout"
	key := "squarespace"
	order := &dbent.PaymentOrder{PaymentType: key, ProviderKey: &key, PayURL: &url, Amount: 10, Status: "PENDING"}
	require.Empty(t, sanitizePaymentOrderForResponse(order).PayURL)
	require.Empty(t, buildPublicOrderResult(order).PayURL)
	require.Equal(t, url, *order.PayURL)
	key = payment.TypeGelv
	order.PaymentType = key
	require.Equal(t, url, sanitizePaymentOrderForResponse(order).PayURL)
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "false")
	key = "squarespace"
	order.PaymentType = key
	require.Equal(t, url, sanitizePaymentOrderForResponse(order).PayURL)
}
