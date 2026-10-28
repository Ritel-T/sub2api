package service

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"os"
	"testing"
)

func TestGPT61TerraIdentityAndReasoning(t *testing.T) {
	for _, model := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra", "GPT-6.1-TERRA", "gpt-6.1-terra-max", "gpt-6.1-terra-2026-10-28", "gpt-6.1-terra-openai-compact"} {
		require.True(t, openai.IsGPT61TerraModelSpelling(model), model)
		require.Equal(t, "gpt-6.1-terra", normalizeCodexModel(model), model)
		require.Equal(t, "gpt-6.1-terra", NormalizeOpenAICompatRequestedModel(model), model)
	}
	for _, model := range []string{"gpt-6.1-terrain", "gpt-6.1-sol", "gpt-5.6-terra", "gpt-6.1-terra-ultra", "gpt-6.1-terra-preview"} {
		require.False(t, openai.IsGPT61TerraModelSpelling(model), model)
	}
	for _, effort := range []string{"none", "low", "medium", "high", "xhigh", "max"} {
		require.NoError(t, openai.ValidateGPT61TerraReasoningEffort("gpt-6.1-terra", effort))
		require.Equal(t, effort, normalizeOpenAIReasoningEffortForModel(effort, "gpt-6.1-terra"))
	}
	for _, effort := range []string{"minimal", "ultra", "bogus"} {
		require.Error(t, openai.ValidateGPT61TerraReasoningEffort("gpt-6.1-terra", effort))
	}
	req := &apicompat.AnthropicRequest{Model: "gpt-6.1-terra-max"}
	applyOpenAICompatModelNormalization(req)
	require.Equal(t, "gpt-6.1-terra", req.Model)
	require.Equal(t, "max", req.OutputConfig.Effort)
	// Existing independent model contracts must stay unchanged.
	require.Equal(t, "low", normalizeOpenAIReasoningEffortForModel("none", "gpt-6.1-sol"))
	require.Equal(t, "none", normalizeOpenAIReasoningEffortForModel("none", "gpt-6-luna"))
	require.Equal(t, "xhigh", normalizeOpenAIReasoningEffortForModel("max", "gpt-5.4"))
}

func TestGPT61TerraSamplingAndPromptCache(t *testing.T) {
	for _, effort := range []string{"none", "medium", "max"} {
		raw := []byte(`{"model":"gpt-6.1-terra","reasoning":{"effort":"` + effort + `"},"temperature":0.5,"top_p":0.9,"top_logprobs":2,"logprobs":true,"include":["reasoning.encrypted_content","message.output_text.logprobs"],"prompt_cache_retention":"in-memory"}`)
		out, changed, err := normalizeGPT6ResponsesSampling(raw, "gpt-6.1-terra")
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, effort, gjson.GetBytes(out, "reasoning.effort").String())
		require.Equal(t, "30m", gjson.GetBytes(out, "prompt_cache_options.ttl").String())
		require.False(t, gjson.GetBytes(out, "prompt_cache_retention").Exists())
		for _, key := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
			require.Equal(t, effort == "none", gjson.GetBytes(out, key).Exists(), key)
		}
		require.Equal(t, effort == "none", gjson.GetBytes(out, "include").Array()[len(gjson.GetBytes(out, "include").Array())-1].String() == "message.output_text.logprobs")
	}
	_, _, err := normalizeGPT6ResponsesSampling([]byte(`{"reasoning":{"effort":"ultra"}}`), "gpt-6.1-terra")
	require.Error(t, err)
	out, _, err := normalizeGPT6ResponsesSampling([]byte(`{"prompt_cache_options":{"ttl":"12h"},"prompt_cache_retention":"24h","reasoning":{"effort":"none"}}`), "gpt-6.1-terra")
	require.NoError(t, err)
	require.Equal(t, "12h", gjson.GetBytes(out, "prompt_cache_options.ttl").String())
}

