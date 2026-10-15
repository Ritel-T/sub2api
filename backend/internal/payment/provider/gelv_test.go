package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type gelvTransport func(*http.Request) (*http.Response, error)

func (f gelvTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const gelvTestID = "0123456789abcdef01234567"
const gelvTestReference = "sub2_test_source_order_0001"
const gelvTestPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3eEAAAAASUVORK5CYII="

func gelvTestConfig() map[string]string {
	return map[string]string{"partnerId": "site-test", "partnerSecret": strings.Repeat("fake-test-secret-", 3), "productIds": "3,4,5,6,7,8", "credit10": "9.60", "credit30": "28.80", "credit50": "48", "credit100": "96", "credit200": "192", "credit500": "480"}
}
func gelvTestOrder() map[string]any {
	return map[string]any{"id": gelvTestID, "partnerOrderId": gelvTestReference, "amount": 1000, "currency": "CNY", "productId": "3", "status": "pending", "refunded": 0, "paymentChannel": "wechat_virtual", "channelVerified": true, "channelVerificationStatus": "verified", "feeRateBps": 100}
}
func gelvJSONResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}
}
func newGelvTest(t *testing.T) *Gelv {
	t.Helper()
	g, err := NewGelv("1", gelvTestConfig())
	require.NoError(t, err)
	return g
}

