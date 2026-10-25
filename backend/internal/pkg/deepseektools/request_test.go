package deepseektools

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustRequest(t *testing.T, op, raw string) *Request {
	t.Helper()
	r, err := ParseRequest(op, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseRequestDefaultsAndUnicode(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"你好 🌍","ori_question":null,"localization":"2001:db8::1","future":{"ignored":true}}`)
	if r.Query != "你好 🌍" || r.Count != 10 || r.NeedContent || r.OriginalQuestion != "" || r.Localization != "2001:db8::1" {
		t.Fatalf("unexpected request: %+v", r)
	}
	r = mustRequest(t, "search", `{"query":"`+strings.Repeat("界", 128)+`","count":50,"need_content":true,"ori_question":"你好","localization":"8.8.8.8"}`)
	if r.Count != 50 || !r.NeedContent {
		t.Fatal(r)
	}
	fetch := mustRequest(t, "fetch", `{"url":"https://example.com:8443/page?q=hello#part"}`)
	if !fetch.NeedContent {
		t.Fatal("fetch must return page content by default")
	}
}

func TestParseRequestRejectsInvalidKnownFields(t *testing.T) {
	cases := []string{
		``, `null`, `[]`, `"x"`, `{`, `{} {}`, `{"query":"x"}null`, `{}`,
		`{"query":null}`, `{"query":12}`, `{"query":true}`, `{"query":[]}`, `{"query":""}`, `{"query":" \n\t　"}`,
		`{"query":"` + strings.Repeat("🌍", 129) + `"}`,
		`{"query":"x","count":0}`, `{"query":"x","count":51}`, `{"query":"x","count":-1}`, `{"query":"x","count":1.0}`, `{"query":"x","count":1e1}`, `{"query":"x","count":"10"}`, `{"query":"x","count":null}`, `{"query":"x","count":true}`, `{"query":"x","count":9999999999999999999999999999}`,
		`{"query":"x","need_content":1}`, `{"query":"x","need_content":"true"}`, `{"query":"x","need_content":null}`,
		`{"query":"x","ori_question":""}`, `{"query":"x","ori_question":"　"}`, `{"query":"x","ori_question":1}`, `{"query":"x","ori_question":"` + strings.Repeat("界", 129) + `"}`,
		`{"query":"x","localization":"localhost"}`, `{"query":"x","localization":"1.2.3"}`, `{"query":"x","localization":"1.2.3.4/24"}`, `{"query":"x","localization":"fe80::1%eth0"}`, `{"query":"x","localization":null}`, `{"query":"x","url":false}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if r, err := ParseRequest("search", []byte(raw)); err == nil {
				t.Fatalf("accepted invalid request: %+v", r)
			}
		})
	}
	if _, err := ParseRequest("SEARCH", []byte(`{"query":"x"}`)); err == nil {
		t.Fatal("accepted unknown operation")
	}
	if _, err := ParseRequest("search", append([]byte(`{"query":"`), 0xff, '"', '}')); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	if _, err := ParseRequest("search", []byte(`{"query":"x","future":"`+strings.Repeat("a", MaxInputBytes)+`"}`)); err == nil {
		t.Fatal("accepted oversized request")
	}
}

func TestFetchURLValidation(t *testing.T) {
	bad := []string{
		"", "ftp://example.com/a", "file:///etc/passwd", "/relative", "https:///page", "https://user:pw@example.com", "https://user@example.com", "https://example.com:0", "https://example.com:65536", "https://example.com:", "https://example.com:abc", "http://[::1", "http://example.com/%zz", "https://bad_host.example", "http://a..com", "http://-host.example", "http://example.com\\@127.0.0.1", " https://example.com", "http://example.com\n", "https://example.com/" + strings.Repeat("x", 8192),
		"http://localhost", "http://LOCALHOST.", "http://sub.localhost", "http://router.local", "http://host.internal", "http://host.lan", "http://metadata.google.internal", "http://metadata", "http://metadata.azure.internal", "http://instance-data.ec2.internal",
		"http://127.0.0.1", "http://127.1", "http://2130706433", "http://0x7f000001", "http://0177.0.0.1", "http://0x7f.0.0.1", "http://0.0.0.0", "http://0.2.3.4", "http://10.0.0.1", "http://172.16.1.1", "http://192.168.1.1", "http://169.254.169.254", "http://100.100.100.200", "http://100.64.0.1", "http://198.18.0.1", "http://224.0.0.1", "http://255.255.255.255", "http://[::1]", "http://[::]", "http://[fd00::1]", "http://[fe80::1]", "http://[::ffff:127.0.0.1]", "http://[fe80::1%25eth0]",
	}
	for _, u := range bad {
		t.Run(u, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"url": u})
			if _, err := ParseRequest("fetch", raw); err == nil {
				t.Fatalf("accepted URL %q", u)
			}
		})
	}
	for _, u := range []string{"http://example.com", "https://example.com/a?q=1", "https://8.8.8.8/", "https://[2606:4700:4700::1111]:443/"} {
		raw, _ := json.Marshal(map[string]string{"url": u})
		if _, err := ParseRequest("fetch", raw); err != nil {
			t.Fatalf("rejected public URL %s: %v", u, err)
		}
	}
	if _, err := ParseRequest("fetch", []byte(`{}`)); err == nil {
		t.Fatal("fetch requires URL")
	}
}

