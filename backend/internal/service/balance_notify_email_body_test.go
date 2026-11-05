//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests guard against fmt.Sprintf arg-count mismatches in the email
// templates. A mismatch would produce "%!(EXTRA ...)" or "%!v(MISSING)" in
// the output, which these assertions will catch.

// ---------- buildBalanceLowEmailBody ----------

func TestBuildBalanceLowEmailBody_ContainsRequiredFields(t *testing.T) {
	s := &BalanceNotifyService{}
	body := s.buildBalanceLowEmailBody("Alice", 3.14, 10.0, "MySite", "")

	// All substituted values should appear in the output.
	require.Contains(t, body, "MySite")
	require.Contains(t, body, "Alice")
	require.Contains(t, body, "$3.14")
	require.Contains(t, body, "$10.00")

	// No fmt.Sprintf format error markers.
	require.NotContains(t, body, "%!")
	require.NotContains(t, body, "MISSING")
	require.NotContains(t, body, "EXTRA")
}

func TestBuildBalanceLowEmailBody_WithRechargeURL(t *testing.T) {
	s := &BalanceNotifyService{}
	body := s.buildBalanceLowEmailBody("Bob", 5.0, 20.0, "Site", "https://example.com/pay")

	// The recharge anchor element should appear with the URL.
	require.Contains(t, body, `href="https://example.com/pay"`)
	require.Contains(t, body, "立即充值")
	require.NotContains(t, body, "%!")
}

func TestBuildBalanceLowEmailBody_RechargeURLEscaped(t *testing.T) {
	s := &BalanceNotifyService{}
	// Try a URL with characters that need HTML escaping.
	body := s.buildBalanceLowEmailBody("u", 1.0, 5.0, "Site", `https://example.com/?a=1&b=<script>`)

	// `&` and `<` should be escaped in the href.
	require.Contains(t, body, "&amp;")
	require.Contains(t, body, "&lt;script&gt;")
	require.NotContains(t, body, "<script>")
}

func TestBuildBalanceLowEmailBody_NoRechargeURLOmitsButton(t *testing.T) {
	s := &BalanceNotifyService{}
	body := s.buildBalanceLowEmailBody("u", 1.0, 5.0, "Site", "")
	// The anchor element should not be rendered (style class may still appear).
	require.NotContains(t, body, `<a href`)
	require.NotContains(t, body, "立即充值")
}

// ---------- buildQuotaAlertEmailBody ----------

func TestBuildQuotaAlertEmailBody_AllFieldsPresent(t *testing.T) {
	s := &BalanceNotifyService{}
	body := s.buildQuotaAlertEmailBody(
		42,            // accountID
		"acc-foo",     // accountName
		"anthropic",   // platform
		"日限额 / Daily", // dimLabel
		750.50,        // used
		1000.0,        // limit
		249.50,        // remaining
		"$249.50",     // thresholdDisplay
		"MySite",      // siteName
	)

	require.Contains(t, body, "MySite")
	require.Contains(t, body, "#42")
	require.Contains(t, body, "acc-foo")
	require.Contains(t, body, "anthropic")
	require.Contains(t, body, "Daily")
	require.Contains(t, body, "$750.50")
	require.Contains(t, body, "$1000.00")
	require.Contains(t, body, "$249.50")

	// No format error markers.
	require.NotContains(t, body, "%!")
	require.NotContains(t, body, "MISSING")
	require.NotContains(t, body, "EXTRA")
}
