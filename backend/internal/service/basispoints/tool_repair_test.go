package basispoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func repairCall(id, summary, code string) object {
	args, _ := json.Marshal(object{"summary": summary, "code": code, "extended_summary": "{}", "destructive": false, "references": []any{}})
	return object{"type": "function_call", "name": "run_officejs", "id": "fc_" + id, "call_id": id, "arguments": string(args), "status": "completed"}
}

func repairResponse(id string, input, output int, items ...any) object {
	return object{"id": id, "model": "gpt-5.6-sol", "status": "completed", "output": items, "usage": object{
		"input_tokens": input, "output_tokens": output, "total_tokens": input + output, "input_tokens_details": object{"cached_tokens": 2}, "output_tokens_details": object{"reasoning_tokens": 1},
	}}
}

func repairBridge(t *testing.T, cache *ReplayCache) (object, *Bridge) {
	t.Helper()
	source := testSource()
	source["tools"] = []any{object{"type": "custom", "name": "functions.exec"}, object{"type": "function", "name": "shell"}}
	return mustPrepare(t, source, "repair-scope", cache)
}

func repairEvents(t *testing.T, body io.ReadCloser) []object {
	t.Helper()
	defer func() { _ = body.Close() }()
	var events []object
	err := readEvents(body, func(_ string, raw []byte) error {
		var event object
		require.NoError(t, decode(raw, &event))
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	for i, event := range events {
		require.Equal(t, json.Number(fmt.Sprint(i)), event["sequence_number"])
	}
	return events
}

func repairValue[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	require.True(t, ok, "unexpected decoded value type: %T", value)
	return result
}

type repairTrackedBody struct {
	io.Reader
	closed atomic.Int32
}

func (r *repairTrackedBody) Close() error { r.closed.Add(1); return nil }

func TestToolRepairKeepsOneResponseAndAtomicToolBatch(t *testing.T) {
	cache := new(ReplayCache)
	_, bridge := repairBridge(t, cache)
	code := "const value = 'literal';\ntext(value);"
	valid := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})
	invalid := repairCall("bad", "Run client tool", code)
	message := object{"type": "message", "id": "msg_original", "role": "assistant", "content": []any{object{"type": "output_text", "text": "Working."}}}
	original := repairResponse("resp_original", 10, 3, message, valid, invalid)
	wire := sse(object{"type": "response.created", "response": object{"id": "resp_original", "output": []any{}}}) + sse(object{"type": "response.output_text.delta", "delta": "Working."}) + sse(object{"type": "response.completed", "response": original})
	upstream := &repairTrackedBody{Reader: strings.NewReader(wire)}
	repairedValid := nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}})
	repairedValid["call_id"], repairedValid["id"] = "fixed_shell", "fc_fixed_shell"
	repairedCustom := repairCall("fixed_exec", "codex2api.custom/functions.exec", code)
	calls := 0
	stream := bridge.StreamWithToolRepair(context.Background(), upstream, func(ctx context.Context, failed object, validation error) (object, error) {
		calls++
		if upstream.closed.Load() != 1 || cache.get("repair-scope", "call_native") != nil {
			return nil, errors.New("upstream lease or partial cache retained before correction")
		}
		if validation == nil || text(failed["id"]) != "resp_original" || ctx.Err() != nil {
			return nil, errors.New("incorrect correction context")
		}
		return repairResponse("resp_hidden", 20, 4, object{"type": "message", "content": "Hidden commentary"}, repairedValid, repairedCustom), nil
	})
	events := repairEvents(t, stream)
	require.Equal(t, 1, calls)
	counts := make(map[string]int)
	var indexes []json.Number
	for _, event := range events {
		kind := text(event["type"])
		counts[kind]++
		if kind == "response.output_item.added" {
			indexes = append(indexes, repairValue[json.Number](t, event["output_index"]))
		}
	}
	require.Equal(t, []json.Number{"1", "2"}, indexes)
	require.Equal(t, 1, counts["response.created"])
	require.Equal(t, 1, counts["response.completed"])
	require.Equal(t, 1, counts["response.output_text.delta"])
	require.Zero(t, counts["response.failed"])
	last := repairValue[object](t, events[len(events)-1]["response"])
	require.Equal(t, "resp_original", last["id"])
	output := repairValue[[]any](t, last["output"])
	require.Equal(t, "msg_original", repairValue[object](t, output[0])["id"])
	require.Equal(t, code, repairValue[object](t, output[2])["input"])
	usage := repairValue[object](t, last["usage"])
	require.Equal(t, json.Number("30"), usage["input_tokens"])
	require.Equal(t, json.Number("7"), usage["output_tokens"])
	require.Equal(t, json.Number("37"), usage["total_tokens"])
	require.Equal(t, json.Number("4"), repairValue[object](t, usage["input_tokens_details"])["cached_tokens"])
	require.True(t, reflect.DeepEqual(repairedCustom, cache.get("repair-scope", "fixed_exec")))
	require.Nil(t, cache.get("repair-scope", "bad"))
	require.EqualValues(t, 1, upstream.closed.Load())
}

