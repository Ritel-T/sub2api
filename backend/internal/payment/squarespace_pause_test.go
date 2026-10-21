package payment

import "testing"

func TestSquarespacePaymentsPauseSetting(t *testing.T) {
	for _, tc := range []struct {
		value  string
		paused bool
	}{{"", false}, {"false", false}, {"0", false}, {" true ", true}, {"1", true}, {"invalid", true}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", tc.value)
			if got := SquarespacePaymentsPaused(); got != tc.paused {
				t.Fatalf("paused=%v", got)
			}
			if (RequireSquarespacePaymentsActive() != nil) != tc.paused {
				t.Fatal("active gate mismatch")
			}
		})
	}
}
