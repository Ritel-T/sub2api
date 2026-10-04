package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

const GatewayBorrowModelsKey = "openai_gateway_borrow_models"
const GatewayBorrowQualityKey = "quality_candy_models"
const GatewayBorrowCandyAlgorithm = "ranxi-candy-sequential-four-v1"
const GatewayBorrowCandyPromptSHA256 = "df1a06950b3883d44cb2f1046164281bd7e6ba09c6792c3042e6658dfbb30eb5"

var GatewayBorrowPolicyKeys = []string{GatewayBorrowModelsKey, GatewayBorrowQualityKey, "quality_candy", "openai_excel_bps", ExcelBPSRequiredGroupIDsKey, ExcelBPSRequiredModelsKey}
var gatewayBorrowRunPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-zA-Z0-9_-]{1,64}$`)

type GatewayBorrowModelResult struct {
	State           string    `json:"state"`
	Model           string    `json:"model"`
	ReasoningEffort string    `json:"reasoning_effort"`
	ExpectedAnswer  string    `json:"expected_answer"`
	Correct         int       `json:"correct"`
	Total           int       `json:"total"`
	CheckedAt       time.Time `json:"checked_at"`
	RunID           string    `json:"run_id"`
	Algorithm       string    `json:"algorithm"`
	PromptSHA256    string    `json:"prompt_sha256"`
}
type GatewayBorrowPolicyObservation struct {
	ObservedAt       time.Time                           `json:"observed_at"`
	ExpectedProxyID  *int64                              `json:"expected_proxy_id"`
	CredentialSHA256 string                              `json:"credential_sha256"`
	ExpectedPolicy   map[string]any                      `json:"expected_policy"`
	ModelResults     map[string]GatewayBorrowModelResult `json:"model_results"`
	BorrowModels     []string                            `json:"borrow_models"`
	RetireBPS        bool                                `json:"retire_bps"`
}
type GatewayBorrowPolicyResult struct {
	Applied bool   `json:"applied"`
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason"`
}
type GatewayBorrowPolicyRepository interface {
	UpdateGatewayBorrowPolicyIfObserved(context.Context, int64, GatewayBorrowPolicyObservation) (GatewayBorrowPolicyResult, error)
}

func GatewayBorrowPolicyProjection(extra map[string]any) map[string]any {
	out := make(map[string]any, len(GatewayBorrowPolicyKeys))
	for _, key := range GatewayBorrowPolicyKeys {
		out[key] = extra[key]
	}
	return out
}
func gatewayBorrowCanonicalModel(model string) bool {
	return model == "gpt-6-astra" || model == "gpt-6.1-sol" || model == "gpt-6-sol"
}

