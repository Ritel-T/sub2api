//go:build unit

package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransformGeminiToClaudeWithOverride_FirstTurn(t *testing.T) {
	resp := map[string]any{
		"response": map[string]any{
			"candidates": []any{map[string]any{
				"content": map[string]any{
					"parts": []any{map[string]any{"text": "hello"}},
				},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{
				"promptTokenCount":        100,
				"cachedContentTokenCount": 0,
				"candidatesTokenCount":    12,
			},
		},
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)

	_, usage, err := TransformGeminiToClaudeWithOverride(b, "claude-sonnet-4-5", &SimCacheOverride{IsFirstTurn: true})
	require.NoError(t, err)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 0, usage.CacheReadInputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 12, usage.OutputTokens)
}

func TestTransformGeminiToClaudeWithOverride_Hit(t *testing.T) {
	resp := map[string]any{
		"response": map[string]any{
			"candidates": []any{map[string]any{
				"content": map[string]any{
					"parts": []any{map[string]any{"text": "hello"}},
				},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{
				"promptTokenCount":        100,
				"cachedContentTokenCount": 0,
				"candidatesTokenCount":    12,
			},
		},
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)

	_, usage, err := TransformGeminiToClaudeWithOverride(b, "claude-sonnet-4-5", &SimCacheOverride{HistoryCachedTokenCount: 80})
	require.NoError(t, err)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 80, usage.CacheReadInputTokens)
	require.Equal(t, 20, usage.CacheCreationInputTokens)
	require.Equal(t, 12, usage.OutputTokens)
}

func TestStreamingProcessorOverride(t *testing.T) {
	processor := NewStreamingProcessor("claude-sonnet-4-5", &SimCacheOverride{HistoryCachedTokenCount: 70})
	line := `data: {"response":{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":0,"candidatesTokenCount":5}}}`
	out := processor.ProcessLine(line)
	require.NotEmpty(t, out)
	_, usage := processor.Finish()
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 70, usage.CacheReadInputTokens)
	require.Equal(t, 30, usage.CacheCreationInputTokens)
	require.Equal(t, 5, usage.OutputTokens)
}
