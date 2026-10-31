package admin

import (
	"encoding/json"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"strings"
	"testing"
	"time"
)

func TestAdminPaymentSettlementVerifiedEvidence(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, channel, verification, rate, paymentType string
		paid, want                                     bool
	}{
		{"android", "wechat_virtual", "verified", "100", "gelv", true, true},
		{"ios", "apple_iap", "verified", "1200", "gelv", true, true},
		{"missing evidence", "wechat_virtual", "", "100", "gelv", true, false},
		{"conflict", "wechat_virtual", "conflict", "100", "gelv", true, false},
		{"wrong rate", "wechat_virtual", "verified", "1200", "gelv", true, false},
		{"unknown channel", "android", "verified", "100", "gelv", true, false},
		{"pending", "wechat_virtual", "verified", "100", "gelv", false, false},
		{"other provider", "wechat_virtual", "verified", "100", "stripe", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &dbent.PaymentOrder{PaymentType: tc.paymentType, Amount: 9.9, PayAmount: 10, ProviderSnapshot: map[string]any{
				"gelv_payment_channel": tc.channel, "gelv_channel_verification_status": tc.verification, "gelv_fee_rate_bps": tc.rate, "secret": "must-not-leak",
			}}
			if tc.paid {
				o.PaidAt = &now
			}
			got := sanitizeAdminPaymentOrderForResponse(o)
			if (got.Settlement != nil) != tc.want {
				t.Fatalf("settlement=%+v want=%v", got.Settlement, tc.want)
			}
			if tc.want && (got.Settlement.Channel != tc.channel || !got.Settlement.Verified) {
				t.Fatalf("wrong evidence: %+v", got.Settlement)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "must-not-leak") || strings.Contains(string(b), "provider_snapshot") {
				t.Fatalf("snapshot exposed: %s", b)
			}
			if !tc.want && strings.Contains(string(b), "settlement") {
				t.Fatalf("unverified evidence exposed: %s", b)
			}
			if got.Amount != 9.9 || got.PayAmount != 10 {
				t.Fatal("display projection changed financial amounts")
			}
		})
	}
	if adminPaymentSettlement(nil) != nil {
		t.Fatal("nil order must have no settlement")
	}
}

func TestAdminGelvAlipaySettlementUnknownsAndRefundHold(t *testing.T) {
	now := time.Now()
	artifact := "checkout-secret-token"
	order := &dbent.PaymentOrder{PaymentType: "gelv_alipay", Status: "PAID", PaidAt: &now, PayURL: &artifact, QrCode: &artifact, QrCodeImg: &artifact, ProviderSnapshot: map[string]any{
		"gelv_payment_channel": "alipay_web", "gelv_checkout_channel": "alipay_web", "gelv_channel_verification_status": "verified", "gelv_alipay_fee_bps": "50", "gelv_alipay_trade_no": "alipay-identity-123", "gelv_wx_order_id": "alipay:alipay-identity-123", "gelv_refund_pending": "true", "gelv_refunded_minor": "0",
	}}
	if adminPaymentSettlement(order) != nil {
		t.Fatal("unknown actual platform rate cannot be substituted from partner agreement")
	}
	result := sanitizeAdminPaymentOrderForResponse(order)
	if !result.RefundReconciliationPending || result.PayURL != nil || result.QRCode != nil || result.QRCodeImg != nil {
		t.Fatal("held checkout artifacts were exposed")
	}
	if result.RefundAmount != 0 || result.Status != "PAID" {
		t.Fatal("hold must preserve financial status")
	}
	order.ProviderSnapshot["gelv_fee_rate_bps"] = "9876"
	evidence := adminPaymentSettlement(order)
	if evidence == nil || evidence.FeeRateBps != 9876 {
		t.Fatal("known actual rate must remain separate from partner agreement")
	}
}
