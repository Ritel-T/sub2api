package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
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
		{"fast rate limit", true, false, 429}, {"slow rate limit", true, true, 429},
		{"nonstream failure", false, false, 503},
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
					typedKeeper, ok := value.(*openAICompactSSEKeepalive)
					require.True(t, ok)
					keeper = typedKeeper
					require.True(t, keeper.beat())
				}
				if req.URL.String() == basispoints.AttachmentsURL {
					return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-stream-test\"}"))}, nil
				}
				wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_keepalive\",\"status\":\"completed\",\"output\":[]}}\n\n"
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			}}
			account := excelAccount()
			_, err = svc.Forward(context.Background(), c, account, body)
			if tc.status == 200 {
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				require.Contains(t, rec.Body.String(), "response.completed")
				require.Equal(t, 2, strings.Count(rec.Body.String(), ": keepalive"))
				require.False(t, keeper.beat(), "forwarding must stop the pre-upload heartbeat")
			} else {
				require.Error(t, err)
				require.Equal(t, 1, calls, "failed attachments cannot dispatch Responses")
				if tc.status == http.StatusTooManyRequests {
					var failover *UpstreamFailoverError
					require.ErrorAs(t, err, &failover)
					require.Equal(t, http.StatusTooManyRequests, failover.StatusCode)
					require.True(t, failover.SafeToFailoverAfterWrite)
					require.False(t, svc.excelBPSCooldownUntil(account).IsZero())
					require.Equal(t, http.StatusOK, rec.Code)
					require.NotContains(t, rec.Body.String(), "response.failed")
					if tc.beat {
						require.Equal(t, 1, strings.Count(rec.Body.String(), ": keepalive"))
						require.False(t, keeper.beat(), "failover must stop upload heartbeats")
					} else {
						require.Empty(t, rec.Body.String())
						require.False(t, IsResponseCommitted(c))
					}
				} else if tc.beat {
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
	keeper, ok := c.MustGet(openAICompactSSEKeepaliveKey).(*openAICompactSSEKeepalive)
	require.True(t, ok)
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
	require.Nil(t, result, "canceled uploads must not create a billable Responses result")
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
	_, err := svc.uploadExcelBPSAttachment(context.Background(), excelAccount(), "private-token", "private-account", "", basispoints.InlineAttachment{MIME: "image/png"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, "timeout", excelBPSAttachmentFailureKind(err))
	require.True(t, timed.closed)
	require.NotContains(t, err.Error(), "private")
}

func TestExcelBPSAttachmentTransportErrorsRetainOnlySafeSentinels(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		cause      error
		sentinel   error
	}{
		{"canceled", "request_canceled", context.Canceled, context.Canceled},
		{"deadline", "timeout", context.DeadlineExceeded, context.DeadlineExceeded},
		{"socket timeout", "dns_error", &net.DNSError{Err: "PRIVATE_TIMEOUT", Name: "PRIVATE_HOST", IsTimeout: true}, nil},
		{"transport", "transport_error", errors.New("PRIVATE_TRANSPORT"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := &url.Error{Op: "Post", URL: "https://PRIVATE_USER:PRIVATE_PASS@upload.invalid/?token=PRIVATE_TOKEN", Err: tc.cause}
			err := excelBPSAttachmentTransportError(cause)
			if tc.sentinel != nil {
				require.ErrorIs(t, err, tc.sentinel)
			} else {
				require.NoError(t, errors.Unwrap(err))
			}
			require.NotEqual(t, cause, errors.Unwrap(err))
			require.EqualError(t, err, "excel BPS attachment failed: attachment_transport/"+transportdiag.Classify(cause)+" (status 502)")
			require.NotContains(t, err.Error(), "PRIVATE")
			require.Equal(t, tc.kind, excelBPSAttachmentFailureKind(err))
			var attachment *excelBPSAttachmentError
			require.ErrorAs(t, err, &attachment)
			require.Equal(t, http.StatusBadGateway, attachment.status)
			for _, canceled := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				if canceled {
					cancel()
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
				require.Equal(t, canceled && errors.Is(tc.sentinel, context.Canceled), isExcelBPSClientCancellation(c, err))
				cancel()
			}
		})
	}
}

type bpsAttachmentErrorBody struct {
	err        error
	beforeRead func()
	closed     bool
}

func (b *bpsAttachmentErrorBody) Read([]byte) (int, error) {
	if b.beforeRead != nil {
		b.beforeRead()
	}
	return 0, b.err
}
func (b *bpsAttachmentErrorBody) Close() error { b.closed = true; return nil }

func TestExcelBPSNativeAttachmentCancellationClassification(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, stage := range []string{"headers", "body", "error body"} {
			for _, tc := range []struct {
				name         string
				cause        error
				cancelClient bool
			}{
				{"client canceled", context.Canceled, true},
				{"upstream canceled", context.Canceled, false},
				{"upstream deadline", context.DeadlineExceeded, false},
			} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", tc.name, stage, stream), func(t *testing.T) {
					body, _ := nativeGatewayBody(t)
					body, err := sjson.SetBytes(body, "stream", stream)
					require.NoError(t, err)
					clientCtx, cancel := context.WithCancel(context.Background())
					defer cancel()
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(clientCtx)
					wrapped := &url.Error{Op: "Post", URL: "https://upload.invalid/?token=PRIVATE_TOKEN", Err: tc.cause}
					cancelIfRequested := func() {
						if tc.cancelClient {
							cancel()
						}
					}
					failedBody := &bpsAttachmentErrorBody{err: wrapped, beforeRead: cancelIfRequested}
					calls := 0
					svc := openAIClientToolsTestService(nil)
					enableNativeAttachments(svc)
					svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						calls++
						require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
						if stage == "headers" {
							cancelIfRequested()
							return nil, wrapped
						}
						status := http.StatusOK
						if stage == "error body" {
							status = http.StatusTooManyRequests
						}
						return &http.Response{StatusCode: status, Header: http.Header{}, Body: failedBody}, nil
					}}
					account := excelAccount()
					result, err := svc.Forward(context.Background(), c, account, body)
					require.Error(t, err)
					require.Equal(t, 1, calls, "failed uploads must not replay or start Responses")
					require.NotContains(t, err.Error(), "PRIVATE")
					require.NotContains(t, rec.Body.String(), "PRIVATE")
					if stage != "headers" {
						require.True(t, failedBody.closed)
					}
					if stage == "error body" {
						require.False(t, svc.excelBPSCooldownUntil(account).IsZero(), "429 must enter BPS cooldown even when its body read is canceled")
					} else {
						require.True(t, svc.excelBPSCooldownUntil(account).IsZero())
					}
					require.True(t, account.IsSchedulable())
					require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
					marked, recorded := GetOpsStreamError(c)
					_, providerFailure := c.Get(OpsUpstreamErrorMessageKey)
					if tc.cancelClient {
						require.ErrorIs(t, err, context.Canceled)
						require.Nil(t, result)
						require.True(t, recorded)
						require.Equal(t, OpsClientCanceledCode, marked.Code)
						require.Equal(t, 499, marked.IntendedStatus)
						require.True(t, marked.RequestScoped)
						require.Equal(t, !stream, marked.NonStream)
						require.False(t, providerFailure)
						require.Less(t, rec.Code, 400)
						require.NotContains(t, rec.Body.String(), "response.failed")
					} else if stage == "error body" {
						var failover *UpstreamFailoverError
						require.ErrorAs(t, err, &failover)
						require.Equal(t, http.StatusTooManyRequests, failover.StatusCode)
						require.True(t, failover.SafeToFailoverAfterWrite)
						require.Nil(t, result)
						require.NoError(t, clientCtx.Err())
						require.Empty(t, rec.Body.String())
						require.False(t, IsResponseCommitted(c))
						require.False(t, recorded && marked.Code == OpsClientCanceledCode)
						require.False(t, providerFailure)
					} else {
						require.NoError(t, clientCtx.Err())
						require.Equal(t, http.StatusBadGateway, rec.Code)
						require.False(t, recorded && marked.Code == OpsClientCanceledCode)
						require.True(t, providerFailure)
						require.Contains(t, rec.Body.String(), "basispoints_attachment_error")
						if errors.Is(tc.cause, context.DeadlineExceeded) {
							require.Contains(t, rec.Body.String(), "reason=timeout")
						}
					}
				})
			}
		}
	}
}
