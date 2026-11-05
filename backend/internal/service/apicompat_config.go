package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func configuredMinOutputTokens(cfg *config.Config) int {
	if cfg != nil && cfg.Gateway.MinOutputTokens >= 16 {
		return cfg.Gateway.MinOutputTokens
	}
	return apicompat.DefaultMinMaxOutputTokens
}

func chatCompletionsToResponses(cfg *config.Config, req *apicompat.ChatCompletionsRequest) (*apicompat.ResponsesRequest, error) {
	return apicompat.ChatCompletionsToResponsesWithMinOutputTokens(req, configuredMinOutputTokens(cfg))
}

func anthropicToResponses(cfg *config.Config, account *Account, req *apicompat.AnthropicRequest) (*apicompat.ResponsesRequest, error) {
	// Known OpenAI endpoints can replay their gAAAA ciphertext. Keep the
	// compatible-provider policy for xAI and unverified relays, even when the
	// client uses a GPT alias. This does not assert same-account provenance.
	openAIReasoning := account != nil && account.Platform != PlatformGrok &&
		(account.UsesOpenAICodexProtocol() ||
			(account.IsOpenAIApiKey() && isOfficialOpenAIModelsBaseURL(account.GetOpenAIBaseURL())))
	return apicompat.AnthropicToResponsesWithOptions(req, apicompat.AnthropicToResponsesOptions{
		MinOutputTokens: configuredMinOutputTokens(cfg), OpenAIEncryptedReasoning: openAIReasoning,
	})
}

func settingServiceConfig(settingService *SettingService) *config.Config {
	if settingService == nil {
		return nil
	}
	return settingService.cfg
}
