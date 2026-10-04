package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func borrowResult(model, state string, total, correct int, at time.Time) GatewayBorrowModelResult {
	return GatewayBorrowModelResult{State: state, Model: model, Total: total, Correct: correct, CheckedAt: at, ReasoningEffort: "medium", ExpectedAnswer: "21", RunID: "20261004T190000Z-test", Algorithm: GatewayBorrowCandyAlgorithm, PromptSHA256: GatewayBorrowCandyPromptSHA256}
}
func borrowRecord(r GatewayBorrowModelResult) map[string]any {
	b, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	out["version"] = float64(1)
	return out
}
func TestGatewayBorrowPolicyEvidenceAndRetirement(t *testing.T) {
	now := time.Now().UTC()
	astra := borrowResult("gpt-6-astra", "degraded", 4, 0, now.Add(-time.Minute))
	extra := map[string]any{"quality_candy": borrowRecord(astra), "openai_excel_bps": true, ExcelBPSRequiredGroupIDsKey: []int64{16}, ExcelBPSRequiredModelsKey: []string{"gpt-6-astra"}, "openai_excel_bps_rate_limit_reset_at": "keep", "unrelated": true}
	o := GatewayBorrowPolicyObservation{ObservedAt: now.Add(-time.Second), CredentialSHA256: strings.Repeat("a", 64), BorrowModels: []string{"gpt-6-astra"}, RetireBPS: true, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "degraded", 4, 1, now)}}
	update, reason, err := BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "legacy_policy_migrated", reason)
	require.Equal(t, false, update["openai_excel_bps"])
	require.Equal(t, []int64{}, update[ExcelBPSRequiredGroupIDsKey])
	require.Equal(t, []string{"gpt-6-astra"}, update[GatewayBorrowModelsKey])
	require.NotContains(t, update, "openai_excel_bps_rate_limit_reset_at")
	require.NotContains(t, update, "unrelated")
	previous, ok := extra["quality_candy"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(0), previous["correct"], "input snapshot is immutable")
}
func TestGatewayBorrowSingleProbeCannotChangeClassOrRoute(t *testing.T) {
	now := time.Now().UTC()
	old := borrowResult("gpt-6-astra", "healthy", 4, 3, now.Add(-time.Minute))
	extra := map[string]any{"quality_candy": borrowRecord(old)}
	o := GatewayBorrowPolicyObservation{BorrowModels: []string{}, CredentialSHA256: strings.Repeat("a", 64), ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "healthy", 1, 1, now)}}
	updates, reason, err := BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Empty(t, reason)
	records, ok := updates[GatewayBorrowQualityKey].(map[string]any)
	require.True(t, ok)
	record, ok := records["gpt-6-astra"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(4), record["total"])
	require.Equal(t, float64(3), record["correct"])
	require.Equal(t, old.CheckedAt.Format(time.RFC3339Nano), record["checked_at"])
	require.Equal(t, now.Format(time.RFC3339Nano), record["latest_probe_at"])
	require.NotContains(t, updates, "quality_candy")
	o.BorrowModels = []string{"gpt-6-astra"}
	o.ModelResults["gpt-6-astra"] = borrowResult("gpt-6-astra", "degraded", 1, 0, now)
	_, reason, err = BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "single_probe_requires_matching_classification", reason)
}
func TestGatewayBorrowInconclusiveAndUntouchedModelsPreserveRoute(t *testing.T) {
	now := time.Now().UTC()
	extra := map[string]any{GatewayBorrowModelsKey: []string{"gpt-6-astra"}, GatewayBorrowQualityKey: map[string]any{"gpt-6-astra": borrowRecord(borrowResult("gpt-6-astra", "degraded", 4, 0, now.Add(-time.Minute)))}}
	o := GatewayBorrowPolicyObservation{BorrowModels: []string{"gpt-6-astra"}, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "inconclusive", 1, 0, now)}}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Empty(t, reason)
	records, ok := u[GatewayBorrowQualityKey].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, records["gpt-6-astra"], "latest_probe_at")
	o.BorrowModels = []string{}
	_, reason, err = BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "unproven_route_change", reason)
}
func TestGatewayBorrowRejectsStaleModelEvidenceAndForeignBPS(t *testing.T) {
	now := time.Now().UTC()
	r := borrowResult("gpt-6.1-sol", "healthy", 4, 4, now)
	extra := map[string]any{GatewayBorrowQualityKey: map[string]any{r.Model: borrowRecord(r)}}
	o := GatewayBorrowPolicyObservation{BorrowModels: []string{}, ModelResults: map[string]GatewayBorrowModelResult{r.Model: r}}
	_, reason, err := BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "stale_model_result", reason)
	o.ModelResults = nil
	o.RetireBPS = true
	_, reason, err = BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "bps_not_candy_owned", reason)
}

type borrowPolicyServiceRepo struct {
	AccountRepository
	calls int
}

