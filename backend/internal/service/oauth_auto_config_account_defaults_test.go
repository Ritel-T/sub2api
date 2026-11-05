package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/stretchr/testify/require"
)

func nativeAccountDefaultsFixture() OAuthAutoConfig {
	lf := 20
	c := DefaultOAuthAutoConfig()
	c.Enabled, c.GroupIDs, c.Revision = true, []int64{2}, "native-defaults-test"
	c.OpenAIAccountDefaults = &OpenAIAccountDefaultsConfig{
		Enabled: true,
		Templates: map[string]OAuthAccountTemplate{
			"business": {Concurrency: 20, LoadFactor: &lf, Priority: 2,
				ModelMappings: []OAuthModelMappingRule{{From: "gpt-6-astra", To: "gpt-6-astra"}, {From: "gpt-6-sol", To: "gpt-6.1-sol"}}},
			"free": {Concurrency: 2, Priority: 0, ModelMappings: []OAuthModelMappingRule{{From: "gpt-6-luna", To: "gpt-6-luna"}}},
		},
	}
	return c
}

func nativeAccountDefaultsService(t *testing.T, c OAuthAutoConfig) *adminServiceImpl {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	return &adminServiceImpl{
		settingService: NewSettingService(&accountOpsSettingsStub{raw: string(raw)}, nil),
		groupRepo:      autoConfigGroups{group: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive}},
	}
}

func TestOpenAIAccountDefaultsBusinessAliasesAndExactMapping(t *testing.T) {
	for _, plan := range []string{"business", "team", "self_serve_business_usage_based", "self_serve_business_prolite"} {
		t.Run(plan, func(t *testing.T) {
			c := nativeAccountDefaultsFixture()
			svc := nativeAccountDefaultsService(t, c)
			originalMapping := map[string]any{"custom": "custom-upstream", "gpt-6-astra": "other"}
			original := map[string]any{"plan_type": plan, "access_token": "fixture-token", "model_mapping": originalMapping, OpenAIModelMappingModeKey: "aliases"}
			originalExtra := map[string]any{AccountCostMultiplierExtraKey: 0.9, "keep": true}
			input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: original, Extra: originalExtra, Concurrency: 3, Priority: 99, GroupIDs: []int64{9, 2, 9}}
			require.NoError(t, svc.ApplyOAuthAutoConfig(t.Context(), input))
			require.Equal(t, 20, input.Concurrency)
			require.Equal(t, 20, *input.LoadFactor)
			require.Equal(t, 2, input.Priority)
			require.Equal(t, []int64{2, 9}, input.GroupIDs)
			require.Equal(t, map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6.1-sol"}, input.Credentials["model_mapping"])
			require.Equal(t, "whitelist", input.Credentials[OpenAIModelMappingModeKey])
			require.Equal(t, "fixture-token", input.Credentials["access_token"])
			require.Equal(t, 0.9, input.Extra[AccountCostMultiplierExtraKey])
			require.Equal(t, true, input.Extra["keep"])
			require.Equal(t, c.Revision, input.Extra["auto_config_initial_revision"])
			require.Equal(t, c.Revision, input.Extra["openai_account_defaults_revision"])
			require.Equal(t, "other", originalMapping["gpt-6-astra"])
			require.Equal(t, "aliases", original[OpenAIModelMappingModeKey])
			require.NotContains(t, originalExtra, "openai_account_defaults_revision")
		})
	}
}

func TestOpenAIAccountDefaultsFreePreservesImportedLoadFactorAndCost(t *testing.T) {
	for _, importedLF := range []*int{nil, func() *int { v := 6; return &v }()} {
		c := nativeAccountDefaultsFixture()
		input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"}, LoadFactor: importedLF}
		require.NoError(t, nativeAccountDefaultsService(t, c).ApplyOAuthAutoConfig(t.Context(), input))
		require.Equal(t, importedLF, input.LoadFactor)
		require.Equal(t, 2, input.Concurrency)
		require.Zero(t, input.Priority)
		require.NotContains(t, input.Extra, AccountCostMultiplierExtraKey, "native defaults preserve an omitted imported multiplier")
	}
}

