package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// OAuthAccountModelReconcileRepository changes model admission without replacing
// a refreshed token or any account scheduling/capacity setting.
type OAuthAccountModelReconcileRepository interface {
	ReconcileOAuthAccountModels(context.Context, *Account, map[string]string) (bool, error)
}

// OAuthAccountModelReconcileState contains only inputs used by the model policy.
// Tokens and unrelated usage observations deliberately do not participate in CAS.
func OAuthAccountModelReconcileState(a *Account) map[string]any {
	if a == nil {
		return nil
	}
	candy, _ := a.Extra["quality_candy"].(map[string]any)
	results, _ := a.Extra["quality_model_results"].(map[string]any)
	_, bpsModelsPresent := a.Extra["openai_excel_bps_models"]
	return map[string]any{
		"platform": a.Platform, "type": a.Type, "parent_account_id": a.ParentAccountID,
		"plan_type":          a.Credentials["plan_type"],
		"auth_mode":          a.Credentials[openAIAuthModeCredentialKey],
		"legacy_auth_mode":   a.Credentials[openAIAuthModeLegacyCredentialKey],
		"chatgpt_account_id": a.Credentials["chatgpt_account_id"],
		"chatgpt_user_id":    a.Credentials["chatgpt_user_id"],
		"organization_id":    a.Credentials["organization_id"],
		"model_mapping":      a.Credentials["model_mapping"],
		"model_mapping_mode": a.Credentials[OpenAIModelMappingModeKey],
		"native_revision":    a.Extra["openai_account_defaults_revision"],
		"native_identity":    a.Extra[nativeOAuthAccountDefaultsIdentityKey],
		"quality_version":    candy["version"], "quality_state": candy["state"],
		"quality_results": map[string]any{"gpt-6-sol": results["gpt-6-sol"], "gpt-6.1-sol": results["gpt-6.1-sol"], "gpt-6-astra": results["gpt-6-astra"]},
		"bps":             a.Extra["openai_excel_bps"], "bps_models_present": bpsModelsPresent,
		"bps_models":                          a.Extra["openai_excel_bps_models"],
		GatewayBorrowAccountQualityModeKey:    a.Extra[GatewayBorrowAccountQualityModeKey],
		GatewayBorrowAccountQualityPendingKey: a.Extra[GatewayBorrowAccountQualityPendingKey],
		GatewayBorrowInitialReadyKey:          a.Extra[GatewayBorrowInitialReadyKey],
	}
}

func OAuthAccountModelReconcileEligible(a *Account) bool {
	if a == nil || !a.IsOpenAIOAuth() || a.IsShadow() || a.IsOpenAIAgentIdentity() || a.IsOpenAIPersonalAccessToken() {
		return false
	}
	// A free account without Astra has no initial classification to coordinate.
	// Initial Astra classification owns the model scope until it finishes.
	return !(a.gatewayBorrowAccountQualityPending() && a.gatewayBorrowAccountHasAstraPermission())
}

// OAuthAccountModelMapping preserves the previous completed-account model-only
// reconciliation: static exclusions, then Candy/BPS/legacy quality precedence.
func OAuthAccountModelMapping(c *OpenAIAccountDefaultsConfig, a *Account) (map[string]string, bool) {
	if c == nil || !c.Enabled || !OAuthAccountModelReconcileEligible(a) {
		return nil, false
	}
	template, ok := OAuthAccountDefaultsTemplateFor(c, a.GetCredential("plan_type"))
	if !ok {
		return nil, false
	}
	excluded := make(map[string]bool)
	for _, model := range c.ModelExclusions[strconv.FormatInt(a.ID, 10)] {
		excluded[model] = true
	}
	candy, _ := a.Extra["quality_candy"].(map[string]any)
	version, _ := json.Marshal(candy["version"])
	classified := string(version) == "1" && (candy["state"] == "healthy" || candy["state"] == "degraded")
	results, _ := a.Extra["quality_model_results"].(map[string]any)
	for _, model := range []string{"gpt-6-sol", "gpt-6.1-sol", "gpt-6-astra"} {
		if classified || oauthAccountBPSRoutesModel(a.Extra, model) || results[model] == "good" {
			delete(excluded, model)
		} else if results[model] == "degraded" {
			excluded[model] = true
		}
	}
	mapping := make(map[string]string, len(template.ModelMappings))
	for _, rule := range template.ModelMappings {
		from := strings.TrimSpace(rule.From)
		if !excluded[from] {
			mapping[from] = strings.TrimSpace(rule.To)
		}
	}
	return mapping, true
}

