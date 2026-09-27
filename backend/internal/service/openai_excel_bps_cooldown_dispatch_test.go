package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSCooldownAfterProxyAcquisitionPreventsSend(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			account := excelAccount()
			account.Extra["openai_excel_bps_mihomo"] = true
			svc := &OpenAIGatewayService{}
			calls, acquired := 0, 0
			leases := []*bpsTestLease{}
			acquire := func(context.Context, string, ...string) (string, excelBPSLease, error) {
				acquired++
				lease := &bpsTestLease{}
				leases = append(leases, lease)
				if !retry || acquired == 2 {
					svc.extendLocalExcelBPSCooldown(account.ID, time.Now().Add(time.Hour))
				}
				return fmt.Sprintf("http://127.0.0.1:%d", 19000+acquired), lease, nil
			}
			svc.httpUpstream = &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
				calls++
				httptrace.ContextClientTrace(req.Context()).GetConn("bps.openai.com:443")
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNRESET}
			}}
			body := []byte("{\"model\":\"gpt-6-astra\"}")
			guard := func(ctx context.Context) error { return svc.checkExcelBPSCooldownBeforeDispatch(ctx, account, body) }
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			resp, lease, _, err := svc.doExcelBPSRequest(context.Background(), c, account, "test", body, "synthetic", "synthetic", acquire, guard)
			var cooldown *ExcelBPSCooldownError
			require.ErrorAs(t, err, &cooldown)
			require.Nil(t, resp)
			require.Nil(t, lease)
			wantCalls := 0
			if retry {
				wantCalls = 1
			}
			require.Equal(t, wantCalls, calls)
			require.Equal(t, wantCalls+1, acquired)
			for _, lease := range leases {
				require.Equal(t, 1, lease.releases)
			}
			require.Zero(t, leases[len(leases)-1].failures, "a local quota guard must not penalize the proxy")
		})
	}
}

func TestExcelBPSCooldownDispatchRechecksGroupAndPause(t *testing.T) {
	for _, change := range []string{"removed_group", "manual_pause"} {
		t.Run(change, func(t *testing.T) {
			account := excelAccount()
			account.GroupIDs = []int64{9}
			latest := *account
			if change == "removed_group" {
				latest.GroupIDs = []int64{10}
			} else {
				latest.Schedulable = false
			}
			svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: &latest}}
			err := svc.checkExcelBPSCooldownBeforeDispatchForGroup(context.Background(), account, []byte("{\"model\":\"gpt-6-astra\"}"), 9, true)
			require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
		})
	}
}

func TestExcelBPSCooldownPreventsEncryptedRecoverySend(t *testing.T) {
	account := excelAccount()
	svc := openAIClientToolsTestService(&httpUpstreamRecorder{})
	calls := 0
	svc.httpUpstream = &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
		calls++
		svc.extendLocalExcelBPSCooldown(account.ID, time.Now().Add(time.Hour))
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))}, nil
	}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	_, err := svc.Forward(context.Background(), c, account, excelBPSEncryptedHistoryRequest(true))
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.True(t, failover.SafeToFailoverAfterWrite)
	require.Equal(t, 1, calls)
	require.Empty(t, rec.Body.String())
}

func TestExcelBPSStreamQuotaFailureCoolsWithoutReplay(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_quota\",\"status\":\"failed\",\"error\":{\"type\":\"usage_limit_reached\",\"code\":\"usage_limit_reached\",\"resets_in_seconds\":3600}}}\n\n"
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(wire))}}
			svc := openAIClientToolsTestService(upstream)
			account := excelAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			body := []byte(fmt.Sprintf("{\"model\":\"gpt-6-astra\",\"input\":\"test\",\"stream\":%t}", stream))
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover, "semantic stream output must never be replayed")
			require.Len(t, upstream.requests, 1)
			require.WithinDuration(t, time.Now().Add(time.Hour), svc.excelBPSCooldownUntil(account), 2*time.Second)
			if stream {
				require.Contains(t, rec.Body.String(), "partial")
			} else {
				require.Equal(t, http.StatusTooManyRequests, rec.Code)
			}
		})
	}
}
