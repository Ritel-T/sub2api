package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSGlobalDisablePreservesProtectedRouting(t *testing.T) {
	ctx := context.Background()
	repo := &excelBPSImageSettingsRepo{}
	settings := NewSettingService(repo, &config.Config{})
	require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{ExcelBPSEnabled: false}))
	upstream := &httpUpstreamRecorder{}
	gateway := openAIClientToolsTestService(upstream)
	gateway.settingService = settings
	account := isolatedExcelAccount()
	account.Extra["openai_passthrough"] = false
	c, _ := isolationForwardContext(16, "/v1/responses")
	_, err := gateway.Forward(ctx, c, account, []byte(`{"model":"gpt-6-astra","input":"test","stream":true}`))
	require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
	require.Contains(t, err.Error(), "basispoints_globally_disabled")
	require.Empty(t, upstream.requests, "global disable must not send a protected request natively")
	require.Equal(t, "basispoints_required_use_responses", gateway.controlledRouteReason(ctx, account, "gpt-6-astra", "native_http"))
	require.Equal(t, "basispoints_required_use_responses", gateway.controlledRouteReason(ctx, account, "gpt-6-astra", "native_ws"))
}

func TestControlledExperimentMatchesBPSBeforePrism(t *testing.T) {
	ctx := context.Background()
	gateway := &OpenAIGatewayService{cfg: &config.Config{}}
	gateway.cfg.Gateway.PrismBrowser.Enabled = true
	account := isolatedExcelAccount()
	account.Extra["openai_passthrough"] = false
	account.Extra["openai_prism_browser"] = true
	require.True(t, account.IsPrismBrowserEnabledForModel("gpt-6.1-sol"))
	require.Empty(t, gateway.controlledRouteReason(ctx, account, "gpt-6.1-sol", "bps"))
	require.Equal(t, "prism_not_enabled_for_account_model", gateway.controlledRouteReason(ctx, account, "gpt-6.1-sol", "prism"))
}
