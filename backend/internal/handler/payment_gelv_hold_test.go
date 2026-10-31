package handler

import (
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGelvRefundHoldSuppressesCheckoutAndSignalsAllOrderResponses(t *testing.T) {
	url := "https://checkout.example.invalid/token"
	order := &dbent.PaymentOrder{PaymentType: "gelv_alipay", Status: service.OrderStatusPending, PayURL: &url, ProviderSnapshot: map[string]any{"gelv_refund_pending": "true", "gelv_refunded_minor": "0"}}
	require.Empty(t, psHandlerPaymentURL(order))
	for _, response := range []any{buildPublicOrderResult(order), buildPublicOrderVerifyResult(order), sanitizePaymentOrderForResponse(order)} {
		encoded, err := json.Marshal(response)
		require.NoError(t, err)
		var actual map[string]any
		require.NoError(t, json.Unmarshal(encoded, &actual))
		require.Equal(t, true, actual["refund_reconciliation_pending"])
		require.NotContains(t, string(encoded), url)
		require.Equal(t, service.OrderStatusPending, actual["status"])
	}
	require.Equal(t, &url, order.PayURL)
	order.ProviderSnapshot["gelv_refund_pending"] = "false"
	require.Equal(t, url, psHandlerPaymentURL(order))
}