func oauthAccountBPSRoutesModel(extra map[string]any, model string) bool {
	if extra["openai_excel_bps"] != true {
		return false
	}
	value, present := extra["openai_excel_bps_models"]
	if !present {
		return true
	}
	switch models := value.(type) {
	case []string:
		for _, candidate := range models {
			if candidate == model {
				return true
			}
		}
	case []any:
		for _, candidate := range models {
			if candidate == model {
				return true
			}
		}
	}
	return false
}

func oauthModelMappingEqual(actual any, desired map[string]string) bool {
	// Persistence decodes maps to map[string]any, while in-process tests/callers
	// may retain map[string]string. JSON is the repository representation.
	a, err := json.Marshal(actual)
	if err != nil {
		return false
	}
	b, err := json.Marshal(desired)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// Called by AccountOps' existing worker at the model-reconciliation cadence.
// Model maintenance never changes capacity, groups, proxy or scheduling.
// A proven same-owner token refresh may renew only initial readiness via CAS.
func (s *AccountOpsService) reconcileOAuthAccountModels(parent context.Context) {
	if s == nil || s.accounts == nil {
		return
	}
	c, _ := s.autoConfig.Load().(OAuthAutoConfig)
	if !c.Enabled || c.Platform != PlatformOpenAI || c.OpenAIAccountDefaults == nil || !c.OpenAIAccountDefaults.Enabled {
		return
	}
	repo, ok := s.accounts.(OAuthAccountModelReconcileRepository)
	if !ok {
		slog.Warn("oauth_account_models_reconcile_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	accounts, err := s.accounts.ListAllWithFilters(ctx, PlatformOpenAI, AccountTypeOAuth, "", "", 0, "")
	if err != nil {
		slog.Warn("oauth_account_models_reconcile_failed", "stage", "list")
		return
	}
	for i := range accounts {
		if ctx.Err() != nil {
			return
		}
		account := &accounts[i]
		revision, _ := account.Extra["openai_account_defaults_revision"].(string)
		if revision == "" && !slices.Contains(c.OpenAIAccountDefaults.LegacyAccountIDs, account.ID) {
			// Observer-created accounts deliberately skipped the global creation
			// policy. Only explicitly migrated legacy rows join the model worker.
			continue
		}
		if revision != "" && account.Status == StatusActive && account.Schedulable &&
			account.gatewayBorrowAccountQualityPending() && account.gatewayBorrowAccountHasAstraPermission() &&
			!GatewayBorrowInitialReadyMatchesCurrent(account) && nativeOAuthAccountDefaultsIdentityMatches(account) {
			// A same-owner refresh can rotate the token before initial sampling.
			// The stored creation proof must still bind this proxy and config.
			ready, err := markNativeOAuthInitialReady(ctx, s.accounts, account)
			if err != nil {
				slog.Warn("oauth_account_initial_ready_reconcile_failed", "account_id", account.ID)
				continue
			}
			account = ready
		}
		desired, applicable := OAuthAccountModelMapping(c.OpenAIAccountDefaults, account)
		if !applicable || oauthModelMappingEqual(account.Credentials["model_mapping"], desired) {
			continue
		}
		if _, err := repo.ReconcileOAuthAccountModels(ctx, account, desired); err != nil {
			slog.Warn("oauth_account_models_reconcile_failed", "stage", "update", "account_id", account.ID)
		}
	}
}