func (r *borrowPolicyServiceRepo) UpdateGatewayBorrowPolicyIfObserved(context.Context, int64, GatewayBorrowPolicyObservation) (GatewayBorrowPolicyResult, error) {
	r.calls++
	return GatewayBorrowPolicyResult{Applied: true, Reason: "applied"}, nil
}
func TestGatewayBorrowObservationValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := func() GatewayBorrowPolicyObservation {
		return GatewayBorrowPolicyObservation{ObservedAt: now, CredentialSHA256: strings.Repeat("a", 64), ExpectedPolicy: map[string]any{}, BorrowModels: []string{"gpt-6-astra"}, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "degraded", 4, 0, now)}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*GatewayBorrowPolicyObservation)
		bad    bool
		skip   bool
	}{
		{"valid", func(*GatewayBorrowPolicyObservation) {}, false, false},
		{"stale", func(o *GatewayBorrowPolicyObservation) { o.ObservedAt = now.Add(-11 * time.Minute) }, false, true},
		{"future", func(o *GatewayBorrowPolicyObservation) { o.ObservedAt = now.Add(time.Minute) }, true, false},
		{"unknown owned field", func(o *GatewayBorrowPolicyObservation) { o.ExpectedPolicy["credentials"] = nil }, true, false},
		{"unknown borrowed model", func(o *GatewayBorrowPolicyObservation) { o.BorrowModels = []string{"alias"} }, true, false},
		{"duplicate", func(o *GatewayBorrowPolicyObservation) { o.BorrowModels = []string{"gpt-6-astra", "gpt-6-astra"} }, true, false},
		{"wrong classification", func(o *GatewayBorrowPolicyObservation) {
			r := o.ModelResults["gpt-6-astra"]
			r.Correct = 4
			o.ModelResults[r.Model] = r
		}, true, false},
		{"partial classified", func(o *GatewayBorrowPolicyObservation) {
			r := o.ModelResults["gpt-6-astra"]
			r.Total = 2
			o.ModelResults[r.Model] = r
		}, true, false},
		{"wrong prompt", func(o *GatewayBorrowPolicyObservation) {
			r := o.ModelResults["gpt-6-astra"]
			r.PromptSHA256 = strings.Repeat("b", 64)
			o.ModelResults[r.Model] = r
		}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := valid()
			tc.mutate(&o)
			repo := &borrowPolicyServiceRepo{}
			s := &RateLimitService{accountRepo: repo}
			result, err := s.UpdateGatewayBorrowPolicyFromProbe(context.Background(), 1, o)
			if tc.bad {
				require.Error(t, err)
				require.Zero(t, repo.calls)
			} else {
				require.NoError(t, err)
				if tc.skip {
					require.True(t, result.Skipped)
					require.Zero(t, repo.calls)
				} else {
					require.True(t, result.Applied)
					require.Equal(t, 1, repo.calls)
				}
			}
		})
	}
}
func TestGatewayBorrowCredentialHashMatchesPython(t *testing.T) {
	require.Equal(t, "a06728d0b614cc567ca67abac323a1a90888658055db784bc8cd88c56ecf6289", GatewayBorrowCredentialSHA256(map[string]any{"access_token": "token", "chatgpt_account_id": "account"}))
	require.Equal(t, "690f768133968a2c967845694d5c3b5896f973a47ec090951f3dba885f2381ed", GatewayBorrowCredentialSHA256(nil))
	require.Equal(t, "533a78cb59ebc473fb825e1e6852d21bc4750e4a3f2e6e2d156a5c92ee09b8b3", GatewayBorrowCredentialSHA256(map[string]any{"access_token": "tokén<>&", "chatgpt_account_id": "账户😀"}))
}

func TestGatewayBorrowMigrationWithoutFreshClassification(t *testing.T) {
	now := time.Now().UTC()
	extra := map[string]any{"quality_candy": borrowRecord(borrowResult("gpt-6-astra", "degraded", 4, 0, now.Add(-time.Hour))), "openai_excel_bps": true, ExcelBPSRequiredGroupIDsKey: []int64{16}, ExcelBPSRequiredModelsKey: []string{"gpt-6-sol", "gpt-6.1-sol", "gpt-6-astra"}}
	o := GatewayBorrowPolicyObservation{RetireBPS: true, BorrowModels: []string{"gpt-6-astra", "gpt-6.1-sol"}}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "legacy_policy_migrated", reason)
	require.Empty(t, u[GatewayBorrowQualityKey])
	require.NotContains(t, u, "quality_candy")
	extra["openai_excel_bps"] = false
	_, reason, err = BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "unproven_route_change", reason)
	extra["openai_excel_bps"] = true
	extra[GatewayBorrowModelsKey] = []string{}
	_, reason, err = BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "legacy_scope_conflicts_with_borrow_policy", reason)
	delete(extra, GatewayBorrowModelsKey)
	extra[ExcelBPSRequiredGroupIDsKey] = []int64{}
	_, reason, err = BuildGatewayBorrowPolicyUpdates(extra, o)
	require.NoError(t, err)
	require.Equal(t, "bps_not_candy_owned", reason)
}
