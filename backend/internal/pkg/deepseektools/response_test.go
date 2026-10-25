package deepseektools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

const nativeSearchFixture = `First source (https://example.com/one)
【turn0search0】 [wordlim: 200] Published: yesterday; Crawled: today;

First actual snippet.
More first-page text.

第二来源 (https://example.org/two)
【turn0search1】 [wordlim: 200] Crawled: last week;

第二页面的实际片段。
`

const nativeOpenFixture = `Example page (https://example.com/page)
【turn0view0】 [wordlim: 200] Content type: text/html; Source: open({"ref_id":"https://example.com/page"}); Total lines: 350
L0: Example heading
L1:
L2: Readable page body, not necessarily the whole page.
L3: 最后一行。
`

func alphaJSON(t *testing.T, output string, results any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"output": output, "results": results, "encrypted_output": "SECRET_CIPHERTEXT_MUST_NOT_LEAK"})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readSearch(t *testing.T, raw []byte, r *Request) (searchResponse, bool) {
	t.Helper()
	body, usable, err := AdaptResponse(raw, r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "SECRET") || strings.Contains(string(body), "encrypted_output") {
		t.Fatal("encrypted data leaked")
	}
	var response searchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	return response, usable
}

func TestNativeSearchEmptyStructuredResults(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"sources","need_content":true}`)
	response, usable := readSearch(t, alphaJSON(t, nativeSearchFixture, []any{}), r)
	if !usable || len(response.Results) != 2 || response.Provider != "openai" || !response.Partial || response.Truncated {
		t.Fatalf("bad response: %+v", response)
	}
	first := response.Results[0]
	if first.Title != "First source" || first.URL != "https://example.com/one" || first.Content != "First actual snippet.\nMore first-page text." || first.Snippet != first.Content {
		t.Fatalf("first block incorrectly parsed: %+v", first)
	}
	if first.RerankScore != nil || first.PublishTime != nil || first.Authority != -1 {
		t.Fatal("fabricated missing metadata")
	}
	if !strings.Contains(response.Results[1].Content, "第二页面") || strings.Contains(first.Content, "第二页面") {
		t.Fatal("merged independent pages")
	}
	for _, v := range response.Results {
		if strings.Contains(v.Content, "wordlim") || strings.Contains(v.Content, "turn0") || strings.Contains(v.Content, "Crawled") {
			t.Fatalf("metadata leaked: %+v", v)
		}
	}
	r.NeedContent = false
	response, _ = readSearch(t, alphaJSON(t, nativeSearchFixture, []any{}), r)
	for _, v := range response.Results {
		if v.Content != "" || v.Snippet == "" {
			t.Fatal("need_content=false must preserve snippets and clear content")
		}
	}
	r.Count = 1
	response, _ = readSearch(t, alphaJSON(t, nativeSearchFixture, []any{}), r)
	if len(response.Results) != 1 || !response.Truncated {
		t.Fatal("count must clip honestly")
	}
}

func TestNativeFetchLinesAndPartial(t *testing.T) {
	r := mustRequest(t, "fetch", `{"url":"https://example.com/page"}`)
	raw := alphaJSON(t, nativeOpenFixture, []any{})
	body, usable, err := AdaptResponse(raw, r)
	if err != nil {
		t.Fatal(err)
	}
	var response fetchResponse
	_ = json.Unmarshal(body, &response)
	if !usable || !response.Partial || response.Truncated || response.Provider != "openai" || response.Title != "Example page" || response.URL != r.URL || response.Content != "Example heading\n\nReadable page body, not necessarily the whole page.\n最后一行。" {
		t.Fatalf("unexpected open result: %s", body)
	}
	if strings.Contains(string(body), "L0:") || strings.Contains(string(body), "wordlim") || strings.Contains(string(body), "encrypted") {
		t.Fatal("native transport fields leaked")
	}
	r.NeedContent = false
	body, usable, err = AdaptResponse(raw, r)
	if err != nil || !usable {
		t.Fatal(err)
	}
	_ = json.Unmarshal(body, &response)
	if response.Content != "" {
		t.Fatal("explicit need_content=false was ignored for fetch")
	}
}

func TestStructuredResultsAndRefOnlyMerge(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"x","need_content":true}`)
	raw := alphaJSON(t, "", []any{map[string]any{"title": "Structured", "url": "https://example.com/a", "snippet": "excerpt", "summary": "summary", "content": "page text", "publish_time": "do not fabricate", "authority": 99, "rerank_score": 0.9, "encrypted_output": "SECRET"}})
	got, usable := readSearch(t, raw, r)
	if !usable || len(got.Results) != 1 {
		t.Fatal(got)
	}
	result := got.Results[0]
	if result.Content != "page text" || result.Snippet != "excerpt" || result.Summary != "summary" || result.Authority != -1 || result.PublishTime != nil || result.RerankScore != nil {
		t.Fatalf("bad structured conversion: %+v", result)
	}
	raw = alphaJSON(t, nativeSearchFixture, []any{map[string]any{"title": "Declared title", "ref_id": "turn0search0"}})
	got, _ = readSearch(t, raw, r)
	if len(got.Results) != 2 || got.Results[0].Title != "Declared title" || got.Results[0].URL != "https://example.com/one" || got.Results[0].Content == "" {
		t.Fatalf("failed to resolve ref: %+v", got)
	}
	raw = alphaJSON(t, "", []any{map[string]any{"ref_id": "https://example.com/a", "title": "Reference URL", "text": "raw text"}})
	got, _ = readSearch(t, raw, r)
	if len(got.Results) != 1 || got.Results[0].URL != "https://example.com/a" || got.Results[0].Content != "raw text" {
		t.Fatal(got)
	}
	// A structured row and its native block must not consume count twice.
	raw = alphaJSON(t, nativeSearchFixture, []any{map[string]any{"title": "First", "url": "https://example.com/one", "ref_id": "turn0search0"}})
	got, _ = readSearch(t, raw, r)
	if len(got.Results) != 2 || got.Results[0].Content == "" {
		t.Fatalf("failed to merge native content: %+v", got)
	}
}

