package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGPT61SolIdentityAndCatalog(t *testing.T) {
	for _, id := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol", "OPENAI/GPT-6.1_SOL", "gpt-6.1-sol-high", "gpt-6.1-sol-max", "gpt-6.1-sol-2026-09-29", "gpt-6.1-sol-openai-compact"} {
		require.True(t, openai.IsGPT61SolModelSpelling(id), id)
		require.False(t, openai.IsGPT6SolOrLunaModelSpelling(id), "6.1 must not inherit GPT-6 Sol none support")
		require.Equal(t, "gpt-6.1-sol", normalizeKnownOpenAICodexModel(id))
		require.Equal(t, "gpt-6.1-sol", normalizeCodexModel(id))
		require.True(t, shouldAutoInjectPromptCacheKeyForCompat(id))
	}
	for _, id := range []string{"gpt-6.10-sol", "gpt-6.1-solitude", "gpt-6.1-sol-preview", "gpt-6-sol"} {
		require.False(t, openai.IsGPT61SolModelSpelling(id), id)
	}
	require.Contains(t, openai.DefaultModelIDs(), "gpt-6.1-sol")
	require.Equal(t, "gpt-6-sol", normalizeCodexModel("gpt-6-sol"))
	descriptor := newConfiguredCodexModelDescriptor("gpt-6.1-sol")
	require.Equal(t, "GPT-6.1-Sol", descriptor.DisplayName)
	require.EqualValues(t, 1_050_000, descriptor.ContextWindow)
	require.EqualValues(t, 1_050_000, descriptor.MaxContextWindow)
	var levels []string
	for _, level := range descriptor.SupportedReasoningLevels {
		levels = append(levels, level.Effort)
	}
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, levels)
	require.Len(t, descriptor.ServiceTiers, 1)
	require.Equal(t, "priority", descriptor.ServiceTiers[0].ID)
	account := newCodexModelsAPIKeyTestAccount("https://api.openai.com/v1")
	account.Credentials["model_mapping"] = map[string]any{"public-sol": "gpt-6.1-sol-high"}
	body, err := adjustAPIKeyCodexModelsManifest([]byte(`{"models":[{"slug":"public-sol","use_responses_lite":true}]}`), account)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "models.0.use_responses_lite").Bool())
}

func TestGPT61SolReasoningSamplingCompatibility(t *testing.T) {
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
		t.Run(effort, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6.1-sol","reasoning":{"mode":"standard","effort":"` + effort + `"},"temperature":0.7,"top_p":0.9,"top_logprobs":2,"logprobs":true,"include":["reasoning.encrypted_content","message.output_text.logprobs"],"prompt_cache_options":{"ttl":"30m"}}`)
			out, changed, err := normalizeOpenAIResponsesReasoningMode(body, "")
			require.NoError(t, err)
			require.True(t, changed)
			want := effort
			if effort == "none" || effort == "minimal" {
				want = "low"
			}
			require.Equal(t, want, gjson.GetBytes(out, "reasoning.effort").String())
			require.Equal(t, want, normalizeOpenAIReasoningEffortForModel(effort, "gpt-6.1-sol"))
			require.Equal(t, "standard", gjson.GetBytes(out, "reasoning.mode").String())
			for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
				require.False(t, gjson.GetBytes(out, field).Exists(), field)
			}
			require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(out, "include.0").String())
			require.Len(t, gjson.GetBytes(out, "include").Array(), 1)
			require.Equal(t, "30m", gjson.GetBytes(out, "prompt_cache_options.ttl").String())
		})
	}
	old := []byte(`{"model":"gpt-6-sol","reasoning":{"effort":"none"},"temperature":0.7}`)
	out, changed, err := normalizeGPT6ResponsesSampling(old, "gpt-6-sol")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, old, out)
	require.Equal(t, "none", normalizeOpenAIReasoningEffortForModel("none", "gpt-6-sol"))
	require.Equal(t, "", normalizeOpenAIReasoningEffortForModel("ultra", "gpt-6-sol"))
}

func TestGPT61SolRawChatToolsAlwaysRequireResponses(t *testing.T) {
	for _, effort := range []string{"none", "low", "max"} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"public": "gpt-6.1-sol"}}}
		upstream := &httpUpstreamRecorder{}
		svc := &OpenAIGatewayService{httpUpstream: upstream}
		_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, []byte(`{"model":"public","reasoning_effort":"`+effort+`","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`), "")
		require.ErrorContains(t, err, "requires Responses")
		require.NotContains(t, err.Error(), "Use reasoning_effort=none")
		require.Equal(t, 400, rec.Code)
		require.Empty(t, upstream.requests)
	}
}

func TestGPT61SolRawChatTextNormalizesParameters(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-6.1-sol","reasoning_effort":"none","temperature":0.7,"top_p":0.9,"top_logprobs":2,"logprobs":true,"messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"id":"chat_1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10}}`))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://api.openai.com"}}
	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
	for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
		require.False(t, gjson.GetBytes(upstream.lastBody, field).Exists(), field)
	}
}