// Match Python json.dumps(identity, sort_keys=True), including ASCII escaping,
// nulls and its default separators. Never return or log the credential values.
func GatewayBorrowCredentialSHA256(credentials map[string]any) string {
	value := func(key string) string {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(credentials[key])
		raw := strings.TrimSuffix(b.String(), "\n")
		var out strings.Builder
		for _, r := range raw {
			if r <= 127 {
				_, _ = out.WriteRune(r)
			} else if r <= 0xffff {
				fmt.Fprintf(&out, "\\u%04x", r)
			} else {
				h, l := utf16.EncodeRune(r)
				fmt.Fprintf(&out, "\\u%04x\\u%04x", h, l)
			}
		}
		return out.String()
	}
	raw := "{\"access_token\": " + value("access_token") + ", \"chatgpt_account_id\": " + value("chatgpt_account_id") + "}"
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}
func invalidGatewayBorrowPolicy() error {
	return infraerrors.New(http.StatusBadRequest, "INVALID_GATEWAY_BORROW_POLICY", "Invalid gateway borrow policy observation")
}
func (s *RateLimitService) UpdateGatewayBorrowPolicyFromProbe(ctx context.Context, id int64, o GatewayBorrowPolicyObservation) (GatewayBorrowPolicyResult, error) {
	result := GatewayBorrowPolicyResult{Skipped: true, Reason: "stale_observation"}
	now := time.Now().UTC()
	_, offset := o.ObservedAt.Zone()
	if id <= 0 || o.ObservedAt.IsZero() || offset != 0 || o.ObservedAt.After(now.Add(30*time.Second)) || !nativeRateLimitTokenHashPattern.MatchString(o.CredentialSHA256) || (o.ExpectedProxyID != nil && *o.ExpectedProxyID <= 0) || o.ExpectedPolicy == nil || o.BorrowModels == nil || len(o.ModelResults) > 3 || (len(o.ModelResults) == 0 && !o.RetireBPS) {
		return result, invalidGatewayBorrowPolicy()
	}
	for key := range o.ExpectedPolicy {
		if !slices.Contains(GatewayBorrowPolicyKeys, key) {
			return result, invalidGatewayBorrowPolicy()
		}
	}
	seen := map[string]bool{}
	for _, model := range o.BorrowModels {
		if !gatewayBorrowCanonicalModel(model) || seen[model] {
			return result, invalidGatewayBorrowPolicy()
		}
		seen[model] = true
	}
	for model, r := range o.ModelResults {
		_, off := r.CheckedAt.Zone()
		if !gatewayBorrowCanonicalModel(model) || r.Model != model || r.Algorithm != GatewayBorrowCandyAlgorithm || r.PromptSHA256 != GatewayBorrowCandyPromptSHA256 || r.ExpectedAnswer != "21" || !slices.Contains([]string{"low", "medium", "high", "xhigh"}, r.ReasoningEffort) || !gatewayBorrowRunPattern.MatchString(r.RunID) || r.CheckedAt.IsZero() || off != 0 || r.CheckedAt.After(now.Add(30*time.Second)) || r.Total < 1 || r.Total > 4 || r.Correct < 0 || r.Correct > r.Total {
			return result, invalidGatewayBorrowPolicy()
		}
		if r.State == "inconclusive" {
			continue
		}
		if (r.State != "healthy" && r.State != "degraded") || (r.Total != 1 && r.Total != 4) || (r.Total == 4 && ((r.Correct >= 3) != (r.State == "healthy"))) || (r.Total == 1 && ((r.Correct == 1) != (r.State == "healthy"))) {
			return result, invalidGatewayBorrowPolicy()
		}
		if now.Sub(r.CheckedAt) > 10*time.Minute {
			return result, nil
		}
	}
	if now.Sub(o.ObservedAt) > 10*time.Minute {
		return result, nil
	}
	if s == nil || s.accountRepo == nil {
		return result, infraerrors.New(http.StatusServiceUnavailable, "GATEWAY_BORROW_POLICY_UNAVAILABLE", "Gateway borrow policy persistence unavailable")
	}
	repo, ok := s.accountRepo.(GatewayBorrowPolicyRepository)
	if !ok {
		return result, infraerrors.New(http.StatusServiceUnavailable, "GATEWAY_BORROW_POLICY_UNAVAILABLE", "Gateway borrow policy persistence unavailable")
	}
	stateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return repo.UpdateGatewayBorrowPolicyIfObserved(stateCtx, id, o)
}

