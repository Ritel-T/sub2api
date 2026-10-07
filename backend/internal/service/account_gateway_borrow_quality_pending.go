package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"maps"
	"strings"
)

const GatewayBorrowInitialReadyKey = "openai_gateway_borrow_initial_ready"
const GatewayBorrowInitialReadyFingerprintKey = "openai_gateway_borrow_initial_ready_fingerprint"

// New provider identities must be classified after external defaults finish.
// Imported quality records are observations of another account row, not proof.
func prepareGatewayBorrowAccountQualityForCreate(a *Account) {
	if a == nil || !a.IsOpenAIOAuth() || a.IsShadow() || a.IsOpenAIAgentIdentity() || a.IsOpenAIPersonalAccessToken() {
		return
	}
	a.Extra = maps.Clone(a.Extra)
	if a.Extra == nil {
		a.Extra = make(map[string]any)
	}
	a.Extra[GatewayBorrowAccountQualityModeKey] = GatewayBorrowAccountQualityMode
	a.Extra[GatewayBorrowAccountQualityPendingKey] = true
	a.Extra[GatewayBorrowInitialReadyKey] = false
	delete(a.Extra, GatewayBorrowInitialReadyFingerprintKey)
	delete(a.Extra, "quality_candy")
	delete(a.Extra, GatewayBorrowQualityKey)
}

// Ordinary account edits and reauthorization cannot manufacture a completed
// classification or undo a pending initial check. The narrow policy API owns
// these fields; unrelated administrator BPS configuration remains editable.
func MergeOpenAIGatewayAccountQualityExtra(incoming, current map[string]any) map[string]any {
	out := maps.Clone(incoming)
	if out == nil {
		out = make(map[string]any)
	}
	keys := []string{GatewayBorrowAccountQualityModeKey, GatewayBorrowAccountQualityPendingKey, GatewayBorrowInitialReadyKey, GatewayBorrowInitialReadyFingerprintKey}
	if current[GatewayBorrowAccountQualityModeKey] == GatewayBorrowAccountQualityMode {
		keys = append(keys, "quality_candy", GatewayBorrowQualityKey, GatewayBorrowModelsKey)
	}
	for _, key := range keys {
		if value, exists := current[key]; exists {
			out[key] = value
		} else {
			delete(out, key)
		}
	}
	return out
}

func (a *Account) gatewayBorrowAccountQualityPending() bool {
	return a != nil && a.IsOpenAIOAuth() && !a.IsShadow() && !a.IsOpenAIAgentIdentity() && !a.IsOpenAIPersonalAccessToken() &&
		a.Extra[GatewayBorrowAccountQualityModeKey] == GatewayBorrowAccountQualityMode && a.Extra[GatewayBorrowAccountQualityPendingKey] == true
}

// Without Astra permission no replacement Sol probe or extra service
// restriction is introduced. If defaults later add Astra, pending takes effect.
func (a *Account) gatewayBorrowAccountHasAstraPermission() bool {
	if a == nil {
		return false
	}
	if a.IsModelSupported("gpt-6-astra") && config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(a.GetMappedModel("gpt-6-astra"))) == "gpt-6-astra" {
		return true
	}
	for public, upstream := range a.GetModelMapping() {
		if config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(upstream)) == "gpt-6-astra" && a.IsModelSupported(public) {
			return true
		}
	}
	return false
}

// This is a business-admission gate, not an upstream capability predicate.
// Keep IsModelSupported unchanged so direct native quality probes still work.
func (a *Account) IsOpenAIGatewayAccountQualityPendingForModel(model string) bool {
	if a == nil {
		return false
	}
	return a.IsOpenAIGatewayAccountQualityPendingForUpstreamModel(a.GetMappedModel(strings.TrimSpace(model)))
}
func (a *Account) IsOpenAIGatewayAccountQualityPendingForUpstreamModel(model string) bool {
	return a.gatewayBorrowAccountQualityPending() && a.gatewayBorrowAccountHasAstraPermission() &&
		GatewayBorrowAccountQualityLinkedModel(config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(model)))
}

// Key-level account edits can neither erase nor supply managed gate fields.
func stripGatewayBorrowAccountQualityManagedExtraUpdates(updates map[string]any, protectClassification bool) map[string]any {
	out := maps.Clone(updates)
	for _, key := range []string{GatewayBorrowAccountQualityModeKey, GatewayBorrowAccountQualityPendingKey, GatewayBorrowInitialReadyKey, GatewayBorrowInitialReadyFingerprintKey} {
		delete(out, key)
	}
	if protectClassification {
		for _, key := range []string{"quality_candy", GatewayBorrowQualityKey, GatewayBorrowModelsKey} {
			delete(out, key)
		}
	}
	return out
}
