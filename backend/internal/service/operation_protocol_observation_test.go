package service

import (
	"github.com/tidwall/gjson"
	"testing"
)

func TestOperationProtocolUnknownFinish(t *testing.T) {
	e := operationProtocolEvidence("chat", gjson.Parse(`{"choices":[{"message":{"content":"text"},"finish_reason":"vendor_future_reason"}]}`), "")
	if e.Terminal != "" || e.Complete || !e.Unsupported {
		t.Fatalf("unknown finish must stay unknown: %+v", e)
	}
}

func TestOperationProtocolSourceEvidence(t *testing.T) {
	cases := []struct {
		name, protocol, payload string
		answer, reasoning, tool bool
		terminal                string
	}{
		{"reasoning only is not answer", "chat", `{"choices":[{"message":{"reasoning_content":"hidden"},"finish_reason":"stop"}]}`, false, true, false, "completed"},
		{"usage is not terminal", "chat", `{"choices":[],"usage":{"completion_tokens":8}}`, false, false, false, ""},
		{"legal tool without text", "responses", `{"status":"completed","output":[{"type":"function_call","id":"fc1","call_id":"c1","name":"search","arguments":"{}"}]}`, false, false, true, "completed"},
		{"incomplete tool not success evidence", "responses", `{"status":"completed","output":[{"type":"function_call","id":"fc1","name":"search","arguments":"{"}]}`, false, false, false, "completed"},
		{"budget cut", "chat", `{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`, true, false, false, "incomplete"},
		{"source failure despite text", "responses", `{"type":"response.failed","response":{"status":"failed","output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}}`, true, false, false, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := operationProtocolEvidence(c.protocol, gjson.Parse(c.payload), "")
			if e.Answer != c.answer || e.Reasoning != c.reasoning || e.ToolCall != c.tool || e.Terminal != c.terminal {
				t.Fatalf("unexpected evidence: %+v", e)
			}
		})
	}
}