func TestPATCitationsNeverBecomePageBody(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"x","need_content":true}`)
	rows := []any{map[string]any{"type": "text_result", "ref_id": "turn0search0", "url": "https://example.com/page", "title": "Citation", "content": "MODEL_ANSWER_COPIED_TO_CONTENT"}}
	got, usable := readSearch(t, alphaJSON(t, "MODEL_SYNTHESIS_ANSWER", rows), r)
	if !usable || len(got.Results) != 1 || got.Results[0].Content != "" || got.Results[0].Snippet != "" || !got.Partial {
		t.Fatal(got)
	}
	// Even a model answer formatted as a native-looking block is not a page.
	got, _ = readSearch(t, alphaJSON(t, nativeOpenFixture, rows), r)
	if len(got.Results) != 1 || got.Results[0].Content != "" {
		t.Fatal("parsed PAT synthesis as native page text")
	}
	fetch := mustRequest(t, "fetch", `{"url":"https://example.com/page"}`)
	if body, ok, err := AdaptResponse(alphaJSON(t, nativeOpenFixture, rows), fetch); err == nil || ok || body != nil {
		t.Fatal("PAT citation-only fetch incorrectly succeeded")
	}
}

func TestConfirmedEmptyAndUnrecognizedErrors(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"x"}`)
	for _, raw := range []string{`{"results":[]}`, `{"output":"","results":[]}`, `{"output":"No results found.","results":[]}`, `{"output":"No search results found"}`} {
		body, ok, err := AdaptResponse([]byte(raw), r)
		if err != nil || ok || !strings.Contains(string(body), `"results":[]`) {
			t.Fatalf("confirmed empty failed: %s / %v", raw, err)
		}
	}
	bad := []string{
		``, `null`, `[]`, `{}`, `{"encrypted_output":"SECRET"}`, `{"output":""}`, `{"output":"some unstructured answer","results":[]}`, `{"output":"Error: upstream broken","results":[]}`, `{"output":"Failed to fetch URL","results":[]}`, `{"output":"403 Forbidden","results":[]}`, `{"output":"无法打开网页","results":[]}`, `{"output":"Access denied","results":[]}`,
		`{"error":{"message":"SECRET"},"results":[]}`, `{"errors":["bad"],"results":[]}`, `{"status":"failed","results":[]}`, `{"type":"error","results":[]}`, `{"success":false,"results":[]}`, `{"status_code":503,"results":[]}`, `{"output":[],"results":[]}`, `{"results":{}}`, `{"results":[1]}`, `{"results":[null]}`, `{"results":[{"title":7,"url":"https://example.com"}]}`, `{"results":[{"url":"https://example.com","content":{}}]}`, `{"results":[{"url":"http://127.0.0.1","content":"private page"}]}`, `{"results":[{"ref_id":"unknown"}]}`,
	}
	for _, raw := range bad {
		t.Run(raw, func(t *testing.T) {
			body, ok, err := AdaptResponse([]byte(raw), r)
			if err == nil || ok || body != nil {
				t.Fatalf("accepted bad alpha payload: %s", raw)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("raw upstream error leaked")
			}
		})
	}
	if _, _, err := AdaptResponse([]byte(`{}`), nil); err == nil {
		t.Fatal("accepted nil request")
	}
	oversized := []byte(`{"output":"` + strings.Repeat("x", MaxInputBytes) + `"}`)
	if body, ok, err := AdaptResponse(oversized, r); err == nil || ok || body != nil {
		t.Fatal("accepted oversized upstream response")
	}
}

