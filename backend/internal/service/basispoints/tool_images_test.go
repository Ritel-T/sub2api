package basispoints

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNativeToolImagesKeepOrderedContentAndCallIdentity(t *testing.T) {
	for _, kind := range []string{"function", "custom_tool"} {
		t.Run(kind, func(t *testing.T) {
			call := object{"type": kind + "_call", "call_id": "call_image", "name": "inspect", "arguments": `{}`, "input": "inspect"}
			parts := []any{
				object{"type": "input_text", "text": "before"},
				object{"type": "input_image", "file_id": "file-first", "detail": "original"},
				object{"type": "input_text", "text": "between"},
				object{"type": "input_image", "image_url": "https://example.com/image.png?signed=1"},
				object{"type": "input_image", "file_id": "file-second", "detail": "low"},
				object{"type": "input_text", "text": "after"},
			}
			source := testSource()
			source["input"] = []any{message("user", "inspect"), call, object{"type": kind + "_call_output", "call_id": "call_image", "output": parts}}
			body, _ := mustPrepare(t, source, "scope", new(ReplayCache))
			items := mustTestValue[[]any](t, body["input"])
			result := mustTestValue[object](t, items[len(items)-2])
			if result["type"] != "function_call_output" || result["call_id"] != "call_image" || !strings.Contains(text(result["output"]), "call_image") {
				t.Fatalf("tool result lost its identity: %+v", result)
			}
			imageMessage := mustTestValue[object](t, items[len(items)-1])
			content := mustTestValue[[]any](t, imageMessage["content"])
			if imageMessage["role"] != "user" || !reflect.DeepEqual(content[1:], parts) || !strings.Contains(text(mustTestValue[object](t, content[0])["text"]), "call_image") {
				t.Fatalf("tool image result changed order or lost content: %+v", imageMessage)
			}
			retry, _ := mustPrepare(t, source, "scope", new(ReplayCache))
			if !reflect.DeepEqual(body, retry) {
				t.Fatal("tool image replay is not deterministic")
			}
		})
	}
}

func TestNativeToolImagesValidateBeforeRelocating(t *testing.T) {
	source := testSource()
	source["input"] = []any{
		object{"type": "function_call", "name": "inspect", "call_id": "call_image", "arguments": `{}`},
		object{"type": "function_call_output", "call_id": "call_image", "output": []any{object{"type": "input_image", "file_id": "bad-private-id"}}},
	}
	raw, _ := json.Marshal(source)
	_, _, err := Prepare(raw, "scope", nil)
	if err == nil || !strings.Contains(err.Error(), "path=input[1].output[0]") || strings.Contains(err.Error(), "bad-private-id") {
		t.Fatalf("invalid image must retain a safe source diagnostic: %v", err)
	}
}