func TestAlphaBodySchemaAndIsolation(t *testing.T) {
	r := mustRequest(t, "search", `{"query":"你好","ori_question":"why","localization":"8.8.8.8","count":3}`)
	raw, err := r.AlphaBody("gpt-search")
	if err != nil {
		t.Fatal(err)
	}
	var obj struct {
		ID       string `json:"id"`
		Model    string `json:"model"`
		Commands struct {
			Queries []struct {
				Q string `json:"q"`
			} `json:"search_query"`
			Open []struct {
				Ref string `json:"ref_id"`
			} `json:"open"`
			Length string `json:"response_length"`
		} `json:"commands"`
		Input []struct {
			Type, Role string
			Content    []struct{ Type, Text string }
		} `json:"input"`
		Settings struct {
			Allowed  []string `json:"allowed_callers"`
			External bool     `json:"external_web_access"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if len(obj.ID) != 36 || obj.Model != "gpt-search" || len(obj.Commands.Queries) != 1 || obj.Commands.Queries[0].Q != "你好" || obj.Commands.Length != "long" || len(obj.Commands.Open) != 0 {
		t.Fatalf("bad alpha commands: %s", raw)
	}
	if len(obj.Input) != 1 || obj.Input[0].Type != "message" || obj.Input[0].Role != "user" || obj.Input[0].Content[0].Type != "input_text" || !strings.Contains(obj.Input[0].Content[0].Text, "why") || !obj.Settings.External || len(obj.Settings.Allowed) != 1 || obj.Settings.Allowed[0] != "direct" {
		t.Fatalf("bad input/settings: %s", raw)
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["store"]; ok {
		t.Fatal("store leaked into alpha schema")
	}
	if strings.Contains(string(raw), "user_location") {
		t.Fatal("IP must not become fabricated geographic data")
	}
	next, _ := r.AlphaBody("gpt-search")
	var second map[string]any
	_ = json.Unmarshal(next, &second)
	if second["id"] == obj.ID {
		t.Fatal("request id reused")
	}
	fetch := mustRequest(t, "fetch", `{"url":"https://example.com/page"}`)
	raw, err = fetch.AlphaBody("m")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &obj)
	if len(obj.Commands.Open) != 1 || obj.Commands.Open[0].Ref != "https://example.com/page" {
		t.Fatalf("bad open: %s", raw)
	}
	if _, err := r.AlphaBody(" "); err == nil {
		t.Fatal("accepted blank model")
	}
	if _, err := (*Request)(nil).AlphaBody("m"); err == nil {
		t.Fatal("accepted nil request")
	}
	r.URL = "http://localhost"
	if _, err := r.AlphaBody("m"); err == nil {
		t.Fatal("did not revalidate mutable request")
	}
}

func TestUnicodeBoundedText(t *testing.T) {
	budget := 7
	s, cut := boundedText("界🌍界", 100, &budget)
	if s != "界🌍" || !cut || budget != 0 || !utf8.ValidString(s) {
		t.Fatalf("bad boundary %q %t %d", s, cut, budget)
	}
	budget = 6
	s, cut = boundedText("界🌍界", 100, &budget)
	if s != "界" || !cut || budget != 3 {
		t.Fatal("split a Unicode character")
	}
}

func TestEscapedUnicodeValidation(t *testing.T) {
	for _, raw := range []string{`{"query":"\ud800"}`, `{"query":"\udfff"}`, `{"query":"\ud800x"}`, `{"query":"\ud800\u0041"}`} {
		if _, err := ParseRequest("search", []byte(raw)); err == nil {
			t.Fatalf("accepted unpaired surrogate %s", raw)
		}
	}
	for _, raw := range []string{`{"query":"\ud83c\udf0d"}`, `{"query":"\\ud800"}`, `{"query":"\u4f60\u597d"}`} {
		if _, err := ParseRequest("search", []byte(raw)); err != nil {
			t.Fatalf("rejected valid escapes %s: %v", raw, err)
		}
	}
}
