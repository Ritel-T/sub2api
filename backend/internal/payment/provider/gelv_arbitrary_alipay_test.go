package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestGelvAlipayArbitraryCashCreateAndPrecision(t *testing.T) {
	for _, tc := range []struct {
		amount string
		minor  int64
		valid  bool
	}{
		{"0.01", 1, true}, {"12.34", 1234, true}, {"50", 5000, true},
		{"50.01", 5001, false}, {"100", 10000, false}, {"0", 0, false},
		{"-1", 0, false}, {"0.001", 0, false}, {"12.345", 0, false},
		{"NaN", 0, false}, {"Infinity", 0, false}, {"184467440737095516.17", 0, false},
	} {
		t.Run(tc.amount, func(t *testing.T) {
			g := newGelvTest(t)
			calls := 0
			g.client.Transport = gelvTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				var body map[string]any
				require.NoError(t, json.Unmarshal(raw, &body))
				require.Equal(t, float64(tc.minor), body["amount"])
				require.Equal(t, "alipay_web", body["paymentChannel"])
				require.Equal(t, gelvTestReference, body["partnerOrderId"])
				require.NotContains(t, body, "productId")
				o := gelvTestAlipayOrder()
				o["amount"], o["productId"] = tc.minor, ""
				return gelvJSONResponse(t, 200, map[string]any{"order": o, "checkoutUrl": gelvTestCheckoutURL()}), nil
			})
			_, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: tc.amount, PaymentType: payment.TypeGelvAlipay})
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			} else {
				require.Error(t, err)
				require.Zero(t, calls)
			}
		})
	}
}

func TestGelvAlipayHistoricalQueryAboveNewCeiling(t *testing.T) {
	g := newGelvTest(t)
	g.client.Transport = gelvTransport(func(req *http.Request) (*http.Response, error) {
		o := gelvTestAlipayOrder()
		o["amount"], o["productId"], o["status"], o["alipayTradeNo"] = 10000, "6", "paid", "historical-alipay-trade"
		return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
	})
	got, err := g.QueryOrder(context.Background(), gelvTestID)
	require.NoError(t, err)
	require.Equal(t, 100.0, got.Amount)
	require.Equal(t, payment.ProviderStatusPaid, got.Status)
}

func TestGelvWeChatStillRequiresMappedTier(t *testing.T) {
	g := newGelvTest(t)
	_, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "12.34", PaymentType: payment.TypeGelv})
	require.Error(t, err)
	for _, minor := range []int64{1000, 3000, 5000, 10000, 20000, 50000} {
		_, err := GelvProductForMinor(minor)
		require.NoError(t, err)
	}
}

func TestGelvAlipayArbitraryFeeRounding(t *testing.T) {
	for _, tc := range []struct {
		minor int64
		want  float64
	}{{1, 0.01}, {1234, 12.27}, {5000, 49.70}, {83, 0.83}, {84, 0.83}, {250, 2.48}, {10000, 99.40}} {
		got, err := GelvAlipayCredits(tc.minor, 60)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

func TestGelvAlipayHistoricalUnknownCreateRecoversOnlyOriginalReference(t *testing.T) {
	g := newGelvTest(t)
	calls := 0
	g.client.Transport = gelvTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		o := gelvTestAlipayOrder()
		o["amount"], o["productId"] = 10000, "6"
		return gelvJSONResponse(t, 200, map[string]any{"order": o, "checkoutUrl": gelvTestCheckoutURL()}), nil
	})
	req := payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "100", PaymentType: payment.TypeGelvAlipay}
	_, err := g.CreatePayment(context.Background(), req)
	require.Error(t, err)
	require.Zero(t, calls)
	_, err = g.CreatePayment(WithGelvPaymentRecovery(context.Background(), "different-reference"), req)
	require.Error(t, err)
	require.Zero(t, calls)
	result, err := g.CreatePayment(WithGelvPaymentRecovery(context.Background(), gelvTestReference), req)
	require.NoError(t, err)
	require.Equal(t, gelvTestID, result.TradeNo)
	require.Equal(t, 1, calls)
}
