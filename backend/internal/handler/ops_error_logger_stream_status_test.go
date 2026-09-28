package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsErrorLoggerMiddleware_StreamFailurePreservesMatchingIntendedStatus(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		message        string
		explicitStatus int
		marker         *service.OpsStreamError
		want           int
	}{
		{name: "BPS limit after heartbeat", code: "basispoints_rate_limited", message: "Excel BPS rate limit exceeded", marker: &service.OpsStreamError{ErrType: "basispoints_rate_limited", Message: "Excel BPS rate limit exceeded", IntendedStatus: 429}, want: 429},
		{name: "marker with separate code", code: "basispoints_rate_limited", message: "Excel BPS rate limit exceeded", marker: &service.OpsStreamError{ErrType: "upstream_error", Code: "basispoints_rate_limited", Message: "Excel BPS rate limit exceeded", IntendedStatus: 429}, want: 429},
		{name: "earlier attempt marker", code: "context_length_exceeded", message: "input exceeds the context window", marker: &service.OpsStreamError{ErrType: "upstream_error", Message: "earlier attempt failed", IntendedStatus: 502}, want: 400},
		{name: "same code different message", code: "basispoints_rate_limited", message: "current failure", marker: &service.OpsStreamError{ErrType: "basispoints_rate_limited", Message: "earlier failure", IntendedStatus: 429}, want: 502},
		{name: "unknown unmarked failure", code: "unrecognized_failure", message: "request failed", want: 502},
		{name: "terminal status takes priority", code: "basispoints_rate_limited", message: "Excel BPS rate limit exceeded", explicitStatus: 503, marker: &service.OpsStreamError{ErrType: "basispoints_rate_limited", Message: "Excel BPS rate limit exceeded", IntendedStatus: 429}, want: 503},
		{name: "invalid intended status", code: "basispoints_rate_limited", message: "Excel BPS rate limit exceeded", marker: &service.OpsStreamError{ErrType: "basispoints_rate_limited", Message: "Excel BPS rate limit exceeded", IntendedStatus: 200}, want: 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			gin.SetMode(gin.TestMode)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(c *gin.Context) {
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString(": ping\n\n")
				c.Writer.Flush()
				service.SetOpsUpstreamError(c, http.StatusTooManyRequests, tt.message, "")
				if tt.marker != nil {
					service.MarkOpsStreamErrorValue(c, *tt.marker)
				}
				detail := map[string]any{"code": tt.code, "message": tt.message}
				if tt.explicitStatus != 0 {
					detail["status_code"] = tt.explicitStatus
				}
				payload, err := json.Marshal(map[string]any{"type": "response.failed", "response": map[string]any{"status": "failed", "error": detail}})
				require.NoError(t, err)
				_, _ = c.Writer.WriteString("event: response.failed\ndata: ")
				_, _ = c.Writer.Write(payload[:len(payload)/2])
				_, _ = c.Writer.Write(payload[len(payload)/2:])
				_, _ = c.Writer.WriteString("\n\n")
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed"))
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			job := <-opsErrorLogQueue
			require.Equal(t, tt.want, job.entry.StatusCode)
			require.NotNil(t, job.entry.UpstreamStatusCode)
			require.Equal(t, tt.want, *job.entry.UpstreamStatusCode)
		})
	}
}
