package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestExcelBPSNativePreUploadKeepaliveAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		stream, beat bool
		status       int
	}{
		{"slow success", true, true, 200}, {"slow failure", true, true, 503},
		{"fast rate limit", true, false, 429}, {"nonstream failure", false, false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := nativeGatewayBody(t)
			body, err := sjson.SetBytes(body, "stream", tc.stream)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			svc := openAIClientToolsTestService(nil)
			enableNativeAttachments(svc)
			calls := 0
			var keeper *openAICompactSSEKeepalive
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				if tc.beat {
					value, exists := c.Get(openAICompactSSEKeepaliveKey)
					require.True(t, exists, "keepalive must exist during uploads and before Responses headers")
					keeper = value.(*openAICompactSSEKeepalive)
					require.True(t, keeper.beat())
				}
				if req.URL.String() == basispoints.AttachmentsURL {
					return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-stream-test\"}"))}, nil
				}
				wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_keepalive\",\"status\":\"completed\",\"output\":[]}}\n\n"
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			}}
			_, err = svc.Forward(context.Background(), c, excelAccount(), body)
			if tc.status == 200 {
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				require.Contains(t, rec.Body.String(), "response.completed")
				require.Equal(t, 2, strings.Count(rec.Body.String(), ": keepalive"))
				require.False(t, keeper.beat(), "forwarding must stop the pre-upload heartbeat")
			} else {
				require.Error(t, err)
				require.Equal(t, 1, calls, "failed attachments cannot dispatch Responses")
				if tc.beat {
					require.Equal(t, 200, rec.Code)
					require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
					require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.failed"))
					require.Contains(t, rec.Body.String(), "upstream_http")
				} else {
					require.Equal(t, tc.status, rec.Code)
					require.NotContains(t, rec.Body.String(), ": keepalive")
				}
			}
		})
	}
}

type bpsFailingHeartbeatWriter struct{ gin.ResponseWriter }

func (w *bpsFailingHeartbeatWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestExcelBPSUploadHeartbeatWriteFailureCancels(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Writer = &bpsFailingHeartbeatWriter{ResponseWriter: c.Writer}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := startOpenAISSEKeepaliveWithCancel(c, time.Hour, cancel)
	defer stop()
	keeper := c.MustGet(openAICompactSSEKeepaliveKey).(*openAICompactSSEKeepalive)
	require.False(t, keeper.beat())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestExcelBPSNativeClientCancellationDoesNotReportUpload502(t *testing.T) {
	body, _ := nativeGatewayBody(t)
	body, err := sjson.SetBytes(body, "stream", true)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	clientCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(clientCtx)
	svc := openAIClientToolsTestService(nil)
	enableNativeAttachments(svc)
	calls := 0
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	result, err := svc.Forward(context.Background(), c, excelAccount(), body)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 1, calls)
	require.Empty(t, rec.Body.String())
}

type bpsTimeoutAttachmentBody struct{ closed bool }

func (*bpsTimeoutAttachmentBody) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (r *bpsTimeoutAttachmentBody) Close() error           { r.closed = true; return nil }

func TestExcelBPSAttachmentBodyTimeoutIsClassified(t *testing.T) {
	svc := openAIClientToolsTestService(nil)
	timed := &bpsTimeoutAttachmentBody{}
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: timed}, nil
	}}
	_, err := svc.uploadExcelBPSAttachment(context.Background(), excelAccount(), "private-token", "private-account", basispoints.InlineAttachment{MIME: "image/png"})
	require.Error(t, err)
	require.Equal(t, "timeout", excelBPSAttachmentFailureKind(err))
	require.True(t, timed.closed)
	require.NotContains(t, err.Error(), "private")
}
