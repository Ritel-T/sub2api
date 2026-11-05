package apicompat

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnthropicResponsesReasoningReplayRoundTrip(t *testing.T) {
	for _, openAI := range []bool{true, false} {
		t.Run(fmt.Sprintf("openai=%t", openAI), func(t *testing.T) {
			opts := AnthropicToResponsesOptions{MinOutputTokens: 16, OpenAIEncryptedReasoning: openAI}
			prefix := "provider-"
			if openAI {
				prefix = "gAAAA-"
			}
			req := &AnthropicRequest{Model: "gpt-6-astra", MaxTokens: 32,
				Messages: []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"Inspect then verify"`)}}}
			wantTypes := []string{"message"}
			var wantCiphers, wantCalls []string
			for turn := 1; turn <= 3; turn++ {
				cipher := fmt.Sprintf("%sopaque-%d", prefix, turn)
				secondCipher := cipher + "-after-text"
				output := []ResponsesOutput{
					{Type: "reasoning", EncryptedContent: cipher, Summary: []ResponsesSummary{{Type: "summary_text", Text: "Public summary"}}},
					{Type: "message", Role: "assistant", Content: []ResponsesContentPart{{Type: "output_text", Text: "Progress"}, {Type: "output_text", Text: "Next step"}}},
					{Type: "reasoning", EncryptedContent: secondCipher}, // signature only, after text
				}
				wantTypes = append(wantTypes, "reasoning", "message", "reasoning")
				wantCiphers = append(wantCiphers, cipher, secondCipher)
				callID := fmt.Sprintf("call_%d", turn)
				if turn < 3 {
					output = append(output, ResponsesOutput{Type: "function_call", CallID: callID, Name: "inspect", Arguments: `{"path":"fixture"}`})
					wantTypes = append(wantTypes, "function_call", "function_call_output")
					wantCalls = append(wantCalls, callID)
				} else {
					wantTypes = append(wantTypes, "message")
				}
				visible := ResponsesToAnthropic(&ResponsesResponse{ID: fmt.Sprintf("resp_%d", turn), Status: "completed", Output: output}, req.Model)
				content, err := json.Marshal(visible.Content)
				require.NoError(t, err)
				req.Messages = append(req.Messages, AnthropicMessage{Role: "assistant", Content: content})
				if turn < 3 {
					result, err := json.Marshal([]AnthropicContentBlock{{Type: "tool_result", ToolUseID: callID, Content: json.RawMessage(`"Observed fixture"`)}})
					require.NoError(t, err)
					req.Messages = append(req.Messages, AnthropicMessage{Role: "user", Content: result})
				} else {
					req.Messages = append(req.Messages, AnthropicMessage{Role: "user", Content: json.RawMessage(`"Continue"`)})
				}
				before, err := json.Marshal(req)
				require.NoError(t, err)
				replayed, err := AnthropicToResponsesWithOptions(req, opts)
				require.NoError(t, err)
				after, err := json.Marshal(req)
				require.NoError(t, err)
				require.Equal(t, before, after, "conversion must not mutate the client's history")
				var items []ResponsesInputItem
				require.NoError(t, json.Unmarshal(replayed.Input, &items))
				var types, ciphers, calls, results []string
				for _, item := range items {
					types = append(types, item.Type)
					switch item.Type {
					case "message":
						if item.Role == "assistant" {
							var parts []ResponsesContentPart
							require.NoError(t, json.Unmarshal(item.Content, &parts))
							require.Equal(t, []ResponsesContentPart{{Type: "output_text", Text: "Progress\n\nNext step"}}, parts)
						}
					case "reasoning":
						ciphers = append(ciphers, item.EncryptedContent)
					case "function_call":
						calls = append(calls, item.CallID)
						require.JSONEq(t, `{"path":"fixture"}`, item.Arguments)
					case "function_call_output":
						results = append(results, item.CallID)
					}
				}
				require.Equal(t, wantTypes, types)
				require.Equal(t, wantCiphers, ciphers)
				require.Equal(t, wantCalls, calls)
				require.Equal(t, calls, results)
			}
		})
	}
}

func TestAnthropicResponsesReasoningReplayStreamSignature(t *testing.T) {
	const cipher = "gAAAA-stream-synthetic-cipher"
	state := NewResponsesEventToAnthropicState()
	var events []AnthropicStreamEvent
	for _, event := range []*ResponsesStreamEvent{
		{Type: "response.created", Response: &ResponsesResponse{ID: "resp_stream", Model: "gpt-6-astra"}},
		{Type: "response.output_item.added", OutputIndex: 0, Item: &ResponsesOutput{Type: "reasoning", ID: "rs_stream"}},
		{Type: "response.reasoning_summary_text.delta", OutputIndex: 0, Delta: "Public summary"},
		{Type: "response.output_item.done", OutputIndex: 0, Item: &ResponsesOutput{Type: "reasoning", EncryptedContent: cipher}},
		{Type: "response.completed", Response: &ResponsesResponse{Status: "completed"}},
	} {
		events = append(events, ResponsesEventToAnthropicEvents(event, state)...)
	}
	requireAnthropicBlockLifecycle(t, events)
	block := AnthropicContentBlock{Type: "thinking"}
	for _, event := range events {
		if event.Delta == nil {
			continue
		}
		switch event.Delta.Type {
		case "thinking_delta":
			block.Thinking += event.Delta.Thinking
		case "signature_delta":
			block.Signature += event.Delta.Signature
		}
	}
	require.Equal(t, cipher, block.Signature)
	content, err := json.Marshal([]AnthropicContentBlock{block})
	require.NoError(t, err)
	req := &AnthropicRequest{Model: "gpt-6-astra", Messages: []AnthropicMessage{{Role: "assistant", Content: content}}}
	back, err := AnthropicToResponsesWithOptions(req, AnthropicToResponsesOptions{OpenAIEncryptedReasoning: true})
	require.NoError(t, err)
	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(back.Input, &items))
	require.Equal(t, []ResponsesInputItem{{Type: "reasoning", EncryptedContent: cipher}}, items)
	// The same history must still be rejected on the default compatible route.
	other, err := AnthropicToResponses(req)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(other.Input, &items))
	require.Empty(t, items)
}

func TestAnthropicResponsesReasoningReplayWhitespaceCannotBypassTarget(t *testing.T) {
	const cipher = "\n gAAAA-synthetic-cipher "
	content, err := json.Marshal([]AnthropicContentBlock{{Type: "thinking", Signature: cipher}})
	require.NoError(t, err)
	req := &AnthropicRequest{Model: "gpt-6-astra", Messages: []AnthropicMessage{{Role: "assistant", Content: content}}}
	legacy, err := AnthropicToResponses(req)
	require.NoError(t, err)
	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(legacy.Input, &items))
	require.Empty(t, items, "whitespace must not let OpenAI ciphertext reach xAI")
	openAI, err := AnthropicToResponsesWithOptions(req, AnthropicToResponsesOptions{OpenAIEncryptedReasoning: true})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(openAI.Input, &items))
	require.Equal(t, []ResponsesInputItem{{Type: "reasoning", EncryptedContent: cipher}}, items, "classify without rewriting the opaque payload; the upstream validates it")
}