func TestToolRepairBoundsAttemptsAndPreservesFailureUsage(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			initial := repairResponse("resp_initial", 10, 2, repairCall("bad", "Run", "text(1)"))
			calls := 0
			stream := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(_ context.Context, failed object, _ error) (object, error) {
				calls++
				if calls == 2 && failed["id"] != "resp_1" {
					return nil, errors.New("did not continue latest failed response")
				}
				summary := "Run"
				if success && calls == 2 {
					summary = "codex2api.custom/functions.exec"
				}
				return repairResponse(fmt.Sprintf("resp_%d", calls), 20, 3, repairCall(fmt.Sprintf("retry_%d", calls), summary, "text(1)")), nil
			})
			events := repairEvents(t, stream)
			require.Equal(t, 2, calls)
			last := events[len(events)-1]
			response := repairValue[object](t, last["response"])
			if success {
				require.Equal(t, "response.completed", last["type"])
			} else {
				require.Equal(t, "response.failed", last["type"])
				require.Len(t, events, 1)
				require.Contains(t, repairValue[object](t, response["error"])["message"], "after 2 corrections")
				require.Empty(t, response["output"])
			}
			usage := repairValue[object](t, response["usage"])
			require.Equal(t, json.Number("50"), usage["input_tokens"])
			require.Equal(t, json.Number("8"), usage["output_tokens"])
		})
	}
}

func TestToolRepairDoesNotRetryOtherFailures(t *testing.T) {
	for _, name := range []string{"unsupported_native", "undeclared_target", "missing_call_id", "invalid_outer_json", "incomplete", "failed", "missing_terminal_tool", "truncated", "structured"} {
		t.Run(name, func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			item := repairCall("bad", "Run", "text(1)")
			kind := "response.completed"
			prefix := ""
			switch name {
			case "unsupported_native":
				item["name"] = "unknown"
			case "undeclared_target":
				item = nativeCall(object{"name": "outside_catalog", "arguments": object{}})
			case "missing_call_id":
				delete(item, "call_id")
			case "invalid_outer_json":
				item["arguments"] = "invalid"
			case "incomplete", "failed":
				kind = "response." + name
			case "missing_terminal_tool":
				prefix = sse(object{"type": "response.output_item.done", "item": repairCall("missing", "Run", "text(1)")})
			case "structured":
				source := testSource()
				source["text"] = object{"format": object{"type": "json_object"}}
				_, bridge = mustPrepare(t, source, "", nil)
			}
			wire := prefix + sse(object{"type": kind, "response": repairResponse("resp", 10, 2, item)})
			if name == "truncated" {
				wire = sse(object{"type": "response.output_item.done", "item": item})
			}
			calls := 0
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(wire)), func(context.Context, object, error) (object, error) {
				calls++
				return nil, errors.New("unexpected repair")
			})
			out, err := io.ReadAll(body)
			_ = body.Close()
			if name == "truncated" {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, calls)
			require.NotContains(t, string(out), "response.custom_tool_call_input")
		})
	}
}

func TestToolRepairRejectsChangedBatchAndOperations(t *testing.T) {
	for _, change := range []string{"extra_tool", "text_only", "raw_code", "valid_operation", "explicit_target", "raw_json"} {
		t.Run(change, func(t *testing.T) {
			cache := new(ReplayCache)
			_, bridge := repairBridge(t, cache)
			code := "text(1)"
			if change == "raw_json" {
				code = `{"payload":1}`
			}
			original := []any{repairCall("bad", "Run", code)}
			corrected := []any{repairCall("fixed", "codex2api.custom/functions.exec", code)}
			switch change {
			case "extra_tool":
				corrected = append(corrected, repairCall("extra", "codex2api.custom/functions.exec", code))
			case "text_only":
				corrected = nil
			case "raw_code", "raw_json":
				corrected = []any{repairCall("fixed", "codex2api.custom/functions.exec", "text(2)")}
			case "valid_operation":
				original = append(original, nativeCall(object{"name": "shell", "arguments": object{"cmd": "pwd"}}))
				corrected = append(corrected, nativeCall(object{"name": "shell", "arguments": object{"cmd": "ls"}}))
			case "explicit_target":
				original = []any{nativeCall(object{"name": "shell", "arguments": 42})}
			}
			calls := 0
			initial := repairResponse("resp", 10, 2, original...)
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				calls++
				return repairResponse("resp_fixed", 20, 3, corrected...), nil
			})
			events := repairEvents(t, body)
			require.Equal(t, 1, calls)
			require.Len(t, events, 1)
			require.Equal(t, "response.failed", events[0]["type"])
			require.Nil(t, cache.get("repair-scope", "fixed"))
		})
	}
}