// Build under the repository's row lock; metadata refreshes never change a
// classification or route from a single probe, and errors never erase evidence.
func BuildGatewayBorrowPolicyUpdates(extra map[string]any, o GatewayBorrowPolicyObservation) (map[string]any, string, error) {
	raw, err := json.Marshal(extra)
	if err != nil {
		return nil, "", err
	}
	var current map[string]any
	if err = json.Unmarshal(raw, &current); err != nil {
		return nil, "", err
	}
	records := map[string]any{}
	if v, exists := current[GatewayBorrowQualityKey]; exists && v != nil {
		var ok bool
		records, ok = v.(map[string]any)
		if !ok {
			return nil, "invalid_stored_policy", nil
		}
	}
	oldBorrow := []string{}
	if v := current[GatewayBorrowModelsKey]; v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, "", err
		}
		if json.Unmarshal(b, &oldBorrow) != nil {
			return nil, "invalid_stored_policy", nil
		}
	}
	oldHas := func(m string) bool { return slices.Contains(oldBorrow, m) }
	newHas := func(m string) bool { return slices.Contains(o.BorrowModels, m) }
	legacyBorrow := []string{}
	if o.RetireBPS {
		marker, _ := current["quality_candy"].(map[string]any)
		if !validGatewayBorrowStoredClassification(marker, "gpt-6-astra") {
			return nil, "bps_not_candy_owned", nil
		}
		groups, groupOK := current[ExcelBPSRequiredGroupIDsKey].([]any)
		models, modelOK := current[ExcelBPSRequiredModelsKey].([]any)
		if current[ExcelBPSRequiredGroupIDsKey] != nil && !groupOK {
			return nil, "invalid_stored_policy", nil
		}
		if len(groups) > 0 && (!modelOK || len(models) == 0) {
			return nil, "invalid_stored_policy", nil
		}
		if current["openai_excel_bps"] == true && len(groups) == 0 {
			return nil, "bps_not_candy_owned", nil
		}
		if len(groups) > 0 && current[GatewayBorrowModelsKey] == nil && current["openai_excel_bps"] != true {
			return nil, "unproven_route_change", nil
		}
		if len(groups) > 0 && current[GatewayBorrowModelsKey] != nil {
			return nil, "legacy_scope_conflicts_with_borrow_policy", nil
		}
		for _, value := range groups {
			id, ok := value.(float64)
			if !ok || id <= 0 || id != float64(int64(id)) {
				return nil, "invalid_stored_policy", nil
			}
		}
		if len(groups) > 0 && current[GatewayBorrowModelsKey] == nil && current["openai_excel_bps"] == true {
			for _, value := range models {
				model, ok := value.(string)
				if !ok || !gatewayBorrowCanonicalModel(model) {
					return nil, "invalid_stored_policy", nil
				}
				if model == "gpt-6-sol" {
					model = "gpt-6.1-sol"
				}
				if !slices.Contains(legacyBorrow, model) {
					legacyBorrow = append(legacyBorrow, model)
				}
			}
		}
	}
	// Seed only the pre-existing fail-closed scope; an incomplete new-model
	// probe cannot remove a route restriction during BPS retirement.
	baselineHas := func(m string) bool { return oldHas(m) || slices.Contains(legacyBorrow, m) }
	for _, m := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"} {
		r, provided := o.ModelResults[m]
		if !provided || r.State == "inconclusive" {
			if baselineHas(m) != newHas(m) {
				return nil, "unproven_route_change", nil
			}
			continue
		}
		previous, _ := records[m].(map[string]any)
		if previous == nil && m == "gpt-6-astra" {
			previous, _ = current["quality_candy"].(map[string]any)
		}
		if previous != nil {
			if at, _ := previous["latest_probe_at"].(string); at != "" {
				if parsed, e := time.Parse(time.RFC3339Nano, at); e != nil || !r.CheckedAt.After(parsed) {
					return nil, "stale_model_result", nil
				}
			}
			if at, _ := previous["checked_at"].(string); at != "" {
				if parsed, e := time.Parse(time.RFC3339Nano, at); e != nil || !r.CheckedAt.After(parsed) {
					return nil, "stale_model_result", nil
				}
			}
		}
		if r.Total == 1 {
			if !validGatewayBorrowStoredClassification(previous, m) || previous["state"] != r.State || previous["expected_answer"] != r.ExpectedAnswer || previous["reasoning_effort"] != r.ReasoningEffort || baselineHas(m) != newHas(m) {
				return nil, "single_probe_requires_matching_classification", nil
			}
			records[m] = previous
		} else {
			if newHas(m) != (r.State == "degraded") {
				return nil, "unproven_route_change", nil
			}
			b, err := json.Marshal(r)
			if err != nil {
				return nil, "", err
			}
			var record map[string]any
			_ = json.Unmarshal(b, &record)
			record["version"] = 1
			records[m] = record
		}
		record, ok := records[m].(map[string]any)
		if !ok || record == nil {
			return nil, "invalid_stored_policy", nil
		}
		record["latest_probe_at"] = r.CheckedAt.UTC().Format(time.RFC3339Nano)
		record["latest_probe_credential_sha256"] = o.CredentialSHA256
		record["latest_probe_proxy_id"] = o.ExpectedProxyID
	}
	updates := map[string]any{GatewayBorrowModelsKey: o.BorrowModels, GatewayBorrowQualityKey: records}
	if r, ok := o.ModelResults["gpt-6-astra"]; ok && r.Total == 4 && r.State != "inconclusive" {
		b, _ := json.Marshal(r)
		var record map[string]any
		_ = json.Unmarshal(b, &record)
		record["version"] = 1
		updates["quality_candy"] = record
	}
	if o.RetireBPS {
		updates["openai_excel_bps"] = false
		updates[ExcelBPSRequiredGroupIDsKey] = []int64{}
		updates[ExcelBPSRequiredModelsKey] = []string{}
		return updates, "legacy_policy_migrated", nil
	}
	return updates, "", nil
}

func validGatewayBorrowStoredClassification(record map[string]any, model string) bool {
	if record == nil || record["version"] != float64(1) || record["model"] != model || record["algorithm"] != GatewayBorrowCandyAlgorithm || record["prompt_sha256"] != GatewayBorrowCandyPromptSHA256 || record["total"] != float64(4) || record["expected_answer"] != "21" {
		return false
	}
	correct, ok := record["correct"].(float64)
	if !ok || correct < 0 || correct > 4 || correct != float64(int(correct)) {
		return false
	}
	return (correct >= 3 && record["state"] == "healthy") || (correct < 3 && record["state"] == "degraded")
}
