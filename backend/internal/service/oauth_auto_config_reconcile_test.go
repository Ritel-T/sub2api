package service

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func reconcileModelConfig() *OpenAIAccountDefaultsConfig {
	return &OpenAIAccountDefaultsConfig{
		Enabled: true,
		Templates: map[string]OAuthAccountTemplate{"prolite": {
			Concurrency: 7, Priority: 4,
			ModelMappings: []OAuthModelMappingRule{
				{From: "gpt-6-astra", To: "gpt-6-astra"},
				{From: "gpt-6-sol", To: "gpt-6.1-sol"},
				{From: "gpt-6.1-sol", To: "gpt-6.1-sol"},
				{From: "gpt-6-luna", To: "gpt-6-luna"},
			},
		}},
		ModelExclusions: map[string][]string{"34": {"gpt-6-astra", "gpt-6-sol", "gpt-6.1-sol", "gpt-6-luna"}},
	}
}

func reconcileModelAccount(extra map[string]any) *Account {
	return &Account{ID: 34, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"plan_type": "prolite", "access_token": "keep", "model_mapping": map[string]any{"old": "old"}},
		Extra:       extra, Concurrency: 91, Priority: 31, Schedulable: false}
}

func TestOAuthAccountModelMappingPreservesQualityPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  map[string]string
	}{
		{"static", nil, map[string]string{}},
		{"healthy candy keeps Luna exclusion", map[string]any{"quality_candy": map[string]any{"version": float64(1), "state": "healthy"}}, map[string]string{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6.1-sol"}},
		{"degraded candy still restores supported models", map[string]any{"quality_candy": map[string]any{"version": 1, "state": "degraded"}, "quality_model_results": map[string]any{"gpt-6-sol": "degraded"}}, map[string]string{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6.1-sol"}},
		{"selected BPS model", map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []any{"gpt-6-sol"}}, map[string]string{"gpt-6-sol": "gpt-6.1-sol"}},
		{"BPS missing scope means all", map[string]any{"openai_excel_bps": true}, map[string]string{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6.1-sol"}},
		{"BPS explicit empty stays empty", map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []any{}}, map[string]string{}},
		{"good result restores one", map[string]any{"quality_model_results": map[string]any{"gpt-6-astra": "good"}}, map[string]string{"gpt-6-astra": "gpt-6-astra"}},
		{"string version is not classified", map[string]any{"quality_candy": map[string]any{"version": "1", "state": "healthy"}}, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := reconcileModelAccount(tt.extra)
			got, ok := OAuthAccountModelMapping(reconcileModelConfig(), account)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
			require.Equal(t, map[string]any{"old": "old"}, account.Credentials["model_mapping"])
			require.Equal(t, "keep", account.Credentials["access_token"])
			require.Equal(t, 91, account.Concurrency)
			require.Equal(t, 31, account.Priority)
			require.False(t, account.Schedulable)
		})
	}
	config := reconcileModelConfig()
	config.ModelExclusions = nil
	account := reconcileModelAccount(map[string]any{"quality_model_results": map[string]any{"gpt-6-astra": "degraded"}})
	got, ok := OAuthAccountModelMapping(config, account)
	require.True(t, ok)
	require.NotContains(t, got, "gpt-6-astra")
	require.Contains(t, got, "gpt-6-luna")
}

func TestOAuthAccountModelReconcileScopeAndInitialGate(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*Account)
		want   bool
	}{
		{"ordinary", func(*Account) {}, true},
		{"API key", func(a *Account) { a.Type = AccountTypeAPIKey }, false},
		{"other platform", func(a *Account) { a.Platform = PlatformGrok }, false},
		{"shadow", func(a *Account) { id := int64(10); a.ParentAccountID = &id }, false},
		{"AgentIdentity", func(a *Account) { a.Credentials[openAIAuthModeCredentialKey] = OpenAIAuthModeAgentIdentity }, false},
		{"PAT", func(a *Account) { a.Credentials[openAIAuthModeCredentialKey] = OpenAIAuthModePersonalAccessToken }, false},
		{"pending not ready", func(a *Account) {
			a.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra"}
			a.Extra = map[string]any{GatewayBorrowAccountQualityModeKey: GatewayBorrowAccountQualityMode, GatewayBorrowAccountQualityPendingKey: true, GatewayBorrowInitialReadyKey: false}
		}, false},
		{"pending ready", func(a *Account) {
			a.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra"}
			a.Extra = map[string]any{GatewayBorrowAccountQualityModeKey: GatewayBorrowAccountQualityMode, GatewayBorrowAccountQualityPendingKey: true, GatewayBorrowInitialReadyKey: true}
		}, false},
		{"no Astra awaiting no probe", func(a *Account) {
			a.Extra = map[string]any{GatewayBorrowAccountQualityModeKey: GatewayBorrowAccountQualityMode, GatewayBorrowAccountQualityPendingKey: true, GatewayBorrowInitialReadyKey: false}
		}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := reconcileModelAccount(nil)
			tt.mutate(a)
			_, ok := OAuthAccountModelMapping(reconcileModelConfig(), a)
			require.Equal(t, tt.want, ok)
		})
	}
	c := reconcileModelConfig()
	c.Enabled = false
	_, ok := OAuthAccountModelMapping(c, reconcileModelAccount(nil))
	require.False(t, ok)
	a := reconcileModelAccount(nil)
	a.Credentials["plan_type"] = "future_plan"
	_, ok = OAuthAccountModelMapping(reconcileModelConfig(), a)
	require.False(t, ok)
}

