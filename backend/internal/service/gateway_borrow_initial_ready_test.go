package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"maps"
	"strings"
	"testing"
	"time"
)

type initialReadyRepositoryStub struct {
	AccountRepository
	calls    int
	observed GatewayBorrowInitialReadyObservation
}

func (r *initialReadyRepositoryStub) MarkGatewayBorrowInitialReadyIfObserved(_ context.Context, _ int64, o GatewayBorrowInitialReadyObservation) (GatewayBorrowPolicyResult, error) {
	r.calls++
	r.observed = o
	return GatewayBorrowPolicyResult{Applied: true, Reason: "initial_defaults_ready"}, nil
}
func initialReadyObservationFixture() GatewayBorrowInitialReadyObservation {
	return GatewayBorrowInitialReadyObservation{ObservedAt: time.Now().UTC(), CredentialSHA256: strings.Repeat("a", 64), ExpectedPolicy: map[string]any{GatewayBorrowAccountQualityModeKey: GatewayBorrowAccountQualityMode, GatewayBorrowAccountQualityPendingKey: true}, ExpectedConfig: map[string]any{"concurrency": 3, "load_factor": nil, "priority": 4, "group_ids": []int64{1, 9}, "model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}}}
}
func TestGatewayBorrowInitialReadyValidatesNarrowObservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*GatewayBorrowInitialReadyObservation)
		code   int
		calls  int
	}{
		{"valid", func(*GatewayBorrowInitialReadyObservation) {}, 0, 1},
		{"missing config field", func(o *GatewayBorrowInitialReadyObservation) { delete(o.ExpectedConfig, "load_factor") }, 400, 0},
		{"unknown config field", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedConfig["cost"] = 0 }, 400, 0},
		{"unsorted bindings", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedConfig["group_ids"] = []int64{9, 1} }, 400, 0},
		{"duplicate bindings", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedConfig["group_ids"] = []int64{1, 1} }, 400, 0},
		{"null bindings", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedConfig["group_ids"] = nil }, 400, 0},
		{"zero concurrency", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedConfig["concurrency"] = 0 }, 400, 0},
		{"fraction priority", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedConfig["priority"] = 1.5 }, 400, 0},
		{"bad mapping", func(o *GatewayBorrowInitialReadyObservation) {
			o.ExpectedConfig["model_mapping"] = map[string]any{"model": true}
		}, 400, 0},
		{"unknown policy", func(o *GatewayBorrowInitialReadyObservation) { o.ExpectedPolicy["quota_used"] = 0 }, 400, 0},
		{"bad identity", func(o *GatewayBorrowInitialReadyObservation) { o.CredentialSHA256 = "token" }, 400, 0},
		{"future", func(o *GatewayBorrowInitialReadyObservation) { o.ObservedAt = time.Now().UTC().Add(time.Minute) }, 400, 0},
		{"stale", func(o *GatewayBorrowInitialReadyObservation) { o.ObservedAt = time.Now().UTC().Add(-11 * time.Minute) }, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &initialReadyRepositoryStub{}
			s := NewRateLimitService(repo, nil, nil, nil, nil)
			o := initialReadyObservationFixture()
			tc.change(&o)
			result, err := s.MarkGatewayBorrowInitialReady(context.Background(), 1, o)
			if tc.code != 0 {
				require.Equal(t, tc.code, infraerrors.Code(err))
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.calls == 1, result.Applied)
			}
			require.Equal(t, tc.calls, repo.calls)
		})
	}
}

func TestGatewayBorrowInitialReadyFingerprintBindsDefaultsAndIdentity(t *testing.T) {
	a := pendingAccountFixture()
	load := 3
	a.LoadFactor = &load
	a.Priority = 4
	a.GroupIDs = []int64{9, 1}
	a.Extra[GatewayBorrowInitialReadyKey] = true
	a.Extra[GatewayBorrowInitialReadyFingerprintKey] = GatewayBorrowInitialReadyFingerprint(a)
	require.True(t, GatewayBorrowInitialReadyMatchesCurrent(a))
	for _, field := range []string{"proxy", "token", "mapping", "concurrency", "load", "priority", "groups"} {
		t.Run(field, func(t *testing.T) {
			changed := *a
			changed.Credentials = maps.Clone(a.Credentials)
			switch field {
			case "proxy":
				id := int64(22)
				changed.ProxyID = &id
			case "token":
				changed.Credentials["access_token"] = "changed-token"
			case "mapping":
				changed.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra"}
			case "concurrency":
				changed.Concurrency++
			case "load":
				v := 4
				changed.LoadFactor = &v
			case "priority":
				changed.Priority++
			case "groups":
				changed.GroupIDs = []int64{1, 10}
			}
			require.False(t, GatewayBorrowInitialReadyMatchesCurrent(&changed), field)
		})
	}
	a.GroupIDs = []int64{1, 9, 1}
	require.True(t, GatewayBorrowInitialReadyMatchesCurrent(a), "binding order and duplicate projection are irrelevant")
	a.Extra["codex_7d_used_percent"] = 99
	require.True(t, GatewayBorrowInitialReadyMatchesCurrent(a), "quota observations do not alter defaults")
}