func TestGPT61SolAPIKeyResponsesHTTPAndWSNormalize(t *testing.T) {
	body := []byte(`{"model":"public-sol","input":"hello","stream":true,"reasoning":{"effort":"none"},"temperature":0.7,"top_p":0.9,"top_logprobs":2,"logprobs":true,"prompt_cache_retention":"24h","include":["reasoning.encrypted_content","message.output_text.logprobs"]}`)
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"public-sol": "gpt-6.1-sol"}}}
	wsBody, changed, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "low", gjson.GetBytes(wsBody, "reasoning.effort").String())
	require.False(t, gjson.GetBytes(wsBody, "temperature").Exists())
	require.False(t, gjson.GetBytes(wsBody, "prompt_cache_retention").Exists())
	require.Equal(t, "30m", gjson.GetBytes(wsBody, "prompt_cache_options.ttl").String())
	wire := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r61\",\"status\":\"completed\",\"model\":\"gpt-6.1-sol\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewBufferString(wire))}}
	svc := openAIClientToolsTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	_, err = svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
	for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs", "prompt_cache_retention"} {
		require.False(t, gjson.GetBytes(upstream.lastBody, field).Exists(), field)
	}
	require.Equal(t, "30m", gjson.GetBytes(upstream.lastBody, "prompt_cache_options.ttl").String())
	require.Len(t, gjson.GetBytes(upstream.lastBody, "include").Array(), 1)
}

func TestGPT61SolFallbackPricingTiersAndBoundary(t *testing.T) {
	for _, pricingSvc := range []*PricingService{nil, {pricingData: map[string]*LiteLLMModelPricing{}}} {
		svc := NewBillingService(&config.Config{}, pricingSvc)
		for _, id := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol", "gpt-6.1-sol-high", "gpt-6.1-sol-2026-09-29"} {
			price, err := svc.GetModelPricing(id)
			require.NoError(t, err)
			require.InDelta(t, 2e-6, price.InputPricePerToken, 1e-12)
			require.InDelta(t, 0.1e-6, price.CacheReadPricePerToken, 1e-12)
			require.InDelta(t, 2.5e-6, price.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, 10e-6, price.OutputPricePerToken, 1e-12)
		}
		boundary, err := svc.CalculateCost("gpt-6.1-sol", UsageTokens{InputTokens: 100_000, CacheCreationTokens: 100_000, CacheReadTokens: 72_000, OutputTokens: 10}, 1)
		require.NoError(t, err)
		require.False(t, boundary.LongContextBillingApplied)
		require.InDelta(t, 72_000*0.1e-6, boundary.CacheReadCost, 1e-12)
		for _, tier := range []struct {
			name  string
			scale float64
		}{{"", 1}, {"priority", 2}} {
			cost, err := svc.CalculateCostWithServiceTier("gpt-6.1-sol", UsageTokens{InputTokens: 100_000, CacheCreationTokens: 100_000, CacheReadTokens: 73_000, OutputTokens: 10}, 1, tier.name)
			require.NoError(t, err)
			require.True(t, cost.LongContextBillingApplied)
			require.InDelta(t, 100_000*2e-6*tier.scale*2, cost.InputCost, 1e-12)
			require.InDelta(t, 100_000*2.5e-6*tier.scale*2, cost.CacheCreationCost, 1e-12)
			require.InDelta(t, 73_000*0.1e-6*tier.scale*2, cost.CacheReadCost, 1e-12)
			require.InDelta(t, 10*10e-6*tier.scale*1.5, cost.OutputCost, 1e-12)
		}
		oldPrice, err := svc.GetModelPricing("gpt-6-sol")
		require.NoError(t, err)
		require.InDelta(t, 0.2e-6, oldPrice.CacheReadPricePerToken, 1e-12)
	}
}

func TestGPT61SolBPSKeepsExplicitModelAndResponsesOnly(t *testing.T) {
	wire, _, err := basispoints.Prepare([]byte(`{"model":"gpt-6.1-sol","reasoning":{"effort":"low"},"input":"hello"}`), "", nil)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(wire, "model").String())
	require.Equal(t, "explicit", gjson.GetBytes(wire, "model_selection").String())
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		upstream := &httpUpstreamRecorder{}
		svc := openAIClientToolsTestService(upstream)
		c, rec := isolationForwardContext(16, path)
		body := []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"test"}]}`)
		if path == "/v1/messages" {
			_, err = svc.ForwardAsAnthropic(context.Background(), c, isolatedExcelAccount(), body, "", "")
		} else {
			_, err = svc.ForwardAsChatCompletions(context.Background(), c, isolatedExcelAccount(), body, "", "")
		}
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "use /v1/responses")
		require.Empty(t, upstream.requests)
	}
}

func TestGPT61SolBPSProtectedGroupNeverFallsBackToNative(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		group := int64(16)
		accounts := []Account{
			{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{group}, Extra: map[string]any{
				"openai_excel_bps": !disabled, "openai_excel_bps_models": []string{"gpt-6.1-sol"}, ExcelBPSRequiredGroupIDsKey: []int64{group}, ExcelBPSRequiredModelsKey: []string{"gpt-6.1-sol"},
			}},
			{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{group}},
		}
		acquired := []int64{}
		cache := schedulerTestConcurrencyCache{loadMap: map[int64]*AccountLoadInfo{1: {AccountID: 1, LoadRate: 100, CurrentConcurrency: 1}, 2: {AccountID: 2}}, acquireResults: map[int64]bool{2: true}, acquiredIDs: &acquired}
		cfg := &config.Config{}
		cfg.Gateway.OpenAIWS.LBTopK = 1
		svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cfg: cfg, concurrencyService: NewConcurrencyService(cache)}
		scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
		selection, _, err := scheduler.Select(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, GroupID: &group, RequestedModel: "gpt-6.1-sol"})
		if disabled {
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection)
		} else {
			require.NoError(t, err)
			require.Equal(t, int64(1), selection.Account.ID)
			require.NotNil(t, selection.WaitPlan)
		}
		require.NotContains(t, acquired, int64(2))
	}
}
