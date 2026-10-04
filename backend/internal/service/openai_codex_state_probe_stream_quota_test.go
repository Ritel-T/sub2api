package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexStateProbe200StreamQuotaIsRateLimited(t *testing.T) {
	for _, code := range []string{"usage_limit_reached", "rate_limit_exceeded", "server_is_overloaded"} {
		t.Run(code, func(t *testing.T) {
			shot, err := fireOpenAICodexStateShotRequest(context.Background(), make(http.Header), "gpt-6-astra", "", "", func(*http.Request) (*http.Response, error) {
				wire := `data: {"type":"response.failed","response":{"status":"failed","error":{"code":"` + code + `","message":"fixture upstream failure"}}}` + "\n\n"
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(wire))}, nil
			})
			require.NoError(t, err)
			require.Error(t, shot.streamErr)
			result := &OpenAICodexStateProbeResult{}
			require.False(t, result.shotUsable(context.Background(), "fixture", shot, err))
			want := OpenAICodexStateFailureRateLimited
			if code == "server_is_overloaded" {
				want = OpenAICodexStateFailureStreamError
			}
			require.Equal(t, want, result.Failure)
			require.Equal(t, OpenAICodexStateInconclusive, result.Verdict)
		})
	}
}
