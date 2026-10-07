package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"maps"
	"net/http"
	"slices"
	"time"
)

// No credential material is transported. Identity is compared to the same
// fingerprint already used by the quality policy endpoint.
type GatewayBorrowInitialReadyObservation struct {
	ObservedAt       time.Time      `json:"observed_at"`
	ExpectedProxyID  *int64         `json:"expected_proxy_id"`
	CredentialSHA256 string         `json:"credential_sha256"`
	ExpectedPolicy   map[string]any `json:"expected_policy"`
	ExpectedConfig   map[string]any `json:"expected_config"`
}
type GatewayBorrowInitialReadyRepository interface {
	MarkGatewayBorrowInitialReadyIfObserved(context.Context, int64, GatewayBorrowInitialReadyObservation) (GatewayBorrowPolicyResult, error)
}

var gatewayBorrowInitialConfigKeys = []string{"concurrency", "load_factor", "priority", "group_ids", "model_mapping"}

// Five nonsecret defaults fields, with bindings sorted and deduplicated.
func GatewayBorrowInitialConfiguration(a *Account) map[string]any {
	groups := slices.Clone(a.GroupIDs)
	slices.Sort(groups)
	groups = slices.Compact(groups)
	if groups == nil {
		groups = []int64{}
	}
	return map[string]any{"concurrency": a.Concurrency, "load_factor": a.LoadFactor, "priority": a.Priority, "group_ids": groups, "model_mapping": a.Credentials["model_mapping"]}
}

// Never return the fingerprint's preimage: it includes credential identity.
func GatewayBorrowInitialReadyFingerprint(a *Account) string {
	if a == nil {
		return ""
	}
	raw, err := json.Marshal(map[string]any{
		"credential_sha256": GatewayBorrowCredentialSHA256(a.Credentials),
		"proxy_id":          a.ProxyID, "config": GatewayBorrowInitialConfiguration(a),
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func GatewayBorrowInitialReadyMatchesCurrent(a *Account) bool {
	if a == nil || a.Extra[GatewayBorrowInitialReadyKey] != true {
		return false
	}
	fingerprint, ok := a.Extra[GatewayBorrowInitialReadyFingerprintKey].(string)
	return ok && fingerprint != "" && fingerprint == GatewayBorrowInitialReadyFingerprint(a)
}

func (s *RateLimitService) MarkGatewayBorrowInitialReady(ctx context.Context, id int64, o GatewayBorrowInitialReadyObservation) (GatewayBorrowPolicyResult, error) {
	result := GatewayBorrowPolicyResult{Skipped: true, Reason: "stale_observation"}
	invalid := func() (GatewayBorrowPolicyResult, error) {
		return result, infraerrors.New(http.StatusBadRequest, "INVALID_GATEWAY_BORROW_INITIAL_READY", "Invalid initial defaults observation")
	}
	now := time.Now().UTC()
	_, off := o.ObservedAt.Zone()
	if id <= 0 || o.ObservedAt.IsZero() || off != 0 || o.ObservedAt.After(now.Add(30*time.Second)) || !nativeRateLimitTokenHashPattern.MatchString(o.CredentialSHA256) || (o.ExpectedProxyID != nil && *o.ExpectedProxyID <= 0) || o.ExpectedPolicy == nil || len(o.ExpectedConfig) != len(gatewayBorrowInitialConfigKeys) {
		return invalid()
	}
	for key := range o.ExpectedPolicy {
		if !slices.Contains(GatewayBorrowPolicyKeys, key) {
			return invalid()
		}
	}
	for _, key := range gatewayBorrowInitialConfigKeys {
		if _, ok := o.ExpectedConfig[key]; !ok {
			return invalid()
		}
	}
	// Normalize JSON numbers so the Go and HTTP entry points have one contract.
	b, err := json.Marshal(o.ExpectedConfig)
	if err != nil {
		return invalid()
	}
	var c map[string]any
	if json.Unmarshal(b, &c) != nil {
		return invalid()
	}
	integer := func(value any, minimum, maximum int) bool {
		n, ok := value.(float64)
		return ok && n >= float64(minimum) && n <= float64(maximum) && n == float64(int64(n))
	}
	if !integer(c["concurrency"], 1, 10000) || !integer(c["priority"], 0, 1000000) || (c["load_factor"] != nil && !integer(c["load_factor"], 1, 10000)) {
		return invalid()
	}
	groups, ok := c["group_ids"].([]any)
	if !ok {
		return invalid()
	}
	var previous float64
	for _, v := range groups {
		n, isNumber := v.(float64)
		if !isNumber || !integer(n, 1, 2147483647) || n <= previous {
			return invalid()
		}
		previous = n
	}
	if c["model_mapping"] != nil {
		mapping, ok := c["model_mapping"].(map[string]any)
		if !ok {
			return invalid()
		}
		for _, v := range mapping {
			if _, ok := v.(string); !ok {
				return invalid()
			}
		}
	}
	o.ExpectedConfig = c
	o.ExpectedPolicy = maps.Clone(o.ExpectedPolicy)
	if now.Sub(o.ObservedAt) > 10*time.Minute {
		return result, nil
	}
	if s == nil || s.accountRepo == nil {
		return result, infraerrors.New(http.StatusServiceUnavailable, "GATEWAY_BORROW_INITIAL_READY_UNAVAILABLE", "Initial defaults persistence unavailable")
	}
	repo, ok := s.accountRepo.(GatewayBorrowInitialReadyRepository)
	if !ok {
		return result, infraerrors.New(http.StatusServiceUnavailable, "GATEWAY_BORROW_INITIAL_READY_UNAVAILABLE", "Initial defaults persistence unavailable")
	}
	stateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return repo.MarkGatewayBorrowInitialReadyIfObserved(stateCtx, id, o)
}
