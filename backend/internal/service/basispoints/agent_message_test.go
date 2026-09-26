package basispoints

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

func collaborationSource(name string) object {
	source := testSource()
	source["tools"] = []any{object{
		"type": "namespace", "name": "collaboration", "tools": []any{object{
			"type": "function", "name": name, "parameters": object{
				"type": "object", "properties": object{
					"message": object{"type": "string", "encrypted": true},
				},
			},
		}},
	}}
	return source
}

func requirePlaintextArguments(t *testing.T, call object) {
	t.Helper()
	metadata, err := json.Marshal(call["encrypted_function_args"])
	// Missing and null are not equivalent to [] in Codex's direct_source.
	if err != nil || string(metadata) != "[]" {
		t.Fatalf("expected an explicit empty encryption list, got %s (%v)", metadata, err)
	}
}

func TestCollaborationPlaintextStreamsAndReplays(t *testing.T) {
	const task = "核对 SVG 动画.\nPreserve \"quotes\", tabs\tand \r\nline endings."
	for _, name := range []string{"spawn_agent", "send_message", "followup_task"} {
		for _, direct := range []bool{false, true} {
			mode := "relay"
			if direct {
				mode = "direct"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				cache := new(ReplayCache)
				source := collaborationSource(name)
				_, bridge := mustPrepare(t, source, "account/key/parent", cache)
				args := object{"message": task}
				if name == "spawn_agent" {
					args["task_name"], args["fork_turns"] = "worker", "none"
				} else {
					args["target"] = "worker"
				}
				native := nativeCall(object{"name": "collaboration." + name, "arguments": args})
				if direct {
					raw, _ := json.Marshal(args)
					native["name"], native["arguments"] = "collaboration."+name, string(raw)
				}
				wire := sse(object{"type": "response.created", "response": object{"id": "resp_parent", "output": []any{}}}) +
					sse(object{"type": "response.output_item.done", "output_index": 0, "item": native}) +
					sse(object{"type": "response.completed", "response": object{"id": "resp_parent", "output": []any{native}}})
				body := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
				defer func() { _ = body.Close() }()
				output, err := io.ReadAll(body)
				if err != nil {
					t.Fatal(err)
				}
				var call object
				markedItems := 0
				var argumentDelta string
				err = readEvents(bytes.NewReader(output), func(_ string, data []byte) error {
					var event object
					if err := decode(data, &event); err != nil {
						return err
					}
					switch text(event["type"]) {
					case "response.output_item.added", "response.output_item.done":
						item := mustTestValue[object](t, event["item"])
						requirePlaintextArguments(t, item)
						markedItems++
						if text(event["type"]) == "response.output_item.done" {
							call = item
						}
					case "response.function_call_arguments.delta":
						argumentDelta += text(event["delta"])
					case "response.completed":
						response := mustTestValue[object](t, event["response"])
						items := mustTestValue[[]any](t, response["output"])
						requirePlaintextArguments(t, mustTestValue[object](t, items[0]))
						markedItems++
					}
					return nil
				})
				if err != nil || markedItems != 3 || call == nil {
					t.Fatalf("missing consistent streaming metadata: items=%d err=%v", markedItems, err)
				}
				if call["namespace"] != "collaboration" || call["name"] != name || argumentDelta != call["arguments"] {
					t.Fatalf("client call changed: %+v", call)
				}
				var clientArgs object
				if err := decode([]byte(text(call["arguments"])), &clientArgs); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(clientArgs, args) {
					t.Fatal("task arguments changed")
				}

				// Codex uses input_text for calls carrying this explicit marker.
				// The child has a different scope and need not share replay state.
				child := testSource()
				childMessage := object{"type": "agent_message", "author": "/root", "recipient": "/root/worker",
					"content": []any{object{"type": "input_text", "text": clientArgs["message"]}}}
				child["input"] = []any{childMessage}
				prepared, _ := mustPrepare(t, child, "account/key/child", new(ReplayCache))
				childItems := mustTestValue[[]any](t, prepared["input"])
				if !reflect.DeepEqual(childItems[len(childItems)-1], childMessage) {
					t.Fatal("child task changed")
				}

				// Both native replay and cache-miss recovery retain the plaintext.
				source["input"] = []any{call, object{"type": "function_call_output", "call_id": call["call_id"], "output": "delivered"}}
				for _, replay := range []*ReplayCache{cache, new(ReplayCache)} {
					prepared, _ := mustPrepare(t, source, "account/key/parent", replay)
					items := mustTestValue[[]any](t, prepared["input"])
					envelope := historyCollisionEnvelope(t, mustTestValue[object](t, items[len(items)-2]))
					if !reflect.DeepEqual(envelope["arguments"], args) {
						t.Fatal("replay changed task arguments")
					}
				}
			})
		}
	}
}

