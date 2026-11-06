package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
	"github.com/tidwall/gjson"
)

// ObserveOperationProtocol consumes already parsed protocol payloads for diagnostics.
// It never changes forwarding, retry eligibility or billing. No response text is retained.
func ObserveOperationProtocol(ctx context.Context, protocol string, payload []byte, eventType string) {
	if _, staged := ctx.Value(deepSeekRecoveryAttemptKey{}).(*DeepSeekRecoveryAttempt); staged {
		return
	}
	if requesttiming.From(ctx) == nil || !gjson.ValidBytes(payload) {
		return
	}
	e := operationProtocolEvidence(protocol, gjson.ParseBytes(payload), eventType)
	requesttiming.ObserveOperationSource(ctx, e)
}

func OperationProtocolContentKind(protocol string, payload []byte, eventType string) string {
	if !gjson.ValidBytes(payload) {
		return ""
	}
	e := operationProtocolEvidence(protocol, gjson.ParseBytes(payload), eventType)
	if e.Answer {
		return "text"
	}
	if e.ToolCall {
		return "tool"
	}
	if e.Reasoning {
		return "reasoning"
	}
	return ""
}

func operationProtocolEvidence(protocol string, r gjson.Result, eventType string) requesttiming.OperationEvidence {
	e := requesttiming.OperationEvidence{Protocol: protocol}
	nonempty := func(v gjson.Result) bool { return strings.TrimSpace(v.String()) != "" }
	validTool := func(t gjson.Result) bool {
		return nonempty(t.Get("name")) && (nonempty(t.Get("id")) || nonempty(t.Get("call_id"))) && json.Valid([]byte(t.Get("arguments").String()))
	}
	finish := func(reason string) {
		if reason == "" {
			return
		}
		e.FinishReason = reason
		e.Complete = true
		switch reason {
		case "stop", "end_turn", "stop_sequence", "completed", "done", "tool_calls", "function_call", "tool_use":
			e.Terminal = "completed"
		case "length", "max_tokens", "max_output_tokens":
			e.Terminal = "incomplete"
		case "content_filter", "refusal":
			e.Terminal = "completed"
			e.Refusal = true
		case "error", "failed", "aborted", "cancelled", "canceled", "insufficient_system_resource":
			e.Terminal = "failed"
			e.FailureReason = "source_finish_failure"
		default:
			e.Complete = false
			e.Unsupported = true
		}
	}
	switch protocol {
	case "responses":
		typ := r.Get("type").String()
		if typ == "" {
			typ = eventType
		}
		item := func(t gjson.Result) {
			switch t.Get("type").String() {
			case "reasoning":
				e.Reasoning = true
			case "message":
				for _, p := range t.Get("content").Array() {
					if p.Get("type").String() == "output_text" && nonempty(p.Get("text")) {
						e.Answer = true
					}
					if p.Get("type").String() == "refusal" {
						e.Refusal = true
					}
				}
			case "custom_tool_call":
				if nonempty(t.Get("name")) && (nonempty(t.Get("id")) || nonempty(t.Get("call_id"))) && t.Get("input").Type == gjson.String {
					e.ToolCall = true
				}
			case "function_call":
				if validTool(t) {
					e.ToolCall = true
				}
			}
		}
		switch typ {
		case "response.output_text.delta":
			e.Answer = nonempty(r.Get("delta"))
		case "response.output_text.done":
			e.Answer = nonempty(r.Get("text"))
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			e.Reasoning = nonempty(r.Get("delta"))
		case "response.reasoning_text.done", "response.reasoning_summary_text.done":
			e.Reasoning = nonempty(r.Get("text"))
		case "response.refusal.delta", "response.refusal.done":
			e.Refusal = true
		case "response.output_item.done":
			item(r.Get("item"))
		}
		response := r
		if r.Get("response").IsObject() {
			response = r.Get("response")
		}
		terminal := typ == "response.completed" || typ == "response.done" || typ == "response.incomplete" || typ == "response.failed" || typ == "response.cancelled" || typ == "response.canceled"
		if terminal || response.Get("status").Exists() && typ == "" {
			for _, t := range response.Get("output").Array() {
				item(t)
			}
			status := response.Get("status").String()
			if status == "" {
				status = strings.TrimPrefix(typ, "response.")
			}
			if status == "incomplete" {
				finish(response.Get("incomplete_details.reason").String())
				if e.Terminal == "" {
					e.Terminal = "incomplete"
					e.Complete = true
				}
			} else {
				finish(status)
			}
			if response.Get("error").Exists() && response.Get("error").Type != gjson.Null {
				e.Terminal = "failed"
				e.Complete = true
				e.FailureReason = "source_error"
			}
		}
	case "chat":
		choices := r.Get("choices").Array()
		if len(choices) > 1 {
			e.Unsupported = true
			return e
		} // Multi-choice completion needs per-choice coverage.
		for _, c := range choices {
			m := c.Get("delta")
			if c.Get("message").IsObject() {
				m = c.Get("message")
			}
			e.Answer = nonempty(m.Get("content"))
			e.Reasoning = nonempty(m.Get("reasoning_content")) || nonempty(m.Get("reasoning"))
			e.Refusal = nonempty(m.Get("refusal"))
			for _, t := range m.Get("tool_calls").Array() {
				f := t.Get("function")
				if nonempty(t.Get("id")) && nonempty(f.Get("name")) && json.Valid([]byte(f.Get("arguments").String())) {
					e.ToolCall = true
				}
			}
			finish(c.Get("finish_reason").String())
			if c.Get("delta").Exists() && (e.FinishReason == "tool_calls" || e.FinishReason == "function_call") {
				e.Complete = false
				e.Unsupported = true
				e.Terminal = ""
			} // Streaming fragmented tools require a complete-tool observer.
		}
	case "messages":
		typ := r.Get("type").String()
		if typ == "" {
			typ = eventType
		}
		switch typ {
		case "content_block_delta":
			d := r.Get("delta")
			e.Answer = d.Get("type").String() == "text_delta" && nonempty(d.Get("text"))
			e.Reasoning = d.Get("type").String() == "thinking_delta" && nonempty(d.Get("thinking"))
		case "message_delta":
			e.FinishReason = r.Get("delta.stop_reason").String() // message_stop must also arrive.
		case "error":
			e.Terminal = "failed"
			e.Complete = true
			e.FailureReason = "source_error"
		}
		if typ == "message" {
			for _, b := range r.Get("content").Array() {
				switch b.Get("type").String() {
				case "text":
					e.Answer = e.Answer || nonempty(b.Get("text"))
				case "thinking":
					e.Reasoning = true
				case "tool_use":
					e.ToolCall = nonempty(b.Get("id")) && nonempty(b.Get("name")) && b.Get("input").IsObject()
				}
			}
			finish(r.Get("stop_reason").String())
		}
	}
	return e
}

// Recovery writers stage terminal frames; their real downstream sink owns delivery.
func operationProtocolOutputFlushed(ctx context.Context) {
	if _, staged := ctx.Value(deepSeekRecoveryAttemptKey{}).(*DeepSeekRecoveryAttempt); staged {
		return
	}
	requesttiming.OperationOutputFlushed(ctx)
}
func operationProtocolDelivered(ctx context.Context, err error) {
	if _, staged := ctx.Value(deepSeekRecoveryAttemptKey{}).(*DeepSeekRecoveryAttempt); staged {
		return
	}
	requesttiming.OperationDelivery(ctx, "", true, err)
}
