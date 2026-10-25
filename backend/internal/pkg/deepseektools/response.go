package deepseektools

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSources       = 256
	maxFieldBytes    = 64 << 10
	maxReadableBytes = 128 << 10
	maxOutputBytes   = 1 << 20
)

type source struct {
	title, url, ref, snippet, summary, content string
	truncated                                  bool
}

type searchResult struct {
	RerankScore *float64 `json:"rerank_score"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Snippet     string   `json:"snippet"`
	Summary     string   `json:"summary"`
	Content     string   `json:"content"`
	Date        *string  `json:"date"`
	SiteName    string   `json:"site_name"`
	SiteIcon    string   `json:"site_icon"`
	PublishTime *string  `json:"publish_time"`
	Authority   int      `json:"authority"`
	Partial     bool     `json:"partial"`
	Truncated   bool     `json:"truncated"`
}

type searchResponse struct {
	Results   []searchResult `json:"results"`
	Provider  string         `json:"provider"`
	Truncated bool           `json:"truncated"`
	Partial   bool           `json:"partial"`
}

type fetchResponse struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	URL       string `json:"url"`
	Provider  string `json:"provider"`
	Truncated bool   `json:"truncated"`
	Partial   bool   `json:"partial"`
}

// AdaptResponse returns only whitelisted readable fields; encrypted_output is
// never inspected or copied. The bool reports a usable result (nonempty search
// results, or fetched page text). A confirmed empty search returns false, nil.
// Errors return nil, false, error. All page text is marked partial: alpha excerpts
// and PAT citations cannot establish that a whole page was fetched. Truncated
// reports actual upstream/local clipping or count-limited omitted results.
type ResponseBackend uint8

const (
	BackendAuto ResponseBackend = iota
	BackendNativeAlpha
	BackendResponsesWebSearch
)

func AdaptResponse(raw []byte, r *Request) ([]byte, bool, error) {
	return adaptResponse(raw, r, BackendAuto)
}

// AdaptResponseFrom uses the actual request transport. A text_result row is a
// citation in both native alpha and Responses; its type is not provenance.
func AdaptResponseFrom(raw []byte, r *Request, backend ResponseBackend) ([]byte, bool, error) {
	if backend != BackendNativeAlpha && backend != BackendResponsesWebSearch {
		return nil, false, errors.New("unknown search backend")
	}
	return adaptResponse(raw, r, backend)
}

func adaptResponse(raw []byte, r *Request, backend ResponseBackend) ([]byte, bool, error) {
	if err := r.validate(); err != nil {
		return nil, false, err
	}
	obj, err := object(raw)
	if err != nil {
		return nil, false, fmt.Errorf("invalid alpha response: %w", err)
	}
	if upstreamFailed(obj) {
		return nil, false, errors.New("alpha returned an upstream error or unsuccessful status")
	}
	output, err := stringField(obj, "output")
	if err != nil {
		return nil, false, err
	}
	var entries []json.RawMessage
	explicitEmpty := false
	if v, ok := obj["results"]; ok && strings.TrimSpace(string(v)) != "null" {
		if err := json.Unmarshal(v, &entries); err != nil {
			return nil, false, errors.New("alpha results must be an array")
		}
		explicitEmpty = len(entries) == 0
	}
	sources := make([]source, 0)
	pat := false
	clipped := jsonBool(obj, "truncated")
	for i, v := range entries {
		if i >= maxSources {
			clipped = true
			break
		}
		item, e := object(v)
		if e != nil {
			return nil, false, errors.New("alpha result must be an object")
		}
		if upstreamFailed(item) {
			if r.Operation == "fetch" {
				return nil, false, errors.New("alpha page open failed")
			}
			clipped = true
			continue
		}
		typ, e := stringField(item, "type")
		if e != nil {
			return nil, false, e
		}
		if typ == "text_result" {
			pat = true
		}
		s := source{truncated: jsonBool(item, "truncated")}
		for _, f := range []struct {
			name string
			dst  *string
		}{{"title", &s.title}, {"url", &s.url}, {"ref_id", &s.ref}, {"snippet", &s.snippet}, {"summary", &s.summary}, {"content", &s.content}} {
			value, e := stringField(item, f.name)
			if e != nil {
				return nil, false, e
			}
			*f.dst = value
		}
		if s.url == "" && validateURL(s.ref) == nil {
			s.url = s.ref
		}
		if s.content == "" && typ != "text_result" {
			s.content, e = stringField(item, "text")
			if e != nil {
				return nil, false, e
			}
		}
		// PAT text_result rows are citations. Even if a future backend echoes its
		// model answer into content, it is not evidence of fetched page text.
		if typ == "text_result" {
			s.content = ""
		}
		if s.url != "" && validateURL(s.url) == nil {
			if looksError(s.content) || looksError(s.snippet) {
				if r.Operation == "fetch" {
					return nil, false, errors.New("alpha page open failed")
				}
				clipped = true
				continue
			}
			sources = mergeSource(sources, s)
		} else if s.ref != "" && s.url == "" {
			// A citation reference can only be resolved using a readable native block.
			sources = append(sources, s)
		} else {
			clipped = true
		}
	}
	if backend == BackendNativeAlpha {
		pat = false
	}
	if backend == BackendResponsesWebSearch {
		pat = true
	}
	if looksError(output) {
		return nil, false, errors.New("alpha returned an error instead of readable results")
	}
	native, _, failed, nativeClipped := parseNative(output)
	if !pat {
		clipped = clipped || nativeClipped
		for _, s := range native {
			sources = mergeSource(sources, s)
		}
	}
	filtered := make([]source, 0, len(sources))
	for _, s := range sources {
		if validateURL(s.url) == nil {
			filtered = append(filtered, s)
		} else {
			clipped = true
		}
	}
	sources = filtered
	if r.Operation == "fetch" {
		if failed {
			return nil, false, errors.New("alpha page open failed")
		}
		// Prefer the requested page; do not silently return another cited page.
		for _, s := range sources {
			if !samePage(s.url, r.URL) || strings.TrimSpace(s.content) == "" {
				continue
			}
			budget := maxReadableBytes
			title, t1 := boundedText(s.title, 2048, &budget)
			content := ""
			t2 := false
			if r.NeedContent {
				content, t2 = boundedText(s.content, maxFieldBytes, &budget)
			}
			u, t3 := boundedText(s.url, maxURLBytes, &budget)
			body, e := marshalBounded(fetchResponse{Title: title, Content: content, URL: u, Provider: "openai", Partial: true, Truncated: clipped || s.truncated || t1 || t2 || t3})
			return body, e == nil, e
		}
		return nil, false, errors.New("alpha fetch did not return readable text for the requested page")
	}
	if len(sources) == 0 {
		if failed {
			return nil, false, errors.New("alpha search returned failed source blocks")
		}
		if (explicitEmpty && strings.TrimSpace(output) == "") || isEmptySearch(output) {
			body, e := marshalBounded(searchResponse{Results: []searchResult{}, Provider: "openai", Truncated: clipped, Partial: true})
			return body, false, e
		}
		return nil, false, errors.New("alpha search has no recognizable readable results")
	}
	if len(sources) > r.Count {
		sources = sources[:r.Count]
		clipped = true
	}
	results := make([]searchResult, 0, len(sources))
	budget := maxReadableBytes
	for _, s := range sources {
		// Stop before there is insufficient budget even to preserve the source URL.
		if len(s.url) > budget {
			clipped = true
			break
		}
		u, t0 := boundedText(s.url, maxURLBytes, &budget)
		title, t1 := boundedText(s.title, 2048, &budget)
		snippet := s.snippet
		if snippet == "" {
			snippet = s.content
		}
		snippet, t2 := boundedText(snippet, 4096, &budget)
		summary, t3 := boundedText(s.summary, 4096, &budget)
		content := ""
		t4 := false
		if r.NeedContent {
			content, t4 = boundedText(s.content, maxFieldBytes, &budget)
		}
		truncated := s.truncated || t0 || t1 || t2 || t3 || t4
		clipped = clipped || truncated
		results = append(results, searchResult{Title: title, URL: u, Snippet: snippet, Summary: summary, Content: content, Authority: -1, Partial: true, Truncated: truncated})
	}
	body, e := marshalBounded(searchResponse{Results: results, Provider: "openai", Truncated: clipped || failed, Partial: true})
	return body, len(results) > 0 && e == nil, e
}

func stringField(obj map[string]json.RawMessage, name string) (string, error) {
	raw, ok := obj[name]
	if !ok || strings.TrimSpace(string(raw)) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("alpha %s must be a string", name)
	}
	return s, nil
}

func jsonBool(obj map[string]json.RawMessage, name string) bool { return string(obj[name]) == "true" }

func upstreamFailed(obj map[string]json.RawMessage) bool {
	for _, key := range []string{"error", "errors", "error_code"} {
		if v, ok := obj[key]; ok {
			s := strings.TrimSpace(string(v))
			if s != "null" && s != "false" && s != "0" && s != "\"\"" && s != "[]" {
				return true
			}
		}
	}
	if string(obj["success"]) == "false" || string(obj["ok"]) == "false" {
		return true
	}
	for _, key := range []string{"status", "type"} {
		s, _ := stringField(obj, key)
		switch strings.ToLower(s) {
		case "error", "failed", "failure", "incomplete", "cancelled", "canceled", "response.failed", "response.incomplete":
			return true
		}
	}
	var code int
	if (json.Unmarshal(obj["status_code"], &code) == nil && code >= 400) || (json.Unmarshal(obj["code"], &code) == nil && code >= 400) || (json.Unmarshal(obj["status"], &code) == nil && code >= 400) {
		return true
	}
	return false
}

func mergeSource(list []source, s source) []source {
	for i := range list {
		dst := &list[i]
		if (s.url != "" && dst.url == s.url) || (s.ref != "" && dst.ref == s.ref) {
			if dst.url == "" {
				dst.url = s.url
			}
			if dst.title == "" {
				dst.title = s.title
			}
			if dst.ref == "" {
				dst.ref = s.ref
			}
			if dst.content == "" {
				dst.content = s.content
			}
			if dst.snippet == "" {
				dst.snippet = s.snippet
			}
			if dst.summary == "" {
				dst.summary = s.summary
			}
			dst.truncated = dst.truncated || s.truncated
			return list
		}
	}
	return append(list, s)
}

func boundedText(s string, limit int, budget *int) (string, bool) {
	s = strings.TrimSpace(s)
	if limit > *budget {
		limit = *budget
	}
	cut := len(s) > limit
	if cut {
		s = s[:limit]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	*budget -= len(s)
	return s, cut
}

func marshalBounded(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxOutputBytes {
		return nil, errors.New("adapted response exceeds output limit")
	}
	return raw, nil
}

func samePage(a, b string) bool {
	// Fragments and a bare trailing slash do not identify another fetched page.
	a = strings.SplitN(a, "#", 2)[0]
	b = strings.SplitN(b, "#", 2)[0]
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

var openFailureText = regexp.MustCompile(`(?i)^(?:url\s+https?://.*(?:not safe to open|non-retryable error)|http\s+error\b|timeout\s+(?:while|when|fetching)|(?:this|the)\s+(?:web)?page\s+(?:could not|cannot|can.t|is not available|is unavailable)|redirecting\s+to\s+(?:non-allowed|blocked))`)

var errorText = regexp.MustCompile(`(?i)^(?:error(?:\s*[:：]|\s*$|\s+(?:fetching|opening|loading|code)\b)|internal\s+(?:server\s+)?error|upstream\s+(?:error|failed)|(?:failed|unable)\s+to\s+(?:open|fetch|retrieve|access|load|search)|(?:cannot|could\s+not|can't)\s+(?:open|fetch|retrieve|access|load)|(?:http\s*)?(?:400|401|403|404|408|429|500|502|503|504)(?:\s|:|$)|access\s+denied|forbidden(?:\s|:|$)|rate\s+limit|no\s+web\s+access|robots\.txt|tool\s+(?:error|unavailable)|the\s+(?:requested\s+)?(?:url|page)\s+(?:could\s+not|cannot)|网页(?:打开|访问|抓取)失败|无法(?:打开|访问|抓取)|请求失败)`)

func looksError(s string) bool {
	s = strings.TrimSpace(s)
	return errorText.MatchString(s) || openFailureText.MatchString(s)
}

func isEmptySearch(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	switch strings.TrimRight(s, ".!。") {
	case "no results", "no results found", "no search results", "no search results found", "0 results", "未找到结果", "没有搜索结果":
		return true
	}
	return false
}
