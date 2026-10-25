// Package deepseektools adapts DeepSeek search/fetch requests to standalone
// OpenAI alpha/search. It has no transport, service or third-party dependencies.
package deepseektools

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxInputBytes bounds both request and upstream JSON before decoding.
	MaxInputBytes = 2 << 20
	maxURLBytes   = 8192
)

// Request is a validated DeepSeek tools request. Operation is "search" or
// "fetch". Count defaults to 10; NeedContent defaults to false for search and true for fetch. An absent or
// null ori_question is represented by an empty OriginalQuestion.
type Request struct {
	Operation        string
	Query            string
	Count            int
	NeedContent      bool
	URL              string
	OriginalQuestion string
	Localization     string
}

// ParseRequest accepts one JSON object. Unknown fields are ignored; known fields
// are strictly typed, even when irrelevant to the selected operation.
func ParseRequest(operation string, raw []byte) (*Request, error) {
	if operation != "search" && operation != "fetch" {
		return nil, errors.New("operation must be search or fetch")
	}
	obj, err := object(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	r := &Request{Operation: operation, Count: 10, NeedContent: operation == "fetch"}
	for _, field := range []struct {
		name     string
		dst      *string
		nullable bool
	}{
		{"query", &r.Query, false}, {"url", &r.URL, false},
		{"ori_question", &r.OriginalQuestion, true}, {"localization", &r.Localization, false},
	} {
		v, ok := obj[field.name]
		if !ok {
			continue
		}
		if field.nullable && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			continue
		}
		if len(v) == 0 || v[0] != '"' {
			return nil, fmt.Errorf("%s must be a string", field.name)
		}
		if err := json.Unmarshal(v, field.dst); err != nil {
			return nil, fmt.Errorf("invalid %s", field.name)
		}
		if (field.name == "ori_question" || field.name == "query") && !validQuestion(*field.dst) {
			return nil, fmt.Errorf("%s must contain 1-128 nonblank Unicode characters", field.name)
		}
	}
	if v, ok := obj["count"]; ok {
		if err := json.Unmarshal(v, &r.Count); err != nil || bytes.Equal(v, []byte("null")) {
			return nil, errors.New("count must be an integer from 1 to 50")
		}
	}
	if v, ok := obj["need_content"]; ok {
		if !bytes.Equal(v, []byte("true")) && !bytes.Equal(v, []byte("false")) {
			return nil, errors.New("need_content must be a boolean")
		}
		r.NeedContent = bytes.Equal(v, []byte("true"))
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Request) validate() error {
	if r == nil {
		return errors.New("request is nil")
	}
	if r.Operation != "search" && r.Operation != "fetch" {
		return errors.New("operation must be search or fetch")
	}
	if r.Count < 1 || r.Count > 50 {
		return errors.New("count must be an integer from 1 to 50")
	}
	if r.Operation == "search" && !validQuestion(r.Query) {
		return errors.New("query must contain 1-128 nonblank Unicode characters")
	}
	if r.Query != "" && !validQuestion(r.Query) {
		return errors.New("invalid query")
	}
	if r.OriginalQuestion != "" && !validQuestion(r.OriginalQuestion) {
		return errors.New("invalid ori_question")
	}
	if r.Localization != "" {
		if _, err := netip.ParseAddr(r.Localization); err != nil || strings.Contains(r.Localization, "%") {
			return errors.New("localization must be an IP address or empty")
		}
	}
	if r.Operation == "fetch" || r.URL != "" {
		if err := validateURL(r.URL); err != nil {
			return err
		}
	}
	return nil
}

func validQuestion(s string) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= 1 && n <= 128 && strings.TrimSpace(s) != ""
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) > MaxInputBytes {
		return nil, errors.New("JSON exceeds 2 MiB")
	}
	if !utf8.Valid(raw) || !validUnicodeEscapes(raw) {
		return nil, errors.New("JSON must be UTF-8")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New("malformed JSON object")
	}
	if obj == nil {
		return nil, errors.New("JSON object is required")
	}
	return obj, nil
}

