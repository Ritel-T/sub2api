package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataKeepsIdentityBoundAccountQualityWithoutTokens(t *testing.T) {
	now := time.Now().UTC()
	proxy := int64(17)
	a := service.Account{ID: 17, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ProxyID: &proxy, Credentials: map[string]any{"access_token": "private-access-token-fixture", "chatgpt_account_id": "private-chatgpt-identity-fixture", "model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6.1-sol": "gpt-6.1-sol"}}, Extra: map[string]any{}}
	a.Extra = map[string]any{
		service.GatewayBorrowAccountQualityModeKey:    service.GatewayBorrowAccountQualityMode,
		service.GatewayBorrowAccountQualityPendingKey: false,
		service.GatewayBorrowInitialReadyKey:          true,
		service.GatewayBorrowModelsKey:                []string{},
		"quality_candy": map[string]any{
			"version": 1, "state": "healthy", "model": "gpt-6-astra", "reasoning_effort": "medium", "expected_answer": "21", "correct": 3, "total": 4,
			"checked_at": now.Add(-time.Minute).Format(time.RFC3339Nano), "run_id": "20261007T190000Z-projection", "algorithm": service.GatewayBorrowCandyAlgorithm, "prompt_sha256": service.GatewayBorrowCandyPromptSHA256,
			"latest_probe_at": now.Add(-time.Minute).Format(time.RFC3339Nano), "latest_probe_credential_sha256": service.GatewayBorrowCredentialSHA256(a.Credentials), "latest_probe_proxy_id": proxy,
		},
		"unrelated_private_extra": "private-ignore-fixture",
	}
	projected := buildSchedulerMetadataAccount(a)
	require.Equal(t, service.GatewayBorrowCredentialSHA256(a.Credentials), projected.SchedulerCredentialSHA256)
	require.Equal(t, &proxy, projected.ProxyID)
	require.NotContains(t, projected.Credentials, "access_token")
	require.NotContains(t, projected.Credentials, "chatgpt_account_id")
	require.NotContains(t, projected.Extra, "unrelated_private_extra")
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-access-token-fixture")
	require.NotContains(t, string(encoded), "private-chatgpt-identity-fixture")
	var restored service.Account
	require.NoError(t, json.Unmarshal(encoded, &restored))
	quality, valid := service.GatewayBorrowAccountQuality(&restored, now, 24*time.Hour)
	require.True(t, valid, "real Redis round trip retains validated classification")
	require.Equal(t, "healthy", quality.State)
	require.Equal(t, 3, quality.Correct)
	// An incoming cache-like field cannot override the current raw DB identity.
	a.SchedulerCredentialSHA256 = projected.SchedulerCredentialSHA256
	a.Credentials["access_token"] = "changed-identity-fixture"
	projected = buildSchedulerMetadataAccount(a)
	_, valid = service.GatewayBorrowAccountQuality(&projected, now, 24*time.Hour)
	require.False(t, valid)
	projected.SchedulerCredentialSHA256 = "invalid"
	_, valid = service.GatewayBorrowAccountQuality(&projected, now, 24*time.Hour)
	require.False(t, valid)
}

func TestSchedulerMetadataKeepsPendingAdmissionGate(t *testing.T) {
	a := service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6.1-sol": "gpt-6.1-sol", "gpt-6-luna": "gpt-6-luna"}}, Extra: map[string]any{service.GatewayBorrowAccountQualityModeKey: service.GatewayBorrowAccountQualityMode, service.GatewayBorrowAccountQualityPendingKey: true, service.GatewayBorrowInitialReadyKey: false}}
	projected := buildSchedulerMetadataAccount(a)
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"} {
		require.True(t, projected.IsOpenAIGatewayAccountQualityPendingForModel(model), model)
	}
	require.False(t, projected.IsOpenAIGatewayAccountQualityPendingForModel("gpt-6-luna"))
}