func TestToolRepairDoesNotRetryCallbackFailure(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("resp", 10, 2, repairCall("bad", "Run", "text(1)"))
	calls := 0
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
		calls++
		return repairResponse("resp_failed", 20, 3), io.ErrUnexpectedEOF
	})
	events := repairEvents(t, body)
	require.Len(t, events, 1)
	require.Equal(t, 1, calls)
	require.Equal(t, "response.failed", events[0]["type"])
	response := repairValue[object](t, events[0]["response"])
	require.Equal(t, json.Number("30"), repairValue[object](t, response["usage"])["input_tokens"])
}

func TestToolRepairCloseCancelsInFlightCorrection(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	initial := repairResponse("resp_initial", 10, 2, repairCall("bad", "Run", "text(1)"))
	started, canceled := make(chan struct{}), make(chan struct{})
	body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(ctx context.Context, _ object, _ error) (object, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("correction did not start")
	}
	require.NoError(t, body.Close())
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("correction was not canceled")
	}
}

func TestToolRepairBuilderPreservesNativeHistoryAndSettings(t *testing.T) {
	prepared, _ := repairBridge(t, nil)
	encoded, err := json.Marshal(prepared)
	require.NoError(t, err)
	initialLen := len(repairValue[[]any](t, prepared["input"]))
	for attempt := 0; attempt < 2; attempt++ {
		failed := repairResponse("resp", 10, 2, repairCall(fmt.Sprint(attempt), "Run", "text(1)"))
		encoded, err = BuildToolRepairRequest(encoded, failed, errors.New("missing CUSTOM marker"))
		require.NoError(t, err)
		var request object
		require.NoError(t, decode(encoded, &request))
		for _, field := range []string{"model", "reasoning_effort", "model_selection", "prompt_cache_key", "context_management"} {
			require.Equal(t, prepared[field], request[field])
		}
		require.NotContains(t, request, "tools")
		require.NotContains(t, request, "previous_response_id")
		metadata := repairValue[object](t, request["metadata"])
		require.Equal(t, repairValue[object](t, prepared["metadata"])["task_id"], metadata["task_id"])
		require.Equal(t, repairValue[object](t, prepared["metadata"])["turn_id"], metadata["turn_id"])
		require.Equal(t, fmt.Sprint(attempt+2), metadata["agent_iteration"])
		input := repairValue[[]any](t, request["input"])
		require.Len(t, input, initialLen+(attempt+1)*3)
		require.Equal(t, prepared["input"], input[:initialLen])
		result := repairValue[object](t, input[len(input)-2])
		require.Equal(t, fmt.Sprint(attempt), result["call_id"])
		var feedback object
		require.NoError(t, decode([]byte(text(result["output"])), &feedback))
		require.Equal(t, false, feedback["executed"])
		require.Equal(t, "invalid_client_tool_transport", repairValue[object](t, feedback["error"])["code"])
	}
}

func TestToolRepairReaderRejectsIncompleteOrMissingTools(t *testing.T) {
	item := repairCall("good", "codex2api.custom/functions.exec", "text(1)")
	for _, kind := range []string{"response.completed", "response.failed", "response.incomplete", "missing_tool", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			response := repairResponse("resp", 10, 2, item)
			event := kind
			if kind == "missing_tool" {
				response["output"], event = []any{}, "response.completed"
			}
			wire := sse(object{"type": "response.output_item.done", "item": item})
			if kind != "truncated" {
				wire += sse(object{"type": event, "response": response})
			}
			actual, err := ReadToolRepairResponse(strings.NewReader(wire))
			if kind == "response.completed" {
				require.NoError(t, err)
				require.Equal(t, "resp", actual["id"])
			} else {
				require.Error(t, err)
			}
			if kind != "truncated" {
				require.Equal(t, json.Number("10"), repairValue[object](t, actual["usage"])["input_tokens"])
			}
		})
	}
}
