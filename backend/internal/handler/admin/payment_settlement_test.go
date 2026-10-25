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