func TestOpenAIAccountDefaultsDisabledKeepsLegacyPolicy(t *testing.T) {
	c := nativeAccountDefaultsFixture()
	c.OpenAIAccountDefaults.Enabled = false
	c.Priority, c.Concurrency, c.LoadFactor, c.CostMultiplier = 7, 9, 11, 0.07
	input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"plan_type": "unknown", "model_mapping": map[string]any{"custom": "target"}},
		Extra:       map[string]any{AccountCostMultiplierExtraKey: 0.9}}
	require.NoError(t, nativeAccountDefaultsService(t, c).ApplyOAuthAutoConfig(t.Context(), input))
	require.Equal(t, 7, input.Priority)
	require.Equal(t, 9, input.Concurrency)
	require.Equal(t, 11, *input.LoadFactor)
	require.Equal(t, 0.07, input.Extra[AccountCostMultiplierExtraKey])
	require.Contains(t, input.Credentials["model_mapping"], "custom")
	require.NotContains(t, input.Extra, "openai_account_defaults_revision")
}

func TestOpenAIAccountDefaultsCreationScope(t *testing.T) {
	for _, tc := range []struct {
		name, kind, platform, authMode string
		disabled, observer             bool
	}{
		{name: "global disabled", kind: AccountTypeOAuth, platform: PlatformOpenAI, disabled: true},
		{name: "API key", kind: AccountTypeAPIKey, platform: PlatformOpenAI},
		{name: "other platform", kind: AccountTypeOAuth, platform: PlatformAnthropic},
		{name: "agent identity", kind: AccountTypeOAuth, platform: PlatformOpenAI, authMode: OpenAIAuthModeAgentIdentity},
		{name: "personal access token", kind: AccountTypeOAuth, platform: PlatformOpenAI, authMode: OpenAIAuthModePersonalAccessToken},
		{name: "observer", kind: AccountTypeOAuth, platform: PlatformOpenAI, observer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := nativeAccountDefaultsFixture()
			c.Enabled = !tc.disabled
			ctx := t.Context()
			if tc.observer {
				ctx = WithObserverScope(ctx, []int64{25})
			}
			input := &CreateAccountInput{Platform: tc.platform, Type: tc.kind, Concurrency: 3, Priority: 42, GroupIDs: []int64{25},
				Credentials: map[string]any{"plan_type": "unknown", "auth_mode": tc.authMode},
				Extra:       map[string]any{"openai_account_defaults_revision": "imported", AccountCostMultiplierExtraKey: 0.9}}
			require.NoError(t, nativeAccountDefaultsService(t, c).ApplyOAuthAutoConfig(ctx, input))
			require.Equal(t, 3, input.Concurrency)
			require.Equal(t, 42, input.Priority)
			require.Equal(t, []int64{25}, input.GroupIDs)
			require.Nil(t, input.LoadFactor)
			require.NotContains(t, input.Extra, "openai_account_defaults_revision")
			require.NotContains(t, input.Extra, "auto_config_initial_revision")
			require.Equal(t, 0.9, input.Extra[AccountCostMultiplierExtraKey])
		})
	}
}

