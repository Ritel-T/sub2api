package service

import (
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func balanceRecoveryTestSnapshot(now time.Time) *AccountBalanceRecoverySnapshot {
	return &AccountBalanceRecoverySnapshot{ID: 194, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusError, Schedulable: false, ErrorMessage: "Payment required (402): insufficient balance", UpdatedAt: now, Credentials: map[string]any{"api_key": "private-test-value"}, Extra: map[string]any{"quality_state": "keep"}}
}
func balanceRecoveryTestRequest(t *testing.T, s *AccountBalanceRecoverySnapshot, now time.Time) AccountBalanceRecoveryRequest {
	t.Helper()
	p, err := BuildAccountBalanceRecoveryPreview(s, now)
	require.NoError(t, err)
	target := false
	return AccountBalanceRecoveryRequest{ExpectedUpdatedAt: p.ExpectedUpdatedAt, ExpectedFingerprint: p.ExpectedFingerprint, TargetSchedulable: &target, FundingConfirmed: true, Confirm: true}
}
func TestBalanceRecoveryRequiresExplicitNativeBalanceError(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"Payment required (402): insufficient balance", true},
		{"Credit balance exhausted (400): Your credit balance is too low", true},
		{"Payment required (402): insufficient account balance", true},
		{"Payment required (402): insufficient balance or billing issue", false},
		{"Payment required (402): subscription expired", false},
		{"Authentication failed (401): credit balance", false},
		{"insufficient balance", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			s := balanceRecoveryTestSnapshot(now)
			s.ErrorMessage = tc.message
			p, err := BuildAccountBalanceRecoveryPreview(s, now)
			require.NoError(t, err)
			require.Equal(t, tc.want, p.Eligible)
		})
	}
	for _, change := range []func(*AccountBalanceRecoverySnapshot){func(s *AccountBalanceRecoverySnapshot) { s.Platform = PlatformOpenAI }, func(s *AccountBalanceRecoverySnapshot) { s.Type = AccountTypeOAuth }, func(s *AccountBalanceRecoverySnapshot) { s.Status = StatusActive }, func(s *AccountBalanceRecoverySnapshot) { id := int64(1); s.ParentAccountID = &id }, func(s *AccountBalanceRecoverySnapshot) { s.PendingManualTimer = true }} {
		s := balanceRecoveryTestSnapshot(now)
		change(s)
		p, err := BuildAccountBalanceRecoveryPreview(s, now)
		require.NoError(t, err)
		require.False(t, p.Eligible)
	}
}
func TestBalanceRecoverySnapshotEvidenceAndExplicitFunding(t *testing.T) {
	now := time.Now().UTC()
	received := now.Add(-time.Minute)
	fresh := now.Add(time.Hour)
	stale := now.Add(-time.Second)
	for _, tc := range []struct {
		name, state                  string
		balance                      float64
		until                        *time.Time
		disabled, confirmed, allowed bool
	}{
		{"positive", "fresh_positive", 5, &fresh, false, false, true},
		{"zero", "fresh_insufficient", 0, &fresh, false, true, false},
		{"negative", "fresh_insufficient", -1, &fresh, false, true, false},
		{"stale without confirmation", "stale", 5, &stale, false, false, false},
		{"stale with confirmation", "stale", 5, &stale, false, true, true},
		{"disabled without confirmation", "unknown", 5, &fresh, true, false, false},
		{"disabled with confirmation", "unknown", 5, &fresh, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := balanceRecoveryTestSnapshot(now)
			s.Extra[UpstreamBillingProbeExtraKey] = &UpstreamBillingProbeSnapshot{Status: "ok", Balance: &UpstreamBalanceSnapshot{Status: "ok", ReceivedAt: &received, FreshUntil: tc.until, Data: map[string]any{"remaining": tc.balance, "unit": "USD"}}}
			if tc.disabled {
				s.Extra[UpstreamBillingProbeEnabledExtraKey] = false
			}
			p, err := BuildAccountBalanceRecoveryPreview(s, now)
			require.NoError(t, err)
			require.Equal(t, tc.state, p.BalanceEvidence.State)
			if tc.state == "stale" || tc.state == "unknown" {
				require.Nil(t, p.BalanceEvidence.Remaining)
			}
			req := balanceRecoveryTestRequest(t, s, now)
			req.FundingConfirmed = tc.confirmed
			_, err = CheckAccountBalanceRecoveryObservation(s, req, now)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
func TestBalanceRecoveryRejectsChangedIdentityErrorExtraAndVersion(t *testing.T) {
	now := time.Now().UTC()
	for _, change := range []func(*AccountBalanceRecoverySnapshot){func(s *AccountBalanceRecoverySnapshot) { s.Credentials["api_key"] = "rotated" }, func(s *AccountBalanceRecoverySnapshot) { s.ErrorMessage = "Authentication failed (401)" }, func(s *AccountBalanceRecoverySnapshot) { s.Extra["quality_state"] = "changed" }, func(s *AccountBalanceRecoverySnapshot) { s.UpdatedAt = s.UpdatedAt.Add(time.Microsecond) }, func(s *AccountBalanceRecoverySnapshot) { s.Schedulable = true }, func(s *AccountBalanceRecoverySnapshot) { s.PendingManualTimer = true }} {
		s := balanceRecoveryTestSnapshot(now)
		req := balanceRecoveryTestRequest(t, s, now)
		change(s)
		_, err := CheckAccountBalanceRecoveryObservation(s, req, now)
		require.Equal(t, "BALANCE_RECOVERY_CHANGED", infraerrors.Reason(err))
	}
	s := balanceRecoveryTestSnapshot(now)
	req := balanceRecoveryTestRequest(t, s, now)
	req.TargetSchedulable = nil
	require.Error(t, ValidateAccountBalanceRecoveryRequest(req))
	req = balanceRecoveryTestRequest(t, s, now)
	req.Confirm = false
	require.Error(t, ValidateAccountBalanceRecoveryRequest(req))
	p, err := BuildAccountBalanceRecoveryPreview(s, now)
	require.NoError(t, err)
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-test-value")
	require.NotContains(t, string(raw), "insufficient balance")
	require.False(t, p.Schedulable)
}