type modelReconcileAccountsStub struct {
	AccountRepository
	rows       []Account
	calls      []*Account
	mappings   []map[string]string
	readyCalls int
	t          *testing.T
}

func (r *modelReconcileAccountsStub) ListAllWithFilters(_ context.Context, platform, accountType, status, search string, groupID int64, privacy string) ([]Account, error) {
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth || status != "" || search != "" || groupID != 0 || privacy != "" {
		panic("unexpected model reconciliation filters")
	}
	rows := make([]Account, len(r.rows))
	copy(rows, r.rows)
	for i := range rows {
		rows[i].Extra = maps.Clone(rows[i].Extra)
		rows[i].Credentials = maps.Clone(rows[i].Credentials)
	}
	return rows, nil
}
func (r *modelReconcileAccountsStub) ReconcileOAuthAccountModels(_ context.Context, a *Account, mapping map[string]string) (bool, error) {
	r.calls = append(r.calls, a)
	r.mappings = append(r.mappings, mapping)
	return true, nil
}
func TestNativeModelReconcileOnlyWritesDrift(t *testing.T) {
	config := reconcileModelConfig()
	config.ModelExclusions = nil
	first := reconcileModelAccount(map[string]any{"openai_account_defaults_revision": "native"})
	second := reconcileModelAccount(map[string]any{"openai_account_defaults_revision": "native"})
	second.ID = 35
	desired, _ := OAuthAccountModelMapping(config, second)
	second.Credentials["model_mapping"] = desired
	key := reconcileModelAccount(nil)
	key.Type = AccountTypeAPIKey
	unmanaged := reconcileModelAccount(nil)
	unmanaged.ID = 36
	legacy := reconcileModelAccount(nil)
	legacy.ID = 37
	config.LegacyAccountIDs = []int64{37}
	repo := &modelReconcileAccountsStub{rows: []Account{*first, *second, *key, *unmanaged, *legacy}}
	svc := NewAccountOpsService(nil, nil, nil)
	svc.accounts = repo
	c := DefaultOAuthAutoConfig()
	c.Enabled = true
	c.OpenAIAccountDefaults = config
	svc.autoConfig.Store(c)
	svc.reconcileOAuthAccountModels(context.Background())
	require.Len(t, repo.calls, 2)
	require.Equal(t, int64(34), repo.calls[0].ID)
	require.Equal(t, int64(37), repo.calls[1].ID)
	require.Equal(t, desired, repo.mappings[0])
	require.Equal(t, "keep", repo.calls[0].Credentials["access_token"])
}

func TestOAuthAccountModelMappingNormalizesAcceptedNames(t *testing.T) {
	config := reconcileModelConfig()
	config.ModelExclusions = nil
	template := config.Templates["prolite"]
	template.ModelMappings = []OAuthModelMappingRule{{From: " gpt-6-sol ", To: " gpt-6.1-sol "}}
	config.Templates["prolite"] = template
	got, applicable := OAuthAccountModelMapping(config, reconcileModelAccount(nil))
	require.True(t, applicable)
	require.Equal(t, map[string]string{"gpt-6-sol": "gpt-6.1-sol"}, got)
}

