package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergedOutputFloorPreservesGPT61Reasoning(t *testing.T) {
	t.Run("chat nested effort and configurable floor", func(t *testing.T) {
		var req ChatCompletionsRequest
		require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hi"}],"max_tokens":32,"reasoning_effort":"high","reasoning":{"effort":"none"}}`), &req))
		out, err := ChatCompletionsToResponsesWithMinOutputTokens(&req, 128)
		require.NoError(t, err)
		require.Equal(t, 128, *out.MaxOutputTokens)
		require.Equal(t, "low", out.Reasoning.Effort)
		require.Equal(t, "none", req.Reasoning.Effort)
		require.Equal(t, "high", req.ReasoningEffort)
		out, err = ChatCompletionsToResponsesWithMinOutputTokens(&req, 0)
		require.NoError(t, err)
		require.Equal(t, 32, *out.MaxOutputTokens)
		require.Equal(t, "low", out.Reasoning.Effort)
	})
	t.Run("anthropic thinking disable and configurable floor", func(t *testing.T) {
		var req AnthropicRequest
		require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hi"}],"max_tokens":32,"thinking":{"type":"disabled"},"output_config":{"effort":"max"}}`), &req))
		out, err := AnthropicToResponsesWithMinOutputTokens(&req, 128)
		require.NoError(t, err)
		require.Equal(t, 128, *out.MaxOutputTokens)
		require.Equal(t, "low", out.Reasoning.Effort)
		require.Equal(t, "disabled", req.Thinking.Type)
		require.Equal(t, "max", req.OutputConfig.Effort)
		out, err = AnthropicToResponsesWithMinOutputTokens(&req, 0)
		require.NoError(t, err)
		require.Equal(t, 32, *out.MaxOutputTokens)
		require.Equal(t, "low", out.Reasoning.Effort)
	})
}