func TestGelvCreateUsesExactSignedBodyAndStableReferenceOnUnknownResult(t *testing.T) {
	g := newGelvTest(t)
	var bodies, nonces []string
	g.client.Transport = gelvTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, GelvAPIBase, r.URL.Scheme+"://"+r.URL.Host)
		require.Equal(t, "/api/partner/v1/orders", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"partnerOrderId":"`+gelvTestReference+`","amount":1000}`, string(raw))
		ts, err := strconv.ParseInt(r.Header.Get("X-Gelv-Timestamp"), 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.UnixMilli(ts), time.Second)
		nonce := r.Header.Get("X-Gelv-Nonce")
		require.Len(t, nonce, 32)
		// Independent verification of the exact received bytes and canonical route.
		hash := sha256.Sum256(raw)
		mac := hmac.New(sha256.New, []byte(gelvTestConfig()["partnerSecret"]))
		_, _ = mac.Write([]byte("POST\n/api/partner/v1/orders\n" + r.Header.Get("X-Gelv-Timestamp") + "\n" + nonce + "\n" + hex.EncodeToString(hash[:])))
		require.Equal(t, hex.EncodeToString(mac.Sum(nil)), r.Header.Get("X-Gelv-Signature"))
		bodies = append(bodies, string(raw))
		nonces = append(nonces, nonce)
		if len(bodies) == 1 {
			return nil, errors.New("secret upstream transport text must not escape")
		}
		return gelvJSONResponse(t, 200, map[string]any{"order": gelvTestOrder(), "image": gelvTestPNG}), nil
	})
	req := payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10.00", PaymentType: payment.TypeGelv}
	_, err := g.CreatePayment(context.Background(), req)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret upstream")
	out, err := g.CreatePayment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, gelvTestPNG, out.QRCode)
	require.Equal(t, gelvTestID, out.TradeNo)
	require.Equal(t, bodies[0], bodies[1])
	require.NotEqual(t, nonces[0], nonces[1])
}

func TestGelvRejectsWrongSourceProductMoneyAndUnsafeImages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		image  string
	}{
		{"foreign_reference", func(o map[string]any) { o["partnerOrderId"] = "other_order_123" }, gelvTestPNG},
		{"wrong_amount", func(o map[string]any) { o["amount"] = 3000 }, gelvTestPNG},
		{"wrong_product", func(o map[string]any) { o["productId"] = 999 }, gelvTestPNG},
		{"wrong_allowed_tier", func(o map[string]any) { o["productId"] = "4" }, gelvTestPNG},
		{"numeric_product_id", func(o map[string]any) { o["productId"] = 3 }, gelvTestPNG},
		{"wrong_currency", func(o map[string]any) { o["currency"] = "USD" }, gelvTestPNG},
		{"fractional_minor", func(o map[string]any) { o["amount"] = 1000.1 }, gelvTestPNG},
		{"no_refund_evidence", func(o map[string]any) { delete(o, "refunded") }, gelvTestPNG},
		{"svg", func(map[string]any) {}, "data:image/svg+xml;base64,PHN2Zz4="},
		{"remote_image", func(map[string]any) {}, "https://example.com/track.png"},
		{"fake_png", func(map[string]any) {}, "data:image/png;base64,YWJjZA=="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGelvTest(t)
			o := gelvTestOrder()
			tc.change(o)
			g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
				return gelvJSONResponse(t, 200, map[string]any{"order": o, "image": tc.image}), nil
			})
			_, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10", PaymentType: payment.TypeGelv})
			require.Error(t, err)
		})
	}
}

func TestGelvQueryRequiresCompletePaidEvidenceAndKeepsRefundSeparate(t *testing.T) {
	for _, status := range []string{"pending", "paid", "partially_refunded", "refunded", "closed"} {
		t.Run(status, func(t *testing.T) {
			g := newGelvTest(t)
			o := gelvTestOrder()
			o["status"] = status
			o["wxOrderId"] = "wx-test-transaction"
			if strings.Contains(status, "refunded") {
				o["refunded"] = 500
			}
			if status == "refunded" {
				o["refunded"] = 1000
			}
			g.client.Transport = gelvTransport(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/partner/v1/order", r.URL.Path)
				return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
			})
			out, err := g.QueryOrder(context.Background(), gelvTestID)
			require.NoError(t, err)
			require.Equal(t, "1000", out.Metadata["amount_minor"])
			require.Equal(t, gelvTestReference, out.Metadata["partner_order_id"])
			if status == "paid" {
				require.Equal(t, payment.ProviderStatusPaid, out.Status)
				delete(o, "wxOrderId")
				_, err = g.QueryOrder(context.Background(), gelvTestID)
				require.Error(t, err)
			}
			if strings.Contains(status, "refunded") {
				require.Equal(t, payment.ProviderStatusRefunded, out.Status)
			}
		})
	}
}

func TestGelvNoUnapprovedDenominationOrFabricatedWebhookRefund(t *testing.T) {
	g := newGelvTest(t)
	called := false
	g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) { called = true; return nil, errors.New("unexpected") })
	for _, amount := range []string{"0", "-10", "10.001", "10.01", "20", "NaN", "1e100"} {
		_, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: amount, PaymentType: payment.TypeGelv})
		require.Error(t, err)
	}
	require.False(t, called)
	_, err := g.VerifyNotification(context.Background(), `{"status":"paid"}`, nil)
	require.ErrorIs(t, err, ErrGelvUnsupported)
	_, err = g.Refund(context.Background(), payment.RefundRequest{})
	require.ErrorIs(t, err, ErrGelvUnsupported)
	for _, key := range []string{"partnerId", "partnerSecret", "productIds"} {
		cfg := gelvTestConfig()
		delete(cfg, key)
		_, err = NewGelv("1", cfg)
		require.Error(t, err)
	}
	for _, channel := range []string{"android", "ios", "unknown", ""} {
		_, err = GelvCredits(1000, channel)
		require.Error(t, err)
	}
	android, err := GelvCredits(1000, "wechat_virtual")
	require.NoError(t, err)
	require.Equal(t, 9.9, android)
	ios, err := GelvCredits(1000, "apple_iap")
	require.NoError(t, err)
	require.Equal(t, 8.8, ios)
}

func TestGelvPaidChannelMustComeFromVerifiedUpstreamNotDeviceHint(t *testing.T) {
	for _, tc := range []struct {
		channel  string
		verified bool
	}{{"", false}, {"android", true}, {"ios", true}, {"apple_iap", false}, {"wechat_virtual", false}} {
		t.Run(tc.channel+strconv.FormatBool(tc.verified), func(t *testing.T) {
			g := newGelvTest(t)
			o := gelvTestOrder()
			o["status"] = "paid"
			o["wxOrderId"] = "wx-receipt"
			o["paymentChannel"] = tc.channel
			o["channelVerified"] = tc.verified
			g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
				return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
			})
			_, err := g.QueryOrder(context.Background(), gelvTestID)
			require.Error(t, err)
			require.Contains(t, err.Error(), "paid_channel_unverified")
		})
	}
}

func TestGelvFormalProviderFixtureUsesStringProductAndPreservesUnknownFees(t *testing.T) {
	raw, err := os.ReadFile("testdata/gelv_formal_refunded.json")
	require.NoError(t, err)
	var document struct {
		Order map[string]any `json:"order"`
	}
	require.NoError(t, json.Unmarshal(raw, &document))
	o := document.Order
	g := newGelvTest(t)
	g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) { return gelvJSONResponse(t, 200, document), nil })
	id, ok := o["id"].(string)
	require.True(t, ok)
	result, err := g.QueryOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusRefunded, result.Status)
	require.Equal(t, "3", result.Metadata["product_id"])
	require.Equal(t, "1000", result.Metadata["refunded_minor"])
	require.Equal(t, "100", result.Metadata["fee_rate_bps"])
	require.Equal(t, "verified", result.Metadata["channel_verification_status"])
	_, exists := result.Metadata["platform_fee_fen"]
	require.False(t, exists, "unknown settlement fee must not become zero")
	// These are isolated fixture mutations, not additional live payments.
	o["status"] = "paid"
	o["refunded"] = 0
	result, err = g.QueryOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPaid, result.Status)
	o["feeRateBps"] = 1200
	_, err = g.QueryOrder(context.Background(), id)
	require.ErrorContains(t, err, "paid_fee_rule_mismatch")
	o["paymentChannel"] = "apple_iap"
	result, err = g.QueryOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "1200", result.Metadata["fee_rate_bps"])
	o["channelVerificationStatus"] = "conflict"
	o["channelVerified"] = false
	_, err = g.QueryOrder(context.Background(), id)
	require.ErrorContains(t, err, "paid_channel_conflict")
	o["status"] = "refunded"
	o["refunded"] = 1000
	result, err = g.QueryOrder(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusRefunded, result.Status)
	delete(o, "currency")
	_, err = g.QueryOrder(context.Background(), id)
	require.Error(t, err)
}
