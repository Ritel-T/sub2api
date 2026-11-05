package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestGPTImage26CatalogAndRestrictedDiscovery(t *testing.T) {
	ids := openai.DefaultModelIDs()
	require.Contains(t, ids, "gpt-image-2.6-flare")
	require.Contains(t, ids, "gpt-image-2.6-sunburst")
	require.NotContains(t, ids, "gpt-image-2.6")
	_, _ = newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"gpt-6-astra"}]}`)
	svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{}}
	account := newCodexModelsTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-image-2.6-flare": "gpt-image-2.6-flare"}
	models, err := svc.FetchOpenAIAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "gpt-image-2.6-flare", models[0].ID)
	require.Equal(t, "GPT Image 2.6 Flare", models[0].DisplayName)
	account.Credentials["model_mapping"] = map[string]any{"gpt-image-2": "gpt-image-2"}
	models, err = svc.FetchOpenAIAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "gpt-image-2", models[0].ID, "catalog additions must not expand a configured allowlist")
}

func TestGPTImage26OfficialPricingAndExistingOverrides(t *testing.T) {
	legacy := &LiteLLMModelPricing{InputCostPerToken: 2.5e-6, OutputCostPerImageToken: 15e-6}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"gpt-image-2": legacy}}
	for _, model := range []string{"gpt-image-2.6-flare", "gpt-image-2.6-sunburst"} {
		t.Run(model, func(t *testing.T) {
			p := svc.GetModelPricing(model)
			require.Equal(t, 5e-6, p.InputCostPerToken)
			require.Equal(t, 1.25e-6, p.CacheReadInputTokenCost)
			require.Equal(t, 8e-6, p.InputCostPerImageToken)
			require.Equal(t, 2e-6, p.CacheReadInputImageTokenCost)
			require.Equal(t, 30e-6, p.OutputCostPerImageToken)
			billing := &BillingService{pricingService: svc}
			price, err := billing.GetModelPricing(model)
			require.NoError(t, err)
			// Bill cached text/image separately, then preserve the existing caller multiplier.
			cost := billing.computeTokenBreakdown(price, UsageTokens{InputTokens: 50, ImageInputTokens: 40, CacheReadTokens: 50, ImageCacheReadTokens: 40, OutputTokens: 200, ImageOutputTokens: 200}, 0.6, "", false)
			require.InDelta(t, 10*5e-6, cost.InputCost, 1e-12)
			require.InDelta(t, 40*8e-6, cost.ImageInputCost, 1e-12)
			require.InDelta(t, 10*1.25e-6+40*2e-6, cost.CacheReadCost, 1e-12)
			require.InDelta(t, 200*30e-6, cost.ImageOutputCost, 1e-12)
			require.InDelta(t, (10*5e-6+40*8e-6+10*1.25e-6+40*2e-6+200*30e-6)*0.6, cost.ActualCost, 1e-12)
			custom := &LiteLLMModelPricing{InputCostPerToken: 7e-6}
			svc.pricingData[model] = custom
			require.Same(t, custom, svc.GetModelPricing(model))
		})
	}
	require.Same(t, legacy, svc.GetModelPricing("gpt-image-2"))
}
