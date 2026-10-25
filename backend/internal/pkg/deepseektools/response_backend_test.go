package deepseektools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeAlphaCitationRowsDoNotHidePageText(t *testing.T) {
	raw := []byte(`{"output":"Fixture page (https://example.com/)\nciteturn0view0 [wordlim: 200] Source: open({}); Total lines: 2\nL0: # Fixture page\nL1: Real readable page text.","results":[{"type":"text_result","ref_id":"turn0view0","url":"https://example.com/","title":"Fixture page","snippet":"A citation"}]}`)
	for _, operation := range []string{"fetch", "search"} {
		input := `{"url":"https://example.com/"}`
		if operation == "search" {
			input = `{"query":"fixture","need_content":true}`
		}
		r, err := ParseRequest(operation, []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		body, usable, err := AdaptResponseFrom(raw, r, BackendNativeAlpha)
		if err != nil || !usable || !strings.Contains(string(body), "Real readable page text.") {
			t.Fatalf("%s native body lost: usable=%v err=%v body=%s", operation, usable, err, body)
		}
		var decoded map[string]any
		if err = json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["provider"] != "openai" || decoded["partial"] != true {
			t.Fatal("honest provider/partial metadata lost")
		}
		body, usable, err = AdaptResponseFrom(raw, r, BackendResponsesWebSearch)
		if operation == "fetch" {
			if err == nil || usable {
				t.Fatal("generated fallback text became fetched page")
			}
		} else if err != nil || !usable || strings.Contains(string(body), "Real readable page text.") {
			t.Fatalf("fallback summary treated as page content: %v %s", err, body)
		}
	}
}

func TestUnknownResponseBackendIsRejected(t *testing.T) {
	r, err := ParseRequest("search", []byte(`{"query":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, usable, err := AdaptResponseFrom([]byte(`{"results":[]}`), r, BackendAuto); err == nil || usable {
		t.Fatal("unknown provenance accepted")
	}
}

func TestCanonicalDeepSeekMetadataFieldsAreExplicitlyUnknown(t *testing.T) {
	r, err := ParseRequest("search", []byte(`{"query":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"output":"Fixture page (https://example.com/)\nPage text.","results":[]}`)
	body, usable, err := AdaptResponseFrom(raw, r, BackendNativeAlpha)
	if err != nil || !usable {
		t.Fatal(err)
	}
	var result struct {
		Results []map[string]any `json:"results"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	row := result.Results[0]
	for _, key := range []string{"date", "site_name", "site_icon"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("canonical metadata key %s missing", key)
		}
	}
	if row["date"] != nil || row["site_name"] != "" || row["site_icon"] != "" {
		t.Fatal("unknown metadata was invented")
	}
}