func TestDirectCollaborationPlaintextMetadata(t *testing.T) {
	for _, metadata := range []any{nil, []any{}} {
		_, bridge := mustPrepare(t, collaborationSource("spawn_agent"), "scope", nil)
		native := object{"type": "function_call", "name": "collaboration.spawn_agent", "call_id": "call_direct",
			"arguments": `{"message":"opaque-ciphertext","task_name":"worker"}`, "encrypted_function_args": metadata}
		call, err := bridge.translateCall(native)
		if err != nil {
			t.Fatal(err)
		}
		if metadata == nil {
			requirePlaintextArguments(t, call)
		} else if got, exists := call["encrypted_function_args"]; !exists || !reflect.DeepEqual(got, metadata) {
			t.Fatalf("native encryption declaration was changed: got=%v want=%v", got, metadata)
		}
		if call["arguments"] != native["arguments"] {
			t.Fatal("ciphertext arguments changed")
		}
	}
}

func TestEncryptedDirectCollaborationIsRejectedBeforeDispatch(t *testing.T) {
	for _, name := range []string{"spawn_agent", "send_message", "followup_task"} {
		for _, metadata := range []any{[]any{"message"}, "message", false, object{}} {
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, collaborationSource(name), "scope", cache)
			native := object{"type": "function_call", "name": "collaboration." + name, "call_id": "call_encrypted", "id": "fc_encrypted",
				"arguments": `{"message":"private-ciphertext","target":"worker"}`, "encrypted_function_args": metadata}
			wire := sse(object{"type": "response.output_item.done", "item": native, "output_index": 0}) +
				sse(object{"type": "response.completed", "response": object{"id": "resp_encrypted", "status": "completed", "output": []any{native}}})
			body := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
			raw, err := io.ReadAll(body)
			_ = body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte("encrypted collaboration arguments")) || bytes.Contains(raw, []byte("private-ciphertext")) || bytes.Contains(raw, []byte("response.output_item.added")) || cache.get("scope", "call_encrypted") != nil {
				t.Fatalf("encrypted collaboration escaped validation: %s", raw)
			}
		}
	}
}

func TestDirectNonCollaborationEncryptionMetadataIsPreserved(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "opaque_tool", "parameters": object{"type": "object"}}}
	_, bridge := mustPrepare(t, source, "scope", nil)
	native := object{"type": "function_call", "name": "opaque_tool", "call_id": "call_opaque",
		"arguments": `{"value":"opaque"}`, "encrypted_function_args": []any{"value"}}
	call, err := bridge.translateCall(native)
	if err != nil || !reflect.DeepEqual(call["encrypted_function_args"], native["encrypted_function_args"]) || call["arguments"] != native["arguments"] {
		t.Fatalf("unrelated tool encryption metadata changed: %+v, %v", call, err)
	}
}

func TestPlaintextChildReplyCanContinueParentAfterSendMessage(t *testing.T) {
	source := collaborationSource("send_message")
	cache := new(ReplayCache)
	_, bridge := mustPrepare(t, source, "parent", cache)
	native := nativeCall(object{"name": "collaboration.send_message", "arguments": object{"target": "worker", "message": "check the result"}})
	call, err := bridge.translateCall(native)
	if err != nil {
		t.Fatal(err)
	}
	reply := object{"type": "agent_message", "author": "/root/worker", "recipient": "/root", "content": []any{
		object{"type": "input_text", "text": "Message from worker:"}, object{"type": "input_text", "text": "checked result"},
	}}
	source["input"] = []any{message("user", "continue"), call, object{"type": "function_call_output", "call_id": call["call_id"], "output": ""}, reply}
	prepared, _ := mustPrepare(t, source, "parent", cache)
	items := mustTestValue[[]any](t, prepared["input"])
	if !reflect.DeepEqual(items[len(items)-1], reply) {
		t.Fatal("plaintext child reply was changed")
	}
	parts := reply["content"].([]any)
	parts[1] = object{"type": "encrypted_content", "encrypted_content": "opaque-child-reply"}
	raw, _ := json.Marshal(source)
	_, _, err = Prepare(raw, "parent", cache)
	if err == nil || !strings.Contains(err.Error(), "path=input[3].content[1]") || strings.Contains(err.Error(), "opaque-child-reply") {
		t.Fatalf("opaque child reply must not be discarded or relabeled: %v", err)
	}
}

func TestRelayDoesNotCopyWrapperEncryptionMetadata(t *testing.T) {
	_, bridge := mustPrepare(t, collaborationSource("spawn_agent"), "scope", nil)
	native := nativeCall(object{"name": "collaboration.spawn_agent", "arguments": object{"message": "plain task"}})
	native["encrypted_function_args"] = []any{"code"}
	call, err := bridge.translateCall(native)
	if err != nil {
		t.Fatal(err)
	}
	requirePlaintextArguments(t, call)
}

func TestAgentCiphertextIsNeverGuessedAsPlaintext(t *testing.T) {
	for _, value := range []string{"ordinary-looking task from an old session", "gAAAAABopaqueCiphertext"} {
		source := testSource()
		source["input"] = []any{object{"type": "agent_message", "author": "/root", "recipient": "/root/worker",
			"content": []any{object{"type": "encrypted_content", "encrypted_content": value}}}}
		raw, _ := json.Marshal(source)
		if _, _, err := Prepare(raw, "scope", nil); err == nil || !strings.Contains(err.Error(), "type=encrypted_content") || !strings.Contains(err.Error(), "path=input[0].content[0]") || strings.Contains(err.Error(), value) {
			t.Fatalf("encrypted content must be rejected with a safe, specific diagnostic: %v", err)
		}
	}
}
