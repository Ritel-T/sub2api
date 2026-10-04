package service

import (
	"crypto/sha256"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type borrowQualityProbeUpstream struct {
	HTTPUpstream
	call func(*http.Request) (*http.Response, error)
}

func (u borrowQualityProbeUpstream) DoWithTLS(r *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.call(r)
}

func TestBorrowRouteQualityRequiresSameRouteModelAndCorrectCandy(t *testing.T) {
	require.Equal(t, GatewayBorrowCandyPromptSHA256, fmt.Sprintf("%x", sha256.Sum256([]byte(GatewayBorrowCandyPrompt))))
	for _, test := range []struct {
		name, model, answer, terminal string
		status                        int
		pass                          bool
	}{
		{"Astra correct", "gpt-6-astra", "21", "gpt-6-astra", 200, true},
		{"Sol correct", "gpt-6.1-sol", "21", "gpt-6.1-sol", 200, true},
		{"wrong answer", "gpt-6.1-sol", "29", "gpt-6.1-sol", 200, false},
		{"created model is not terminal", "gpt-6.1-sol", "21", "gpt-6-luna", 200, false},
		{"quota rejected", "gpt-6-astra", "", "gpt-6-astra", 429, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			shots := 0
			upstream := borrowQualityProbeUpstream{call: func(r *http.Request) (*http.Response, error) {
				shots++
				raw, _ := io.ReadAll(r.Body)
				require.Contains(t, r.Header.Get("Cookie"), "__oailb=fixture-route")
				require.Equal(t, "Bearer fixture-target", r.Header.Get("Authorization"))
				h := http.Header{"Content-Type": {"text/event-stream"}}
				h.Set("X-Codex-Turn-State", "fixture-first")
				if shots == 2 {
					h.Del("X-Codex-Turn-State")
				}
				answer, terminal, status := "OK", test.model, 200
				if shots == 3 {
					require.Contains(t, string(raw), "只输出最终整数")
					answer, terminal, status = test.answer, test.terminal, test.status
				}
				wire := `data: {"type":"response.created","response":{"model":"` + test.model + `"}}` + "\n\n" +
					`data: {"type":"response.completed","response":{"model":"` + terminal + `","status":"completed","output":[{"content":[{"type":"output_text","text":"` + answer + `"}]}]}}` + "\n\n"
				if status == 429 {
					wire = `{"error":{"code":"usage_limit_reached"}}`
				}
				return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(wire))}, nil
			}}
			req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
			req.Header.Set("Authorization", "Bearer fixture-target")
			req.Header.Set("Cookie", "__oailb=fixture-route")
			result := ProbeOpenAICodexBorrowQualityRoute(t.Context(), upstream, req, "fixture-proxy", 300, 1, nil, test.model)
			require.Equal(t, 3, shots)
			require.Equal(t, test.pass, result.Verdict == OpenAICodexStateHealthy)
		})
	}
}
