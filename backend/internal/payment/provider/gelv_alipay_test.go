package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

const gelvTestToken = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func gelvTestCheckoutURL() string {
	return GelvAPIBase + "/partner-pay/#id=" + gelvTestID + "&token=" + gelvTestToken
}

func gelvTestAlipayOrder() map[string]any {
	o := gelvTestOrder()
	o["checkoutChannel"], o["paymentChannel"] = "alipay_web", "alipay_web"
	o["feeRateBps"], o["platformFeeFen"], o["cpsFeeFen"] = nil, nil, nil
	return o
}

func TestGelvAlipaySharesProviderInstanceAndReturnsFrozenCheckout(t *testing.T) {
	g := newGelvTest(t)
	registry := payment.NewRegistry()
	registry.Register(g)
	for _, typ := range []string{payment.TypeGelv, payment.TypeGelvAlipay} {
		actual, err := registry.GetProvider(typ)
		require.NoError(t, err)
		require.Same(t, g, actual)
		require.Equal(t, payment.TypeGelv, registry.GetProviderKey(typ))
	}
	g.client.Transport = gelvTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, GelvAPIBase+"/api/partner/v1/orders", r.URL.String())
		require.Equal(t, g.partnerID, r.Header.Get("X-Gelv-Partner"))
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"partnerOrderId":"`+gelvTestReference+`","amount":1000,"paymentChannel":"alipay_web"}`, string(raw))
		return gelvJSONResponse(t, 200, map[string]any{"order": gelvTestAlipayOrder(), "checkoutUrl": gelvTestCheckoutURL(), "image": gelvTestPNG}), nil
	})
	result, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10", PaymentType: payment.TypeGelvAlipay})
	require.NoError(t, err)
	require.Equal(t, gelvTestCheckoutURL(), result.PayURL)
	require.Empty(t, result.QRCode)
	require.Equal(t, gelvTestID, result.TradeNo)
	require.Equal(t, "CNY", result.Currency)
	require.Equal(t, "alipay_web", result.Metadata["checkout_channel"])
	require.Equal(t, "alipay_web", result.Metadata["payment_channel"])
	require.Equal(t, "false", result.Metadata["refund_pending"])
	for _, value := range result.Metadata {
		require.NotContains(t, value, gelvTestToken)
	}
}

func TestGelvAlipayCheckoutURLAdversarial(t *testing.T) {
	good := gelvTestCheckoutURL()
	for name, raw := range map[string]string{
		"canonical": good,
		"reversed":  GelvAPIBase + "/partner-pay/#token=" + gelvTestToken + "&id=" + gelvTestID,
	} {
		t.Run(name, func(t *testing.T) { require.True(t, validGelvCheckoutURL(raw, gelvTestID)) })
	}
	for name, raw := range map[string]string{
		"empty":             "",
		"http":              strings.Replace(good, "https:", "http:", 1),
		"protocol_relative": strings.TrimPrefix(good, "https:"),
		"uppercase_scheme":  strings.Replace(good, "https:", "HTTPS:", 1),
		"foreign_host":      strings.Replace(good, "alar1c.cn", "evil.example", 1),
		"host_suffix":       strings.Replace(good, "alar1c.cn", "alar1c.cn.evil.example", 1),
		"host_dot":          strings.Replace(good, "alar1c.cn", "alar1c.cn.", 1),
		"uppercase_host":    strings.Replace(good, "alar1c.cn", "ALAR1C.CN", 1),
		"userinfo":          strings.Replace(good, "alar1c.cn", "user:pass@alar1c.cn", 1),
		"empty_userinfo":    strings.Replace(good, "alar1c.cn", "@alar1c.cn", 1),
		"default_port":      strings.Replace(good, "alar1c.cn", "alar1c.cn:443", 1),
		"empty_port":        strings.Replace(good, "alar1c.cn", "alar1c.cn:", 1),
		"other_port":        strings.Replace(good, "alar1c.cn", "alar1c.cn:8443", 1),
		"query":             strings.Replace(good, "/#", "/?x=1#", 1),
		"empty_query":       strings.Replace(good, "/#", "/?#", 1),
		"wrong_path":        strings.Replace(good, "/partner-pay/", "/partner-pay", 1),
		"double_slash":      strings.Replace(good, "/partner-pay/", "//partner-pay/", 1),
		"dot_path":          strings.Replace(good, "/partner-pay/", "/x/../partner-pay/", 1),
		"escaped_path":      strings.Replace(good, "/partner-pay/", "/%70artner-pay/", 1),
		"foreign_id":        strings.Replace(good, gelvTestID, strings.Repeat("a", 24), 1),
		"uppercase_id":      strings.Replace(good, gelvTestID, strings.ToUpper(gelvTestID), 1),
		"short_id":          strings.Replace(good, gelvTestID, gelvTestID[:23], 1),
		"uppercase_token":   strings.Replace(good, gelvTestToken, strings.ToUpper(gelvTestToken), 1),
		"short_token":       strings.Replace(good, gelvTestToken, gelvTestToken[:63], 1),
		"long_token":        good + "a",
		"nonhex_token":      good[:len(good)-1] + "g",
		"missing_token":     GelvAPIBase + "/partner-pay/#id=" + gelvTestID,
		"extra_key":         good + "&next=evil",
		"duplicate_id":      good + "&id=" + gelvTestID,
		"duplicate_token":   good + "&token=" + gelvTestToken,
		"duplicate_only_id": GelvAPIBase + "/partner-pay/#id=" + gelvTestID + "&id=" + gelvTestID,
		"escaped_key":       strings.Replace(good, "id=", "%69d=", 1),
		"escaped_token":     strings.Replace(good, "token=a", "token=%61", 1),
		"empty_extra_key":   good + "&",
		"semicolon":         strings.Replace(good, "&token", ";token", 1),
		"second_hash":       good + "#",
		"newline":           good + "\n",
		"leading_space":     " " + good,
		"backslash_host":    strings.Replace(good, "alar1c.cn", "alar1c.cn\\@evil.example", 1),
	} {
		t.Run(name, func(t *testing.T) {
			g := newGelvTest(t)
			g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
				return gelvJSONResponse(t, 200, map[string]any{"order": gelvTestAlipayOrder(), "checkoutUrl": raw}), nil
			})
			result, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10", PaymentType: payment.TypeGelvAlipay})
			require.Nil(t, result)
			require.ErrorContains(t, err, "invalid_alipay_checkout_url")
			require.NotContains(t, err.Error(), gelvTestToken)
		})
	}
}

func TestGelvCreateCheckoutTypesCannotMix(t *testing.T) {
	for _, tc := range []struct{ name, typ, checkout, actual, link string }{
		{"legacy_receives_alipay", payment.TypeGelv, "alipay_web", "alipay_web", gelvTestCheckoutURL()},
		{"alipay_receives_legacy", payment.TypeGelvAlipay, "wechat_virtual", "wechat_virtual", ""},
		{"alipay_missing_checkout", payment.TypeGelvAlipay, "", "alipay_web", gelvTestCheckoutURL()},
		{"alipay_legacy_payment", payment.TypeGelvAlipay, "alipay_web", "apple_iap", gelvTestCheckoutURL()},
		{"legacy_alipay_payment", payment.TypeGelv, "wechat_virtual", "alipay_web", ""},
		{"legacy_unexpected_link", payment.TypeGelv, "wechat_virtual", "wechat_virtual", gelvTestCheckoutURL()},
		{"unknown_checkout", payment.TypeGelv, "other", "wechat_virtual", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGelvTest(t)
			o := gelvTestOrder()
			o["checkoutChannel"], o["paymentChannel"] = tc.checkout, tc.actual
			g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
				return gelvJSONResponse(t, 200, map[string]any{"order": o, "checkoutUrl": tc.link, "image": gelvTestPNG}), nil
			})
			_, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10", PaymentType: tc.typ})
			require.ErrorContains(t, err, "checkout_channel")
		})
	}
}

func TestGelvLegacyCreateMissingCheckoutAndImageReplay(t *testing.T) {
	g := newGelvTest(t)
	for _, image := range []string{gelvTestPNG, ""} {
		g.client.Transport = gelvTransport(func(r *http.Request) (*http.Response, error) {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.NotContains(t, body, "paymentChannel")
			return gelvJSONResponse(t, 200, map[string]any{"order": gelvTestOrder(), "image": image}), nil
		})
		result, err := g.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10", PaymentType: payment.TypeGelv})
		require.NoError(t, err)
		require.Equal(t, image, result.QRCode)
		require.Empty(t, result.PayURL)
		require.Equal(t, "wechat_virtual", result.Metadata["checkout_channel"])
	}
}

func TestGelvAlipayQueryVerifiedEvidenceAndIndependentPlatformFees(t *testing.T) {
	for _, platformRate := range []any{nil, int64(0), int64(60), int64(100), int64(1200)} {
		t.Run(strconv.FormatBool(platformRate == nil)+"_"+gelvAlipayTestRateName(platformRate), func(t *testing.T) {
			g := newGelvTest(t)
			o := gelvTestAlipayOrder()
			o["status"], o["alipayTradeNo"], o["feeRateBps"] = "paid", "alipay-test-transaction", platformRate
			o["merchantOrderId"] = "P" + gelvTestID
			g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
				return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
			})
			out, err := g.QueryOrder(context.Background(), gelvTestID)
			require.NoError(t, err)
			require.Equal(t, payment.ProviderStatusPaid, out.Status)
			require.Equal(t, "alipay-test-transaction", out.Metadata["alipay_trade_no"])
			require.Empty(t, out.Metadata["wx_order_id"])
			require.Equal(t, "alipay_web", out.Metadata["checkout_channel"])
			require.Equal(t, "P"+gelvTestID, out.Metadata["merchant_order_id"])
			for _, key := range []string{"platform_fee_fen", "cps_fee_fen", "settled_at"} {
				require.NotContains(t, out.Metadata, key)
			}
			if platformRate == nil {
				require.NotContains(t, out.Metadata, "fee_rate_bps")
			} else {
				require.Equal(t, gelvAlipayTestRateName(platformRate), out.Metadata["fee_rate_bps"])
			}
			o["platformFeeFen"] = 0
			out, err = g.QueryOrder(context.Background(), gelvTestID)
			require.NoError(t, err)
			require.Equal(t, "0", out.Metadata["platform_fee_fen"])
		})
	}
}

func gelvAlipayTestRateName(value any) string {
	if value == nil {
		return "null"
	}
	return strconv.FormatInt(value.(int64), 10)
}

func TestGelvAlipayQueryRejectsMissingEvidenceAndMixedChannels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		kind   string
	}{
		{"missing_trade", func(o map[string]any) { delete(o, "alipayTradeNo"); o["wxOrderId"] = "wx-not-alipay-evidence" }, "incomplete_paid_evidence"},
		{"bad_trade", func(o map[string]any) { o["alipayTradeNo"] = "bad space" }, "incomplete_paid_evidence"},
		{"short_trade", func(o map[string]any) { o["alipayTradeNo"] = "short" }, "incomplete_paid_evidence"},
		{"unverified", func(o map[string]any) { o["channelVerified"] = false }, "paid_channel_unverified"},
		{"pending_verification", func(o map[string]any) { o["channelVerificationStatus"] = "pending" }, "paid_channel_unverified"},
		{"conflict", func(o map[string]any) { o["channelVerificationStatus"] = "conflict" }, "paid_channel_conflict"},
		{"missing_checkout", func(o map[string]any) { delete(o, "checkoutChannel") }, "checkout_channel_mismatch"},
		{"legacy_checkout", func(o map[string]any) { o["checkoutChannel"] = "wechat_virtual" }, "checkout_channel_mismatch"},
		{"legacy_payment", func(o map[string]any) { o["paymentChannel"] = "wechat_virtual"; o["wxOrderId"] = "wx-test-payment" }, "checkout_channel_mismatch"},
		{"apple_payment", func(o map[string]any) { o["paymentChannel"] = "apple_iap"; o["wxOrderId"] = "wx-test-payment" }, "checkout_channel_mismatch"},
		{"unknown_payment", func(o map[string]any) { o["paymentChannel"] = "unknown" }, "checkout_channel_mismatch"},
		{"unknown_checkout", func(o map[string]any) { o["checkoutChannel"] = "unknown" }, "invalid_checkout_channel"},
		{"confirmed_refund", func(o map[string]any) { o["refunded"] = 1 }, "incomplete_paid_evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGelvTest(t)
			o := gelvTestAlipayOrder()
			o["status"], o["alipayTradeNo"] = "paid", "alipay-test-transaction"
			tc.change(o)
			g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
				return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
			})
			result, err := g.QueryOrder(context.Background(), gelvTestID)
			require.Nil(t, result)
			require.ErrorContains(t, err, tc.kind)
		})
	}
}

func TestGelvRefundPendingAlwaysHoldsEligibilityAndRetainsEvidence(t *testing.T) {
	for _, channel := range []string{"wechat_virtual", "apple_iap", "alipay_web"} {
		for _, state := range []struct {
			status   string
			refunded int64
		}{{"pending", 0}, {"paid", 0}, {"partially_refunded", 500}, {"refunded", 1000}, {"closed", 0}} {
			t.Run(channel+"_"+state.status, func(t *testing.T) {
				g := newGelvTest(t)
				o := gelvTestOrder()
				o["status"], o["refunded"], o["refundPending"], o["paymentChannel"] = state.status, state.refunded, true, channel
				o["wxOrderId"], o["feeRateBps"] = "wx-test-transaction", 100
				if channel == "apple_iap" {
					o["feeRateBps"] = 1200
				}
				if channel == "alipay_web" {
					o["checkoutChannel"], o["alipayTradeNo"], o["feeRateBps"] = "alipay_web", "alipay-test-transaction", nil
				}
				g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
					return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
				})
				result, err := g.QueryOrder(context.Background(), gelvTestID)
				require.NoError(t, err)
				require.Equal(t, payment.ProviderStatusPending, result.Status)
				require.Equal(t, "true", result.Metadata["refund_pending"])
				require.Equal(t, "refund_pending", result.Metadata["reconciliation_hold"])
				require.Equal(t, state.status, result.Metadata["gelv_status"])
				require.Equal(t, strconv.FormatInt(state.refunded, 10), result.Metadata["refunded_minor"])
				require.Equal(t, channel, result.Metadata["payment_channel"])
				if state.status == "paid" {
					// A hold remains observable even before payment verification completes.
					o["channelVerified"], o["channelVerificationStatus"], o["wxOrderId"], o["alipayTradeNo"], o["feeRateBps"] = false, "conflict", "", "", nil
					result, err = g.QueryOrder(context.Background(), gelvTestID)
					require.NoError(t, err)
					require.Equal(t, payment.ProviderStatusPending, result.Status)
					require.Equal(t, "conflict", result.Metadata["channel_verification_status"])
				}
			})
		}
	}
}

func TestGelvAlipayAgreementRequiredAndNeverInferred(t *testing.T) {
	for _, config := range []map[string]string{nil, {}, gelvTestConfig(), {"alipayFeeRateBps": ""}, {"alipayFeeRateBps": " "}, {"feeRateBps": "60"}} {
		_, err := GelvAlipayFeeBPS(config)
		require.Error(t, err)
		options, err := GelvAlipayCreditOptions(config)
		require.Error(t, err)
		require.Nil(t, options)
	}
	for _, raw := range []string{"-1", "10000", "60.0", "1e2", "NaN", "Infinity", "+60", "0x64", "60bps", "99999999999999999999999999"} {
		_, err := GelvAlipayFeeBPS(map[string]string{"alipayFeeRateBps": raw})
		require.Error(t, err, raw)
	}
	for _, bps := range []int64{0, 1, 60, 9999} {
		config := map[string]string{"alipayFeeRateBps": strconv.FormatInt(bps, 10)}
		actual, err := GelvAlipayFeeBPS(config)
		require.NoError(t, err)
		require.Equal(t, bps, actual)
		options, err := GelvAlipayCreditOptions(config)
		require.NoError(t, err)
		require.Len(t, options, 6)
		for i, cash := range []int64{10, 30, 50, 100, 200, 500} {
			expected, err := GelvAlipayCredits(cash*100, bps)
			require.NoError(t, err)
			require.Equal(t, float64(cash), options[i].CashCNY)
			require.Equal(t, expected, options[i].Credits)
			require.Zero(t, options[i].AndroidCredits)
			require.Zero(t, options[i].IOSCredits)
		}
	}
}

func TestGelvAlipayCreditsRoundFeeInFenBeforeConverting(t *testing.T) {
	for _, tc := range []struct {
		cash, bps int64
		want      float64
	}{
		{1000, 60, 9.94}, {1000, 0, 10}, {1000, 9999, 0}, {50000, 9999, 0.05},
		{101, 50, 1}, {100, 50, 0.99}, {99, 50, 0.99}, {1, 5000, 0}, {3, 5000, 0.01},
	} {
		got, err := GelvAlipayCredits(tc.cash, tc.bps)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "cash=%d bps=%d", tc.cash, tc.bps)
	}
	for _, tc := range []struct{ cash, bps int64 }{{0, 60}, {-1, 60}, {50001, 60}, {1000, -1}, {1000, 10000}} {
		_, err := GelvAlipayCredits(tc.cash, tc.bps)
		require.Error(t, err)
	}
}

func TestGelvAlipayRefundHoldClearsOnlyOnAuthoritativeQuery(t *testing.T) {
	g := newGelvTest(t)
	o := gelvTestAlipayOrder()
	o["status"], o["alipayTradeNo"], o["refundPending"] = "paid", "alipay-test-transaction", true
	g.client.Transport = gelvTransport(func(*http.Request) (*http.Response, error) {
		return gelvJSONResponse(t, 200, map[string]any{"order": o}), nil
	})
	held, err := g.QueryOrder(context.Background(), gelvTestID)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPending, held.Status)
	require.Equal(t, "refund_pending", held.Metadata["reconciliation_hold"])
	o["refundPending"] = false
	cleared, err := g.QueryOrder(context.Background(), gelvTestID)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusPaid, cleared.Status)
	require.Equal(t, "false", cleared.Metadata["refund_pending"])
	require.NotContains(t, cleared.Metadata, "reconciliation_hold")
	require.NotContains(t, cleared.Metadata, "fee_rate_bps")
	require.Equal(t, "true", held.Metadata["refund_pending"], "previous response evidence remains frozen")
}

func TestGelvAlipayUnknownCreateKeepsIdentityAndChannelOnCallerRetry(t *testing.T) {
	g := newGelvTest(t)
	var bodies, nonces []string
	g.client.Transport = gelvTransport(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(raw))
		nonces = append(nonces, r.Header.Get("X-Gelv-Nonce"))
		if len(bodies) == 1 {
			return nil, errors.New("untrusted transport body " + gelvTestToken)
		}
		return gelvJSONResponse(t, 200, map[string]any{"order": gelvTestAlipayOrder(), "checkoutUrl": gelvTestCheckoutURL()}), nil
	})
	req := payment.CreatePaymentRequest{OrderID: gelvTestReference, Amount: "10", PaymentType: payment.TypeGelvAlipay}
	result, err := g.CreatePayment(context.Background(), req)
	require.Nil(t, result)
	require.ErrorContains(t, err, "transport_result_unknown")
	require.NotContains(t, err.Error(), gelvTestToken)
	require.Len(t, bodies, 1, "unknown create must not be retried automatically")
	result, err = g.CreatePayment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, gelvTestCheckoutURL(), result.PayURL)
	require.Equal(t, bodies[0], bodies[1])
	require.Contains(t, bodies[1], `"paymentChannel":"alipay_web"`)
	require.NotEqual(t, nonces[0], nonces[1])
}