func TestFailedOpenAndWrongPageAreErrors(t *testing.T) {
	r := mustRequest(t, "fetch", `{"url":"https://example.com/page"}`)
	outputs := []string{
		"", "Error: unable to open", "No results found", "Other page (https://example.org/other)\n【turn0view0】 [wordlim: 200]\nL0: unrelated page",
		"Page (https://example.com/page)\n【turn0view0】 [wordlim: 200]\nL0: Failed to fetch https://example.com/page: 403 Forbidden",
		"Page (https://example.com/page)\n【turn0view0】 [wordlim: 200]\nL0: 404 Not Found",
		"Internal Error (https://example.com/page)\nL0: Not accessible",
		"Page (https://example.com/page)\n【turn0view0】 [wordlim: 200] Source: open; Total lines: 0",
	}
	for _, output := range outputs {
		if body, ok, err := AdaptResponse(alphaJSON(t, output, []any{}), r); err == nil || ok || body != nil {
			t.Fatalf("failed open succeeded: %s", output)
		}
	}
	if _, _, err := AdaptResponse(alphaJSON(t, "", []any{map[string]any{"url": r.URL, "error": "open failed"}}), r); err == nil {
		t.Fatal("ignored row error")
	}
}

func TestTextAndTotalOutputAreBounded(t *testing.T) {
	r := mustRequest(t, "fetch", `{"url":"https://example.com/page"}`)
	raw := alphaJSON(t, "Page (https://example.com/page)\n【turn0view0】 [wordlim: 200]\nL0: "+strings.Repeat("界🌍", 20000), []any{})
	body, ok, err := AdaptResponse(raw, r)
	if err != nil || !ok {
		t.Fatal(err)
	}
	var fetch fetchResponse
	_ = json.Unmarshal(body, &fetch)
	if !fetch.Truncated || !fetch.Partial || len(fetch.Content) > maxFieldBytes || !utf8.Valid(body) {
		t.Fatal("fetch truncation invalid")
	}
	r = mustRequest(t, "search", `{"query":"x","count":50,"need_content":true}`)
	rows := make([]any, 50)
	for i := range rows {
		rows[i] = map[string]any{"title": "title", "url": fmt.Sprintf("https://example.com/%d", i), "content": strings.Repeat("\x00", 30000), "summary": strings.Repeat("𠮷", 1000)}
	}
	raw = alphaJSON(t, "", rows)
	if len(raw) <= MaxInputBytes {
		t.Fatal("oversized test fixture unexpectedly small")
	}
	// Stay within the input bound while expanding substantially under JSON escaping.
	for i := range rows {
		row, ok := rows[i].(map[string]any)
		if !ok {
			t.Fatal("invalid response fixture row")
		}
		row["content"] = strings.Repeat("\x00", 4000)
	}
	raw = alphaJSON(t, "", rows)
	if len(raw) > MaxInputBytes {
		t.Fatal("bad bounded test fixture")
	}
	got, usable := readSearch(t, raw, r)
	if !usable || !got.Truncated || len(got.Results) > 50 {
		t.Fatal("aggregate budget not enforced")
	}
	body, _, err = AdaptResponse(raw, r)
	if err != nil || len(body) > maxOutputBytes || !utf8.Valid(body) {
		t.Fatalf("unbounded output %d: %v", len(body), err)
	}
}

