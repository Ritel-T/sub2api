package service

import "encoding/json"

// [OpusClaw Patch] Clear Claude thinking signatures after account switch.
// CleanClaudeThinkingSignatures clears the "signature" field from thinking
// blocks in a Claude /v1/messages request body. This causes the downstream
// request_transformer to downgrade those blocks to plain text, preventing
// cross-account signature validation failures (400 errors).
func CleanClaudeThinkingSignatures(body []byte) []byte {
	if len(body) == 0 {
		return body
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return body
	}

	messages, ok := data["messages"].([]any)
	if !ok {
		return body
	}

	changed := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}
		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if blockMap["type"] == "thinking" {
				if _, hasSig := blockMap["signature"]; hasSig {
					blockMap["signature"] = ""
					changed = true
				}
			}
		}
	}

	if !changed {
		return body
	}

	result, err := json.Marshal(data)
	if err != nil {
		return body
	}
	return result
}
