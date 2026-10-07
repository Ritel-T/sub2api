package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func accountQualityFixture(now time.Time, state string) (*Account, map[string]any) {
	correct := 1
	if state == "healthy" {
		correct = 3
	}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "quality-test-token", "chatgpt_account_id": "quality-test-account"}}
	r := borrowRecord(borrowResult("gpt-6-astra", state, 4, correct, now.Add(-48*time.Hour)))
	r["latest_probe_at"] = now.Add(-time.Minute).Format(time.RFC3339Nano)
	r["latest_probe_credential_sha256"] = GatewayBorrowCredentialSHA256(a.Credentials)
	r["latest_probe_proxy_id"] = nil
	a.Extra = map[string]any{GatewayBorrowAccountQualityModeKey: GatewayBorrowAccountQualityMode, "quality_candy": r}
	return a, r
}

func TestGatewayBorrowAccountClassificationFreshnessAndIdentity(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		edit func(*Account, map[string]any)
		ok   bool
	}{
		{"matching refresh of old confirmed round", func(*Account, map[string]any) {}, true},
		{"new identity", func(a *Account, _ map[string]any) { a.Credentials["access_token"] = "changed" }, false},
		{"new proxy", func(a *Account, _ map[string]any) { id := int64(7); a.ProxyID = &id }, false},
		{"expired latest refresh", func(_ *Account, r map[string]any) {
			r["latest_probe_at"] = now.Add(-25 * time.Hour).Format(time.RFC3339Nano)
		}, false},
		{"pending", func(a *Account, _ map[string]any) { a.Extra[GatewayBorrowAccountQualityPendingKey] = true }, false},
		{"Sol evidence cannot qualify", func(_ *Account, r map[string]any) { r["model"] = "gpt-6.1-sol" }, false},
		{"wrong effort", func(_ *Account, r map[string]any) { r["reasoning_effort"] = "high" }, false},
		{"partial round", func(_ *Account, r map[string]any) { r["total"] = 1 }, false},
		{"future latest", func(_ *Account, r map[string]any) {
			r["latest_probe_at"] = now.Add(time.Minute).Format(time.RFC3339Nano)
		}, false},
		{"unbound nil proxy", func(_ *Account, r map[string]any) { delete(r, "latest_probe_proxy_id") }, false},
		{"sanitized scheduler projection", func(a *Account, _ map[string]any) {
			a.SchedulerCredentialSHA256 = GatewayBorrowCredentialSHA256(a.Credentials)
			a.Credentials = map[string]any{}
		}, true},
		{"full identity ignores stale cache hash", func(a *Account, _ map[string]any) { a.SchedulerCredentialSHA256 = strings.Repeat("f", 64) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, r := accountQualityFixture(now, "healthy")
			tc.edit(a, r)
			quality, ok := GatewayBorrowAccountQuality(a, now, 24*time.Hour)
			require.Equal(t, tc.ok, ok)
			if ok {
				require.Equal(t, "healthy", quality.State)
				require.Equal(t, 4, quality.Total)
				require.Equal(t, now.Add(-48*time.Hour), quality.CheckedAt)
			}
		})
	}
}

func TestGatewayBorrowAccountQualityControlsBothModelsWithoutInventingSol(t *testing.T) {
	now := time.Now().UTC()
	for _, state := range []string{"healthy", "degraded"} {
		t.Run(state, func(t *testing.T) {
			a, _ := accountQualityFixture(now, "degraded")
			sol := borrowRecord(borrowResult("gpt-6.1-sol", "degraded", 4, 0, now.Add(-2*time.Hour)))
			a.Extra[GatewayBorrowQualityKey] = map[string]any{"gpt-6.1-sol": sol}
			a.Extra[GatewayBorrowAccountQualityPendingKey] = true
			a.Extra[GatewayBorrowInitialReadyKey] = true
			a.Extra["unrelated"] = "keep"
			correct, models := 3, []string{}
			if state == "degraded" {
				correct, models = 2, []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}
			}
			o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, CredentialSHA256: GatewayBorrowCredentialSHA256(a.Credentials), BorrowModels: models, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", state, 4, correct, now)}}
			u, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
			require.NoError(t, err)
			require.Equal(t, "account_quality_applied", reason)
			require.Equal(t, models, u[GatewayBorrowModelsKey])
			require.Equal(t, false, u[GatewayBorrowAccountQualityPendingKey])
			require.NotContains(t, u, "unrelated")
			history, historyOK := u[GatewayBorrowQualityKey].(map[string]any)
			require.True(t, historyOK)
			require.Equal(t, sol, history["gpt-6.1-sol"], "preserve actual historical Sol evidence")
			classification, classificationOK := u["quality_candy"].(map[string]any)
			require.True(t, classificationOK)
			require.Equal(t, float64(4), classification["total"])
			for k, v := range u {
				a.Extra[k] = v
			}
			q, ok := GatewayBorrowAccountQuality(a, now, 24*time.Hour)
			require.True(t, ok)
			require.Equal(t, state, q.State, "latest completed confirmation wins over old degraded rounds")
		})
	}
}

func TestGatewayBorrowAccountQualityMatchingSingleOnlyRefreshesEvidence(t *testing.T) {
	now := time.Now().UTC()
	a, old := accountQualityFixture(now, "healthy")
	o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, CredentialSHA256: GatewayBorrowCredentialSHA256(a.Credentials), BorrowModels: []string{}, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "healthy", 1, 1, now)}}
	u, _, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	r := u["quality_candy"].(map[string]any)
	require.Equal(t, old["checked_at"], r["checked_at"])
	require.Equal(t, old["correct"], r["correct"])
	require.Equal(t, float64(4), r["total"])
	require.Equal(t, now.Format(time.RFC3339Nano), r["latest_probe_at"])
	require.Equal(t, now.Add(-time.Minute).Format(time.RFC3339Nano), old["latest_probe_at"], "input snapshot remains unchanged")
	o.ModelResults["gpt-6-astra"] = borrowResult("gpt-6-astra", "degraded", 1, 0, now)
	o.BorrowModels = []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.Nil(t, u)
	require.Equal(t, "single_probe_requires_matching_classification", reason)
}

