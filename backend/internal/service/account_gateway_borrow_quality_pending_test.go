package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"
)

func pendingAccountFixture() *Account {
	a := &Account{ID: 112, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 3, Credentials: map[string]any{"access_token": "pending-test-token", "model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6.1-sol": "gpt-6.1-sol", "gpt-6-sol": "gpt-6.1-sol", "gpt-6-luna": "gpt-6-luna"}}}
	prepareGatewayBorrowAccountQualityForCreate(a)
	return a
}

func TestInitialQualityPendingOnlyBlocksLinkedBusinessModels(t *testing.T) {
	a := pendingAccountFixture()
	for _, m := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"} {
		require.True(t, a.IsModelSupported(m), "native quality probes retain permissions")
		require.True(t, a.IsOpenAIGatewayAccountQualityPendingForModel(m), m)
		require.Equal(t, "gateway_borrow_initial_quality_pending", gatewayBorrowEligibilityReason(context.Background(), a, m, false))
	}
	require.False(t, a.IsOpenAIGatewayAccountQualityPendingForModel("gpt-6-luna"))
	require.True(t, a.Schedulable, "no whole-account scheduling mutation")
	a.Extra[GatewayBorrowAccountQualityPendingKey] = false
	require.False(t, a.IsOpenAIGatewayAccountQualityPendingForModel("gpt-6.1-sol"))
}

func TestInitialQualityPendingMappingAndEligibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mapping   map[string]any
		requested string
		pending   bool
	}{
		{"alias to Astra", map[string]any{"quality": "gpt-6-astra", "public-sol": "gpt-6.1-sol"}, "public-sol", true},
		{"legacy Sol", map[string]any{"quality": "gpt-6-astra", "old": "gpt-6-sol"}, "old", true},
		{"Sol name maps to Luna", map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6-luna"}, "gpt-6-sol", false},
		{"no Astra", map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}, "gpt-6.1-sol", false},
		{"Astra name maps to Luna", map[string]any{"gpt-6-astra": "gpt-6-luna", "gpt-6.1-sol": "gpt-6.1-sol"}, "gpt-6.1-sol", false},
		{"empty mapping", map[string]any{}, "gpt-6.1-sol", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := pendingAccountFixture()
			a.Credentials["model_mapping"] = tc.mapping
			require.Equal(t, tc.pending, a.IsOpenAIGatewayAccountQualityPendingForModel(tc.requested))
		})
	}
	a := pendingAccountFixture()
	a.Credentials["model_mapping"] = map[string]any{"public-sol": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6-luna", "gpt-6-astra": "gpt-6-astra"}
	require.True(t, a.IsOpenAIGatewayAccountQualityPendingForModel("public-sol"))
	require.True(t, a.IsOpenAIGatewayAccountQualityPendingForUpstreamModel("gpt-6.1-sol"), "already mapped model must not be mapped again")
	require.False(t, (*Account)(nil).IsOpenAIGatewayAccountQualityPendingForModel("gpt-6-astra"))
}

func TestInitialQualityCreateRejectsImportedCompletion(t *testing.T) {
	original := map[string]any{GatewayBorrowAccountQualityModeKey: "legacy", GatewayBorrowAccountQualityPendingKey: false, GatewayBorrowInitialReadyKey: true, "quality_candy": map[string]any{"state": "healthy"}, GatewayBorrowQualityKey: map[string]any{"gpt-6-astra": true}, "custom": true}
	a, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, original)
	require.NoError(t, err)
	require.Equal(t, GatewayBorrowAccountQualityMode, a.Extra[GatewayBorrowAccountQualityModeKey])
	require.Equal(t, true, a.Extra[GatewayBorrowAccountQualityPendingKey])
	require.Equal(t, false, a.Extra[GatewayBorrowInitialReadyKey])
	require.NotContains(t, a.Extra, "quality_candy")
	require.NotContains(t, a.Extra, GatewayBorrowQualityKey)
	require.Equal(t, true, a.Extra["custom"])
	require.Equal(t, false, original[GatewayBorrowAccountQualityPendingKey], "input observation is not mutated")
	for _, tc := range []struct{ platform, kind string }{{PlatformOpenAI, AccountTypeAPIKey}, {PlatformAnthropic, AccountTypeOAuth}} {
		a, err = buildAccountForCreate(&CreateAccountInput{Platform: tc.platform, Type: tc.kind}, map[string]any{})
		require.NoError(t, err)
		require.NotContains(t, a.Extra, GatewayBorrowAccountQualityPendingKey)
	}
}