func (r *modelReconcileAccountsStub) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.rows {
		if r.rows[i].ID == id {
			a := r.rows[i]
			a.Extra = maps.Clone(a.Extra)
			a.Credentials = maps.Clone(a.Credentials)
			return &a, nil
		}
	}
	return nil, ErrAccountNotFound
}
func (r *modelReconcileAccountsStub) MarkGatewayBorrowInitialReadyIfObserved(_ context.Context, id int64, o GatewayBorrowInitialReadyObservation) (GatewayBorrowPolicyResult, error) {
	if r.t == nil {
		panic("unexpected readiness outside an explicit readiness test")
	}
	for i := range r.rows {
		a := &r.rows[i]
		if a.ID != id {
			continue
		}
		require.Equal(r.t, GatewayBorrowCredentialSHA256(a.Credentials), o.CredentialSHA256)
		require.Equal(r.t, GatewayBorrowInitialConfiguration(a), o.ExpectedConfig)
		require.True(r.t, nativeOAuthAccountDefaultsIdentityMatches(a))
		a.Extra[GatewayBorrowInitialReadyKey] = true
		a.Extra[GatewayBorrowInitialReadyFingerprintKey] = GatewayBorrowInitialReadyFingerprint(a)
		r.readyCalls++
		return GatewayBorrowPolicyResult{Applied: true}, nil
	}
	return GatewayBorrowPolicyResult{Skipped: true}, nil
}
func TestNativeModelWorkerCoordinatesOnlyProvenTokenRefresh(t *testing.T) {
	for _, tt := range []struct {
		name      string
		mutate    func(*Account)
		wantReady bool
	}{
		{"same owner refresh", func(*Account) {}, true},
		{"proxy edit", func(a *Account) { id := int64(9); a.ProxyID = &id }, false},
		{"capacity edit", func(a *Account) { a.Concurrency++ }, false},
		{"group edit", func(a *Account) { a.GroupIDs = []int64{9} }, false},
		{"workspace edit", func(a *Account) { a.Credentials["chatgpt_account_id"] = "different" }, false},
		{"paused", func(a *Account) { a.Schedulable = false }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := reconcileModelAccount(map[string]any{
				"openai_account_defaults_revision":    "native",
				GatewayBorrowAccountQualityModeKey:    GatewayBorrowAccountQualityMode,
				GatewayBorrowAccountQualityPendingKey: true,
				GatewayBorrowInitialReadyKey:          true,
			})
			a.Status = StatusActive
			a.Schedulable = true
			a.GroupIDs = []int64{2}
			a.Credentials["chatgpt_account_id"] = "same-owner"
			a.Credentials["chatgpt_user_id"] = "same-user"
			a.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra"}
			a.Extra[nativeOAuthAccountDefaultsIdentityKey] = nativeOAuthAccountDefaultsIdentityFingerprint(a)
			a.Extra[GatewayBorrowInitialReadyFingerprintKey] = GatewayBorrowInitialReadyFingerprint(a)
			a.Credentials["access_token"] = "refreshed"
			tt.mutate(a)
			repo := &modelReconcileAccountsStub{t: t, rows: []Account{*a}}
			svc := NewAccountOpsService(nil, nil, nil)
			svc.accounts = repo
			c := DefaultOAuthAutoConfig()
			c.Enabled = true
			c.OpenAIAccountDefaults = reconcileModelConfig()
			svc.autoConfig.Store(c)
			svc.reconcileOAuthAccountModels(context.Background())
			if tt.wantReady {
				require.Equal(t, 1, repo.readyCalls)
				require.True(t, GatewayBorrowInitialReadyMatchesCurrent(&repo.rows[0]))
			} else {
				require.Zero(t, repo.readyCalls)
				require.False(t, GatewayBorrowInitialReadyMatchesCurrent(&repo.rows[0]))
			}
			require.Empty(t, repo.calls, "initial classification owns model admission until it completes")
		})
	}
}