func TestGatewayBorrowAccountQualityMigrationReusesBoundCompleteAstraOnly(t *testing.T) {
	now := time.Now().UTC()
	a, r := accountQualityFixture(now, "healthy")
	delete(a.Extra, GatewayBorrowAccountQualityModeKey)
	delete(a.Extra, "quality_candy")
	a.Extra[GatewayBorrowQualityKey] = map[string]any{"gpt-6-astra": r}
	o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, CredentialSHA256: GatewayBorrowCredentialSHA256(a.Credentials), BorrowModels: []string{}, ModelResults: map[string]GatewayBorrowModelResult{}}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.Equal(t, "account_quality_applied", reason)
	require.Equal(t, r, u["quality_candy"], "mode migration creates no new probe or fresh timestamp")
	r["latest_probe_credential_sha256"] = strings.Repeat("f", 64)
	u, reason, err = BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.Nil(t, u)
	require.Equal(t, "account_classification_required", reason)
}

func TestGatewayBorrowAccountQualityDoesNotDowngradeModeOrOpenUnreadyAccount(t *testing.T) {
	now := time.Now().UTC()
	a, _ := accountQualityFixture(now, "healthy")
	_, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, GatewayBorrowPolicyObservation{})
	require.NoError(t, err)
	require.Equal(t, "policy_mode_changed", reason)
	a.Extra[GatewayBorrowAccountQualityPendingKey] = true
	o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, BorrowModels: []string{}, CredentialSHA256: GatewayBorrowCredentialSHA256(a.Credentials), ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "healthy", 4, 4, now)}}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.Nil(t, u)
	require.Equal(t, "initial_defaults_not_ready", reason)
}

func TestGatewayBorrowAccountInconclusiveCannotChangePolicy(t *testing.T) {
	now := time.Now().UTC()
	a, r := accountQualityFixture(now, "healthy")
	o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, CredentialSHA256: GatewayBorrowCredentialSHA256(a.Credentials), BorrowModels: []string{}, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "inconclusive", 4, 2, now)}}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.Nil(t, u)
	require.Equal(t, "inconclusive_account_quality", reason)
	require.Equal(t, now.Add(-time.Minute).Format(time.RFC3339Nano), r["latest_probe_at"])
}

func TestGatewayBorrowAccountQualityRetiresOnlyOwnedLegacyBPS(t *testing.T) {
	now := time.Now().UTC()
	a, _ := accountQualityFixture(now, "degraded")
	delete(a.Extra, GatewayBorrowAccountQualityModeKey)
	a.Extra["openai_excel_bps"] = true
	a.Extra[ExcelBPSRequiredGroupIDsKey] = []int64{16}
	a.Extra[ExcelBPSRequiredModelsKey] = []string{"gpt-6-astra", "gpt-6-sol", "gpt-6.1-sol"}
	o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, CredentialSHA256: GatewayBorrowCredentialSHA256(a.Credentials), BorrowModels: []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}, RetireBPS: true}
	u, _, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.NotNil(t, u)
	require.Equal(t, false, u["openai_excel_bps"])
	require.Equal(t, []int64{}, u[ExcelBPSRequiredGroupIDsKey])
	require.Equal(t, []string{}, u[ExcelBPSRequiredModelsKey])
	a.Extra[ExcelBPSRequiredGroupIDsKey] = []int64{}
	u, reason, err := BuildGatewayBorrowPolicyUpdates(a.Extra, o)
	require.NoError(t, err)
	require.Nil(t, u)
	require.Equal(t, "bps_not_candy_owned", reason)
}

func TestGatewayBorrowAccountObservationValidation(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		edit func(*GatewayBorrowPolicyObservation)
		bad  bool
	}{
		{"Astra classification", func(*GatewayBorrowPolicyObservation) {}, false},
		{"stored classification migration", func(o *GatewayBorrowPolicyObservation) { o.ModelResults = map[string]GatewayBorrowModelResult{} }, false},
		{"unknown mode", func(o *GatewayBorrowPolicyObservation) { o.PolicyMode = "unknown" }, true},
		{"Sol probe", func(o *GatewayBorrowPolicyObservation) {
			o.ModelResults = map[string]GatewayBorrowModelResult{"gpt-6.1-sol": borrowResult("gpt-6.1-sol", "healthy", 4, 4, now)}
		}, true},
		{"nonstandard effort", func(o *GatewayBorrowPolicyObservation) {
			r := o.ModelResults["gpt-6-astra"]
			r.ReasoningEffort = "high"
			o.ModelResults[r.Model] = r
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := GatewayBorrowPolicyObservation{PolicyMode: GatewayBorrowAccountQualityMode, ObservedAt: now, CredentialSHA256: strings.Repeat("a", 64), ExpectedPolicy: map[string]any{}, BorrowModels: []string{}, ModelResults: map[string]GatewayBorrowModelResult{"gpt-6-astra": borrowResult("gpt-6-astra", "healthy", 4, 4, now)}}
			tc.edit(&o)
			repo := &borrowPolicyServiceRepo{}
			_, err := (&RateLimitService{accountRepo: repo}).UpdateGatewayBorrowPolicyFromProbe(context.Background(), 1, o)
			if tc.bad {
				require.Error(t, err)
				require.Zero(t, repo.calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, repo.calls)
			}
		})
	}
}
