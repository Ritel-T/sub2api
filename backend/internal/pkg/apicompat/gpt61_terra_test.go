package apicompat

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGPT61TerraBridgeKeepsNoneAndMax(t *testing.T) {
	sampling := 0.5
	for _, effort := range []string{"none", "low", "medium", "high", "xhigh", "max"} {
		req := &ChatCompletionsRequest{Model: "gpt-6.1-terra", ReasoningEffort: effort, Temperature: &sampling, TopP: &sampling, Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
		out, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		require.Equal(t, effort, out.Reasoning.Effort)
		require.Equal(t, effort == "none", out.Temperature != nil)
		require.Equal(t, effort == "none", out.TopP != nil)
	}
	for _, disabled := range []bool{false, true} {
		var req AnthropicRequest
		thinking := "enabled"
		if disabled {
			thinking = "disabled"
		}
		require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6.1-terra","messages":[{"role":"user","content":"hi"}],"max_tokens":1024,"temperature":0.5,"thinking":{"type":"`+thinking+`"},"output_config":{"effort":"max"}}`), &req))
		out, err := AnthropicToResponses(&req)
		require.NoError(t, err)
		want := "max"
		if disabled {
			want = "none"
		}
		require.Equal(t, want, out.Reasoning.Effort)
		require.Equal(t, disabled, out.Temperature != nil)
	}
	_, err := ChatCompletionsToResponses(&ChatCompletionsRequest{Model: "gpt-6.1-terra", ReasoningEffort: "ultra"})
	require.Error(t, err)
}
