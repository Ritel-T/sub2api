package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"time"
)

const GatewayBorrowAccountQualityModeKey = "openai_gateway_borrow_quality_mode"
const GatewayBorrowAccountQualityMode = "astra_controls_sol_v2"
const GatewayBorrowAccountQualityPendingKey = "openai_gateway_borrow_quality_pending"
const gatewayBorrowAccountQualityReuseAge = 24 * time.Hour

// GatewayBorrowAccountQualityClassification always describes actual Astra
// samples. Sol consumes the account's operating classification, not invented
// Sol evidence. LatestProbeAt may refresh a completed round with a matching
// single sample; an unfinished or contrary sample cannot change that round.
type GatewayBorrowAccountQualityClassification struct {
	State         string
	CheckedAt     time.Time
	LatestProbeAt time.Time
	Correct       int
	Total         int
}

func GatewayBorrowAccountQualityLinkedModel(model string) bool {
	return gatewayBorrowCanonicalModel(model)
}

func GatewayBorrowAccountQuality(a *Account, now time.Time, maxAge time.Duration) (GatewayBorrowAccountQualityClassification, bool) {
	if a == nil || !a.IsOpenAIOAuth() || a.IsShadow() || a.Extra[GatewayBorrowAccountQualityModeKey] != GatewayBorrowAccountQualityMode || a.Extra[GatewayBorrowAccountQualityPendingKey] == true {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	hash := a.SchedulerCredentialSHA256
	if a.GetOpenAIAccessToken() != "" {
		hash = GatewayBorrowCredentialSHA256(a.Credentials)
	}
	if !nativeRateLimitTokenHashPattern.MatchString(hash) {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	record, _ := a.Extra["quality_candy"].(map[string]any)
	return gatewayBorrowAccountQualityRecord(record, hash, a.ProxyID, now, maxAge)
}

func gatewayBorrowAccountQualityRecord(record map[string]any, credentialHash string, proxyID *int64, now time.Time, maxAge time.Duration) (GatewayBorrowAccountQualityClassification, bool) {
	// Normalize numbers and proxy pointers just as JSON persistence does.
	b, err := json.Marshal(record)
	if err != nil {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	var normalized map[string]any
	if json.Unmarshal(b, &normalized) != nil || !validGatewayBorrowStoredClassification(normalized, "gpt-6-astra") || normalized["reasoning_effort"] != "medium" || normalized["latest_probe_credential_sha256"] != credentialHash {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	var expectedProxy any
	if proxyID != nil {
		expectedProxy = float64(*proxyID)
	}
	_, proxyObserved := normalized["latest_probe_proxy_id"]
	if !proxyObserved || !reflect.DeepEqual(normalized["latest_probe_proxy_id"], expectedProxy) {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	checked, err := time.Parse(time.RFC3339Nano, stringField(normalized, "checked_at"))
	if err != nil || !gatewayBorrowRunPattern.MatchString(stringField(normalized, "run_id")) {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	latest, err := time.Parse(time.RFC3339Nano, stringField(normalized, "latest_probe_at"))
	if err != nil || latest.Before(checked) || checked.After(now.Add(30*time.Second)) || latest.After(now.Add(30*time.Second)) || maxAge <= 0 || now.Sub(latest) > maxAge {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	correct, ok := normalized["correct"].(float64)
	if !ok {
		return GatewayBorrowAccountQualityClassification{}, false
	}
	return GatewayBorrowAccountQualityClassification{State: stringField(normalized, "state"), CheckedAt: checked, LatestProbeAt: latest, Correct: int(correct), Total: 4}, true
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// Legacy records place identity-bound refresh metadata in quality_candy_models.
// Select the newest complete Astra classification, without averaging earlier
// failures or falling back to Sol's old independent classification.
func gatewayBorrowReusableAstraRecord(extra map[string]any, hash string, proxy *int64, now time.Time) (map[string]any, bool) {
	candidates := []map[string]any{}
	if r, ok := extra["quality_candy"].(map[string]any); ok {
		candidates = append(candidates, r)
	}
	if extra[GatewayBorrowAccountQualityModeKey] != GatewayBorrowAccountQualityMode {
		models, _ := extra[GatewayBorrowQualityKey].(map[string]any)
		if r, ok := models["gpt-6-astra"].(map[string]any); ok {
			candidates = append(candidates, r)
		}
	}
	var newest map[string]any
	var newestAt time.Time
	var newestProbeAt time.Time
	for _, r := range candidates {
		classification, ok := gatewayBorrowAccountQualityRecord(r, hash, proxy, now, gatewayBorrowAccountQualityReuseAge)
		if ok && (newest == nil || classification.CheckedAt.After(newestAt) || (classification.CheckedAt.Equal(newestAt) && classification.LatestProbeAt.After(newestProbeAt))) {
			newest, newestAt, newestProbeAt = r, classification.CheckedAt, classification.LatestProbeAt
		}
	}
	return newest, newest != nil
}

func buildGatewayBorrowAccountQualityUpdates(extra map[string]any, o GatewayBorrowPolicyObservation) (map[string]any, string, error) {
	if extra[GatewayBorrowAccountQualityPendingKey] == true && extra[GatewayBorrowInitialReadyKey] != true {
		return nil, "initial_defaults_not_ready", nil
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return nil, "", err
	}
	var current map[string]any
	if err = json.Unmarshal(b, &current); err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	previous, hasPrevious := gatewayBorrowReusableAstraRecord(current, o.CredentialSHA256, o.ExpectedProxyID, now)
	r, provided := o.ModelResults["gpt-6-astra"]
	if len(o.ModelResults) > 1 || (len(o.ModelResults) == 1 && !provided) || (provided && (r.Model != "gpt-6-astra" || r.ReasoningEffort != "medium")) {
		return nil, "invalid_account_quality_evidence", nil
	}
	var record map[string]any
	if provided && r.State == "inconclusive" {
		return nil, "inconclusive_account_quality", nil
	}
	if !provided {
		if !hasPrevious {
			return nil, "account_classification_required", nil
		}
		record = previous
	} else {
		if hasPrevious {
			prior, _ := gatewayBorrowAccountQualityRecord(previous, o.CredentialSHA256, o.ExpectedProxyID, now, gatewayBorrowAccountQualityReuseAge)
			if !r.CheckedAt.After(prior.LatestProbeAt) {
				return nil, "stale_model_result", nil
			}
		}
		switch r.Total {
		case 1:
			if !hasPrevious || previous["state"] != r.State {
				return nil, "single_probe_requires_matching_classification", nil
			}
			record = previous
		case 4:
			raw, _ := json.Marshal(r)
			_ = json.Unmarshal(raw, &record)
			record["version"] = float64(1)
		default:
			return nil, "account_classification_required", nil
		}
		record["latest_probe_at"] = r.CheckedAt.UTC().Format(time.RFC3339Nano)
		record["latest_probe_credential_sha256"] = o.CredentialSHA256
		record["latest_probe_proxy_id"] = o.ExpectedProxyID
	}
	if !validGatewayBorrowStoredClassification(record, "gpt-6-astra") {
		return nil, "invalid_account_quality_evidence", nil
	}
	want := []string{}
	if record["state"] == "degraded" {
		want = []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}
	}
	// Accept either explicit legacy alias or the two canonical wire names. The
	// stored policy always protects all three client-facing names.
	got := slices.Clone(o.BorrowModels)
	if slices.Contains(got, "gpt-6.1-sol") && !slices.Contains(got, "gpt-6-sol") {
		got = append(got, "gpt-6-sol")
	}
	slices.Sort(got)
	wantSorted := slices.Clone(want)
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		return nil, "unproven_route_change", nil
	}
	updates := map[string]any{GatewayBorrowAccountQualityModeKey: GatewayBorrowAccountQualityMode, GatewayBorrowAccountQualityPendingKey: false, "quality_candy": record, GatewayBorrowModelsKey: want}
	// Keep historical Sol rows intact. Astra retains actual, identity-bound
	// evidence for older readers; no synthetic Sol test result is produced.
	records := map[string]any{}
	if stored := current[GatewayBorrowQualityKey]; stored != nil {
		var ok bool
		records, ok = stored.(map[string]any)
		if !ok {
			return nil, "invalid_stored_policy", nil
		}
	}
	records["gpt-6-astra"] = record
	updates[GatewayBorrowQualityKey] = records
	if o.RetireBPS {
		// Reuse legacy ownership checks without asking it to infer a Sol result.
		legacyExtra := make(map[string]any, len(current))
		for k, v := range current {
			legacyExtra[k] = v
		}
		delete(legacyExtra, GatewayBorrowAccountQualityModeKey)
		oldModels := []string{}
		if old := current[GatewayBorrowModelsKey]; old != nil {
			raw, _ := json.Marshal(old)
			if json.Unmarshal(raw, &oldModels) != nil {
				return nil, "invalid_stored_policy", nil
			}
		} else if current["openai_excel_bps"] == true {
			raw, _ := json.Marshal(current[ExcelBPSRequiredModelsKey])
			_ = json.Unmarshal(raw, &oldModels)
			for i := range oldModels {
				if oldModels[i] == "gpt-6-sol" {
					oldModels[i] = "gpt-6.1-sol"
				}
			}
			oldModels = slices.Compact(oldModels)
		}
		legacy := GatewayBorrowPolicyObservation{BorrowModels: oldModels, RetireBPS: true}
		_, reason, err := BuildGatewayBorrowPolicyUpdates(legacyExtra, legacy)
		if err != nil || reason != "legacy_policy_migrated" {
			return nil, reason, err
		}
		updates["openai_excel_bps"] = false
		updates[ExcelBPSRequiredGroupIDsKey] = []int64{}
		updates[ExcelBPSRequiredModelsKey] = []string{}
	}
	return updates, "account_quality_applied", nil
}
