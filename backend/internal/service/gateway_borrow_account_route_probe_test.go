package service

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBorrowAccountQualityAstraOnlyAndOnePlusThree(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prior    bool
		answers  []string
		status   int
		terminal string
		changed  bool
		pass     bool
		want     int
	}{
		{name: "first complete four", answers: []string{"21", "21", "29", "21"}, pass: true, want: 4},
		{name: "first two correct fails", answers: []string{"21", "29", "21", "29"}, want: 4},
		{name: "renewal immediate pass", prior: true, answers: []string{"21"}, pass: true, want: 1},
		{name: "renewal expands after wrong", prior: true, answers: []string{"29", "21", "21", "21"}, pass: true, want: 4},
		{name: "renewal one correct fails", prior: true, answers: []string{"29", "29", "21", "29"}, want: 4},
		{name: "stream quota is inconclusive", answers: []string{"21"}, status: 429, want: 1},
		{name: "different terminal fails", answers: []string{"21"}, terminal: "gpt-6-luna", want: 1},
		{name: "replacement route fails", answers: []string{"21"}, changed: true, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := borrowQualityProbeUpstream{call: func(req *http.Request) (*http.Response, error) {
				calls++
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(raw, "model").String())
				require.Equal(t, "medium", gjson.GetBytes(raw, "reasoning.effort").String())
				require.Equal(t, GatewayBorrowCandyPrompt, gjson.GetBytes(raw, "input.0.content.0.text").String())
				require.Empty(t, req.Header.Get("X-Codex-Turn-State"))
				require.Equal(t, "Bearer fixture-target", req.Header.Get("Authorization"))
				require.Equal(t, "__oailb=fixture", req.Header.Get("Cookie"))
				h := http.Header{"Content-Type": {"text/event-stream"}, "X-Codex-Turn-State": {fmt.Sprintf("resigned-%d", calls)}}
				if tc.changed {
					h.Set("Set-Cookie", "__oailb=changed; Path=/; Secure; Max-Age=230")
				}
				status := tc.status
				if status == 0 {
					status = 200
				}
				terminal := tc.terminal
				if terminal == "" {
					terminal = "gpt-6-astra"
				}
				wire := `data: {"type":"response.completed","response":{"model":"` + terminal + `","status":"completed","output":[{"content":[{"type":"output_text","text":"` + tc.answers[calls-1] + `"}]}]}}` + "\n\n"
				if status == 429 {
					wire = `{"error":{"code":"usage_limit_reached"}}`
				}
				return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(wire))}, nil
			}}
			req, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/responses", nil)
			req.Header.Set("Authorization", "Bearer fixture-target")
			req.Header.Set("X-Codex-Turn-State", "business-state")
			req.Header.Set("Cookie", "__oailb=fixture")
			result := ProbeOpenAICodexBorrowAccountQualityRoute(t.Context(), upstream, req, "fixture-proxy", 300, 1, nil, tc.prior)
			require.Equal(t, tc.want, calls)
			require.Equal(t, tc.want, result.Attempts)
			if tc.pass {
				require.GreaterOrEqual(t, result.Correct, 1)
				require.Equal(t, "21", result.Answer)
			}
			require.Equal(t, tc.pass, result.Verdict == OpenAICodexStateHealthy)
			require.Equal(t, "gpt-6-astra", result.Model)
			require.False(t, result.FinishedAt.IsZero())
		})
	}
}
