package service

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// Releasing the new independent performance monitor must not grant permission
// through the production quality or BPS gates on HTTP or subsequent WS turns.
func TestAccountPerformanceReleasePreservesRitelAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*Account)
		reason string
	}{
		{"initial_quality", func(a *Account) {
			a.Extra[GatewayBorrowAccountQualityModeKey] = GatewayBorrowAccountQualityMode
			a.Extra[GatewayBorrowAccountQualityPendingKey] = true
		}, "gateway_borrow_initial_quality_pending"},
		{"quality_isolation", func(a *Account) {
			a.Extra[QualityModelIsolationKey] = map[string]any{
				"gpt-6-astra": map[string]any{"reason": "quality_rule:7"},
				"gpt-6.1-sol": map[string]any{"reason": "quality_rule:7"},
			}
		}, "model_runtime_blocked"},
		{"expired_quality_cooldown", func(a *Account) {
			a.Extra[modelRateLimitsKey] = map[string]any{
				"gpt-6-astra": map[string]any{"reason": "quality_rule:7", "rate_limit_reset_at": time.Now().Add(-time.Hour).Format(time.RFC3339)},
				"gpt-6.1-sol": map[string]any{"reason": "quality_rule:7", "rate_limit_reset_at": time.Now().Add(-time.Hour).Format(time.RFC3339)},
			}
		}, "model_runtime_blocked"},
		{"bps_disabled", func(a *Account) {
			a.Credentials["client_id"] = openai.ExcelClientID
			a.Extra["openai_excel_bps"] = false
		}, excelOAuthRouteErrorCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := pendingAccountFixture()
			delete(a.Extra, GatewayBorrowAccountQualityPendingKey)
			tc.setup(a)
			ctx := context.Background()
			snapshot := &SchedulerSnapshotService{accountRepo: &performanceAccountRepoStub{paused: map[int64]bool{a.ID: false}}}
			accounts, err := snapshot.markPerformanceAccounts(ctx, map[int64]*Account{a.ID: a})
			require.NoError(t, err)
			released := accounts[a.ID]
			require.False(t, released.PerformancePaused)
			require.True(t, released.IsSchedulable(), "performance release is only the independent lifecycle gate")
			svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: released}}
			for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
				_, err := svc.admitOpenAITurnForGroup(ctx, 0, false, released, model)
				var denial *OpenAITurnAdmissionError
				require.ErrorAs(t, err, &denial)
				require.Equal(t, tc.reason, denial.Reason)
				req, err := http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"`+model+`"}`))
				require.NoError(t, err)
				_, err = svc.admitOpenAIHTTPRequest(req, released)
				require.ErrorAs(t, err, &denial)
				require.Equal(t, tc.reason, denial.Reason)
			}
		})
	}
}

// A selected API-key/BPS connection differs from the parent's native Codex
// projection. Final HTTP admission must neither restore the primary token nor
// stop enforcing live account-level restrictions.
func TestManagedConnectionHTTPAdmissionPreservesSelectionAndParentGates(t *testing.T) {
	for _, change := range []string{"none", "performance_pause", "quality_pending", "quality_isolation"} {
		t.Run(change, func(t *testing.T) {
			parent := pendingAccountFixture()
			delete(parent.Extra, GatewayBorrowAccountQualityPendingKey)
			parent.Extra["openai_connections_managed"] = true
			selected := *parent
			selected.Type = AccountTypeAPIKey
			selected.AuthConnectionID = "synthetic-managed-connection"
			selected.AuthConnectionRevision = 7
			selected.Credentials = map[string]any{"api_key": "synthetic-connection-key", "base_url": "https://api.openai.com"}
			selected.Extra = maps.Clone(parent.Extra)
			selected.Extra["openai_responses_supported"] = true
			switch change {
			case "performance_pause":
				parent.PerformancePaused = true
			case "quality_pending":
				parent.Extra[GatewayBorrowAccountQualityPendingKey] = true
			case "quality_isolation":
				parent.Extra[QualityModelIsolationKey] = map[string]any{"gpt-6-astra": map[string]any{"reason": "quality_rule:7"}}
			}
			svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: parent}}
			req, err := http.NewRequest("POST", "https://api.openai.com/v1/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
			require.NoError(t, err)
			got, err := svc.admitOpenAIHTTPRequest(req, &selected)
			if change == "none" {
				require.NoError(t, err)
				require.Same(t, &selected, got)
				require.Equal(t, "synthetic-connection-key", got.GetCredential("api_key"))
				require.Equal(t, selected.AuthConnectionID, got.AuthConnectionID)
			} else {
				require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
				require.Nil(t, got)
			}
		})
	}
}