func TestOpenAIAccountDefaultsUnsupportedPlanStopsBeforePersistence(t *testing.T) {
	for _, plan := range []string{"unknown", "", "plus"} {
		svc := nativeAccountDefaultsService(t, nativeAccountDefaultsFixture())
		repo := &autoConfigAccountRepo{}
		svc.accountRepo = repo
		err := svc.ApplyOAuthAutoConfig(t.Context(), &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": plan}})
		require.ErrorContains(t, err, "no automatic configuration template")
		require.Nil(t, repo.created)
	}
}

func TestOAuthAutoConfigScopedKeyKeepsExplicitGroupsAndNetwork(t *testing.T) {
	for _, native := range []bool{false, true} {
		c := nativeAccountDefaultsFixture()
		c.OpenAIAccountDefaults.Enabled = native
		c.OpenAIAccountDefaults.DedicatedProxyCountryCodes = []string{"US"}
		principal := &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{
			Permissions: []string{"accounts.create", "accounts.credentials"}, GroupIDs: []int64{25}, AccountTypes: []string{AccountTypeOAuth},
		}}}}
		ctx := adminaccess.WithPrincipal(t.Context(), principal)
		input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3, Priority: 42,
			GroupIDs: []int64{25}, Credentials: map[string]any{"plan_type": "self_serve_business_prolite", "model_mapping": map[string]any{"custom": "target"}},
			Extra: map[string]any{AccountCostMultiplierExtraKey: 0.9}}
		// No proxy service/transaction: reaching global selection would fail.
		require.NoError(t, nativeAccountDefaultsService(t, c).ApplyOAuthAutoConfig(ctx, input))
		require.Equal(t, []int64{25}, input.GroupIDs)
		require.Nil(t, input.ProxyID)
		require.Equal(t, 3, input.Concurrency)
		require.Equal(t, 42, input.Priority)
		require.Nil(t, input.LoadFactor)
		require.Equal(t, map[string]any{"custom": "target"}, input.Credentials["model_mapping"])
		require.Equal(t, 0.9, input.Extra[AccountCostMultiplierExtraKey])
		require.NotContains(t, input.Extra, "auto_config_initial_revision")
		require.NotContains(t, input.Extra, "openai_account_defaults_revision")
	}
}

func TestOpenAIAccountDefaultsValidationAndProjection(t *testing.T) {
	valid := nativeAccountDefaultsFixture()
	require.NoError(t, ValidateOAuthAutoConfig(valid))
	for _, mutate := range []func(*OpenAIAccountDefaultsConfig){
		func(c *OpenAIAccountDefaultsConfig) { c.Templates = nil },
		func(c *OpenAIAccountDefaultsConfig) { c.Templates[""] = c.Templates["business"] },
		func(c *OpenAIAccountDefaultsConfig) { c.Templates["unknown"] = c.Templates["business"] },
		func(c *OpenAIAccountDefaultsConfig) {
			v := c.Templates["business"]
			v.Concurrency = 0
			c.Templates["business"] = v
		},
		func(c *OpenAIAccountDefaultsConfig) {
			v := c.Templates["business"]
			v.ModelMappings = nil
			c.Templates["business"] = v
		},
		func(c *OpenAIAccountDefaultsConfig) { c.DedicatedProxyCountryCodes = []string{"US", "US"} },
		func(c *OpenAIAccountDefaultsConfig) { c.DedicatedProxyCountryCodes = []string{"us"} },
		func(c *OpenAIAccountDefaultsConfig) { c.ModelExclusions = map[string][]string{"0": {"gpt-6-astra"}} },
		func(c *OpenAIAccountDefaultsConfig) { c.LegacyAccountIDs = []int64{0} },
		func(c *OpenAIAccountDefaultsConfig) { c.LegacyAccountIDs = []int64{42, 42} },
	} {
		c := nativeAccountDefaultsFixture()
		mutate(c.OpenAIAccountDefaults)
		require.Error(t, ValidateOAuthAutoConfig(c))
	}
	template, ok := OAuthAccountDefaultsTemplateFor(valid.OpenAIAccountDefaults, "self_serve_business_prolite")
	require.True(t, ok)
	require.Equal(t, map[string]any{"gpt-6-sol": "gpt-6.1-sol"}, OAuthAccountDefaultsModelMapping(template, []string{"gpt-6-astra"}))
	*template.LoadFactor = 999
	template.ModelMappings[0].To = "changed"
	require.Equal(t, 20, *valid.OpenAIAccountDefaults.Templates["business"].LoadFactor)
	require.Equal(t, "gpt-6-astra", valid.OpenAIAccountDefaults.Templates["business"].ModelMappings[0].To)
}