// validateURL only checks literal targets; DNS resolution, rebinding and redirect
// enforcement belong to the transport. No network I/O takes place in this package.
func validateURL(raw string) error {
	if len(raw) == 0 || len(raw) > maxURLBytes || !utf8.ValidString(raw) || strings.TrimSpace(raw) != raw {
		return errors.New("url must be a nonempty HTTP(S) URL of at most 8192 bytes")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.Host == "" || u.User != nil {
		return errors.New("url must have an HTTP(S) host and no userinfo")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || strings.ContainsAny(host, "%\\") {
		return errors.New("invalid url host")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("invalid url port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return errors.New("invalid url port")
	}
	if a, e := netip.ParseAddr(host); e == nil {
		a = a.Unmap()
		if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() || netip.MustParsePrefix("::/96").Contains(a) || netip.MustParsePrefix("100.64.0.0/10").Contains(a) || netip.MustParsePrefix("0.0.0.0/8").Contains(a) || netip.MustParsePrefix("198.18.0.0/15").Contains(a) || netip.MustParsePrefix("240.0.0.0/4").Contains(a) {
			return errors.New("url targets a nonpublic IP address")
		}
		return nil
	}
	if strings.ContainsAny(host, ":[]") {
		return errors.New("invalid url IP literal")
	}
	if host == "localhost" || host == "metadata" || host == "instance-data" || strings.HasPrefix(host, "metadata.") || strings.HasPrefix(host, "instance-data.") {
		return errors.New("url targets a local or metadata host")
	}
	for _, s := range []string{".localhost", ".local", ".internal", ".localdomain", ".home", ".lan"} {
		if strings.HasSuffix(host, s) {
			return errors.New("url targets a local host")
		}
	}
	numeric := true
	for _, c := range host {
		if c != '.' && (c < '0' || c > '9') {
			numeric = false
			break
		}
	}
	if numeric || strings.HasPrefix(host, "0x") {
		return errors.New("ambiguous numeric url host")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid url host")
		}
		for _, c := range label {
			if c != '-' && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
				return errors.New("invalid url host")
			}
		}
		if strings.HasPrefix(label, "0x") {
			return errors.New("ambiguous numeric url host")
		}
	}
	if len(host) > 253 {
		return errors.New("invalid url host")
	}
	return nil
}

// AlphaBody builds a fresh, randomly identified standalone alpha/search request.
// OriginalQuestion is context, not a replacement for Query. Localization remains
// a validated compatibility hint: an IP is not fabricated into a geographic
// OpenAI user_location. Count is enforced when adapting results.
func (r *Request) AlphaBody(model string) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if !utf8.ValidString(model) || strings.TrimSpace(model) == "" || len(model) > 512 {
		return nil, errors.New("model must be a nonempty string of at most 512 bytes")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, fmt.Errorf("generate alpha request id: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	h := hex.EncodeToString(id[:])
	uuid := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	commands := map[string]any{"response_length": "long"}
	text := r.Query
	if r.Operation == "search" {
		commands["search_query"] = []any{map[string]string{"q": r.Query}}
	} else {
		commands["open"] = []any{map[string]string{"ref_id": r.URL}}
		text = "Open the requested URL and return the readable page text with its source title and URL: " + r.URL
	}
	if r.OriginalQuestion != "" {
		text += "\nOriginal question: " + r.OriginalQuestion
	}
	return json.Marshal(map[string]any{
		"id": uuid, "model": model, "commands": commands,
		"input":    []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]string{"type": "input_text", "text": text}}}},
		"settings": map[string]any{"allowed_callers": []string{"direct"}, "external_web_access": true},
	})
}

// encoding/json replaces unpaired UTF-16 surrogates with U+FFFD. Reject those
// escapes instead of silently changing a caller's Unicode query or page text.
func validUnicodeEscapes(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