func TestInitialQualityOrdinaryEditPreservesOwnedState(t *testing.T) {
	a := pendingAccountFixture()
	a.Extra[GatewayBorrowModelsKey] = []string{"gpt-6-astra", "gpt-6.1-sol"}
	a.Extra["quality_candy"] = map[string]any{"state": "degraded"}
	incoming := map[string]any{GatewayBorrowAccountQualityModeKey: "old", GatewayBorrowAccountQualityPendingKey: false, GatewayBorrowInitialReadyKey: true, GatewayBorrowModelsKey: []string{}, "quality_candy": map[string]any{"state": "healthy"}, "openai_excel_bps": true, "custom": "changed"}
	got := MergeOpenAIGatewayAccountQualityExtra(incoming, a.Extra)
	for _, key := range []string{GatewayBorrowAccountQualityModeKey, GatewayBorrowAccountQualityPendingKey, GatewayBorrowInitialReadyKey, GatewayBorrowModelsKey, "quality_candy"} {
		require.Equal(t, a.Extra[key], got[key], key)
	}
	require.Equal(t, true, got["openai_excel_bps"], "independent manual BPS configuration remains editable")
	require.Equal(t, "changed", got["custom"])
	require.Equal(t, false, incoming[GatewayBorrowAccountQualityPendingKey], "caller map stays unchanged")
	legacy := MergeOpenAIGatewayAccountQualityExtra(incoming, map[string]any{})
	require.NotContains(t, legacy, GatewayBorrowAccountQualityModeKey)
	require.NotContains(t, legacy, GatewayBorrowAccountQualityPendingKey)
	require.NotContains(t, legacy, GatewayBorrowInitialReadyKey)
}

func TestInitialQualityPendingHTTPAndWSTurnAdmission(t *testing.T) {
	a := pendingAccountFixture()
	repo := &turnAdmissionRepo{account: a}
	s := &OpenAIGatewayService{accountRepo: repo}
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"} {
		_, err := s.admitOpenAITurnForGroup(context.Background(), 0, false, a, model)
		requireInitialPendingAdmissionError(t, err)
		req, err := http.NewRequestWithContext(context.Background(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"`+model+`"}`))
		require.NoError(t, err)
		_, err = s.admitOpenAIHTTPRequest(req, a)
		requireInitialPendingAdmissionError(t, err)
		err = s.checkOpenAIWSBinding(a, model, &openAIWSTurnBinding{model: model, fingerprint: openAITurnRouteFingerprint(a), createdAt: time.Now()})
		requireInitialPendingAdmissionError(t, err)
	}
	latest, err := s.admitOpenAITurnForGroup(context.Background(), 0, false, a, "gpt-6-luna")
	require.NoError(t, err)
	require.Same(t, a, latest)
}

func TestInitialQualityPendingSelectionToFinalSendRace(t *testing.T) {
	latest := pendingAccountFixture()
	selected := *latest
	selected.Extra = maps.Clone(latest.Extra)
	selected.Extra[GatewayBorrowAccountQualityPendingKey] = false
	s := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: latest}}
	_, err := s.admitOpenAITurn(context.Background(), nil, &selected, "gpt-6-astra")
	requireInitialPendingAdmissionError(t, err)
}

func requireInitialPendingAdmissionError(t *testing.T, err error) {
	t.Helper()
	var admission *OpenAITurnAdmissionError
	require.ErrorAs(t, err, &admission)
	require.Equal(t, "gateway_borrow_initial_quality_pending", admission.Reason)
}

func TestInitialQualityClassificationDoesNotInvalidateUnrelatedModelBindings(t *testing.T) {
	a := pendingAccountFixture()
	s := &OpenAIGatewayService{}
	binding := &openAIWSTurnBinding{model: "gpt-6-luna", fingerprint: openAITurnRouteFingerprint(a), createdAt: time.Now()}
	require.NoError(t, s.checkOpenAIWSBinding(a, "gpt-6-luna", binding))
	fingerprint := openAITurnRouteFingerprint(a)
	a.Extra[GatewayBorrowAccountQualityPendingKey] = false
	require.Equal(t, fingerprint, openAITurnRouteFingerprint(a), "quality gate is checked separately from physical route bindings")
	require.NoError(t, s.checkOpenAIWSBinding(a, "gpt-6-luna", binding))
}