func TestNativeTruncationMarkdownAndMixedFailures(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"x","need_content":true}`)
	text := "[Readable title](https://example.com/ok)\n【turn0search0】 [wordlim: 200] Crawled: today\n\nA readable excerpt.\n[truncated]\n\nFailed page (https://example.com/bad)\nL0: Failed to fetch this page\n"
	got, ok := readSearch(t, alphaJSON(t, text, []any{}), r)
	if !ok || len(got.Results) != 1 || !got.Truncated || !got.Results[0].Truncated || strings.Contains(got.Results[0].Content, "turn0search") {
		t.Fatal(got)
	}
	// Quoted page title lines prefixed by a line number cannot spawn new sources.
	text = "Page (https://example.com/ok)\nL0: Quoted source (https://example.org/quote)\nL1: page text"
	got, _ = readSearch(t, alphaJSON(t, text, []any{}), r)
	if len(got.Results) != 1 || !strings.Contains(got.Results[0].Content, "Quoted source") {
		t.Fatal(got)
	}
}

func FuzzParseAndAdapt(f *testing.F) {
	f.Add([]byte(`{"query":"seed"}`))
	f.Add([]byte(`{"results":[]}`))
	f.Add([]byte(`{"output":"Error: no service"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxInputBytes+1 {
			return
		}
		if r, err := ParseRequest("search", raw); err == nil {
			if _, err := r.AlphaBody("m"); err != nil {
				t.Fatalf("parsed request failed alpha encoding: %v", err)
			}
		}
		r := &Request{Operation: "search", Query: "seed", Count: 10}
		body, ok, err := AdaptResponse(raw, r)
		if err != nil && (body != nil || ok) {
			t.Fatal("error returned successful data")
		}
		if err == nil && (!json.Valid(body) || len(body) > maxOutputBytes) {
			t.Fatal("invalid or unbounded output")
		}
	})
}

func TestOpenSafetyAndStatusFailures(t *testing.T) {
	r := mustRequest(t, "fetch", `{"url":"https://example.com/page"}`)
	for _, text := range []string{
		"URL https://example.com/page is not safe to open (non-retryable error)",
		"HTTP Error 403: Forbidden", "Timeout while fetching the page", "The webpage could not be opened", "This page is unavailable", "Redirecting to non-allowed URL",
	} {
		raw := alphaJSON(t, "Page (https://example.com/page)\nL0: "+text, []any{})
		if body, ok, err := AdaptResponse(raw, r); err == nil || body != nil || ok {
			t.Fatalf("accepted open error: %s", text)
		}
	}
	for _, raw := range []string{`{"code":500,"results":[]}`, `{"status":403,"results":[]}`} {
		if _, _, err := AdaptResponse([]byte(raw), r); err == nil {
			t.Fatal("accepted error status")
		}
	}
}

func TestErrorHandlingArticleIsNotAnError(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"errors","need_content":true}`)
	text := "Error handling in Go (https://example.com/article)\n【turn0search0】 [wordlim: 200]\n\nError handling is an essential programming technique."
	got, ok := readSearch(t, alphaJSON(t, text, []any{}), r)
	if !ok || len(got.Results) != 1 || got.Results[0].Content != "Error handling is an essential programming technique." {
		t.Fatal(got)
	}
}