func TestGPT61TerraCodexCatalog(t *testing.T) {
	body, err := BuildCodexModelsManifest([]string{"gpt-6.1-terra"})
	require.NoError(t, err)
	m := decodeCodexManifestModels(t, body)[0]
	require.Equal(t, "gpt-6.1-terra", m["slug"])
	require.Equal(t, "medium", m["default_reasoning_level"])
	require.Equal(t, []string{"none", "low", "medium", "high", "xhigh", "max"}, effortsFromManifestModel(t, m))
	require.Equal(t, []any{"text", "image"}, m["input_modalities"])
	require.EqualValues(t, 1050000, m["context_window"])
	require.EqualValues(t, 1050000, m["max_context_window"])
	require.EqualValues(t, 128000, m["max_output_tokens"])
	require.True(t, m["supports_parallel_tool_calls"].(bool))
	require.Nil(t, m["multi_agent_version"])
	require.Contains(t, m["service_tiers"], map[string]any{"id": "priority", "name": "Fast", "description": "Priority processing for lower latency."})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://api.openai.com"}}
	body, err = completeAPIKeyCodexModelsManifestMetadata([]byte(`{"models":[{"slug":"gpt-6.1-terra","use_responses_lite":true}]}`), true, account)
	require.NoError(t, err)
	body, err = adjustAPIKeyCodexModelsManifest(body, account)
	require.NoError(t, err)
	require.Equal(t, false, decodeCodexManifestModels(t, body)[0]["use_responses_lite"])
}

func TestGPT61TerraPricingStandardFastAndLongContext(t *testing.T) {
	data, err := os.ReadFile("../../resources/model-pricing/model_prices_and_context_window.json")
	require.NoError(t, err)
	var card map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &card))
	require.Contains(t, card, "gpt-6.1-terra")
	catalog := &PricingService{}
	catalog.pricingData, err = catalog.parsePricingData(data)
	require.NoError(t, err)
	sources := map[string]*BillingService{
		"billing fallback": NewBillingService(&config.Config{}, nil),
		"pricing fallback": NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}),
		"catalog":          NewBillingService(&config.Config{}, catalog),
	}
	for source, svc := range sources {
		t.Run(source, func(t *testing.T) {
			for _, model := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max"} {
				for _, n := range []int{271999, 272000, 272001} {
					for tier, mult := range map[string]float64{"": 1, "fast": 2, "priority": 2} {
						im, om := 1.0, 1.0
						if n > 272000 {
							im = 2
							om = 1.5
						}
						tokens := UsageTokens{InputTokens: n - 3000, OutputTokens: 500, CacheReadTokens: 2000, CacheCreationTokens: 1000}
						cost, err := svc.CalculateCostWithServiceTier(model, tokens, 1, tier)
						require.NoError(t, err)
						require.InDelta(t, float64(n-3000)*1e-6*im*mult, cost.InputCost, 1e-10)
						require.InDelta(t, 500*5e-6*om*mult, cost.OutputCost, 1e-10)
						require.InDelta(t, 2000*.05e-6*im*mult, cost.CacheReadCost, 1e-10)
						require.InDelta(t, 1000*1.25e-6*im*mult, cost.CacheCreationCost, 1e-10)
						require.Equal(t, n > 272000, cost.LongContextBillingApplied)
						tokens.CacheCreation5mTokens = 400
						tokens.CacheCreation1hTokens = 600
						cost, err = svc.CalculateCostWithServiceTier(model, tokens, 1, tier)
						require.NoError(t, err)
						require.InDelta(t, (400*1.25e-6+600*2.5e-6)*im*mult, cost.CacheCreationCost, 1e-10)
					}
				}
			}
		})
	}
}

func TestGPT61TerraDoesNotInheritSolLongContextExemption(t *testing.T) {
	for _, typ := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		account := &Account{Platform: PlatformOpenAI, Type: typ}
		require.False(t, openAIGPT61SolLongContextExempt(account, &OpenAIForwardResult{Model: "gpt-6.1-terra", UpstreamModel: "gpt-6.1-terra"}))
	}
}
