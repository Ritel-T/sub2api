package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the actual shared 1 GiB/128-request guard, not a stub reservation.
func TestExcelBPSImageAdmissionBudgetBoundaryKeepsAllDefaultSlotsUsable(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, 1<<20)
	var zipped bytes.Buffer
	writer := gzip.NewWriter(&zipped)
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	for _, tc := range []struct {
		name     string
		wire     []byte
		length   int64
		encoding string
	}{
		{"identity", body, int64(len(body)), ""},
		{"chunked", body, -1, ""},
		{"gzip", zipped.Bytes(), int64(zipped.Len()), "gzip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{}, 128)
			release := make(chan struct{})
			results := make(chan *httptest.ResponseRecorder, 128)
			var once sync.Once
			var wg sync.WaitGroup
			t.Cleanup(func() { once.Do(func() { close(release) }); wg.Wait() })
			router := bpsImageTestRouter(bpsImageTestSettings{enabled: true}, func(c *gin.Context) {
				got, readErr := io.ReadAll(c.Request.Body)
				if readErr != nil || !bytes.Equal(got, body) {
					c.Status(http.StatusBadRequest)
					return
				}
				if c.GetHeader("Hold") == "true" {
					entered <- struct{}{}
					<-release
				}
				c.Status(http.StatusNoContent)
			})
			request := func(wire []byte, length int64, encoding string, hold bool) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(wire))
				req.ContentLength = length
				req.Header.Set("Content-Encoding", encoding)
				if hold {
					req.Header.Set("Hold", "true")
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			for i := 0; i < 128; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); results <- request(tc.wire, tc.length, tc.encoding, true) }()
				select {
				case <-entered:
				case result := <-results:
					t.Fatalf("request %d failed: HTTP%d %s", i+1, result.Code, result.Body.String())
				case <-time.After(5 * time.Second):
					t.Fatal("request did not finish reading")
				}
			}
			overflow := request(tc.wire, tc.length, tc.encoding, false)
			require.Equal(t, http.StatusServiceUnavailable, overflow.Code)
			require.Contains(t, overflow.Body.String(), "basispoints_image_request_busy")
			once.Do(func() { close(release) })
			for i := 0; i < 128; i++ {
				select {
				case result := <-results:
					require.Equal(t, http.StatusNoContent, result.Code)
				case <-time.After(5 * time.Second):
					t.Fatal("request did not release")
				}
			}
			wg.Wait()
			after := request(tc.wire, tc.length, tc.encoding, false)
			require.Equal(t, http.StatusNoContent, after.Code, "all reservations release after completion")
		})
	}
}

func TestExcelBPSImageAdmissionBudgetBoundaryFailuresReleaseEveryAttempt(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, (1<<20)+1)
	for _, encoding := range []string{"identity", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			wire := body
			if encoding == "gzip" {
				var zipped bytes.Buffer
				writer := gzip.NewWriter(&zipped)
				_, err := writer.Write(body)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				wire = zipped.Bytes()
			}
			budget := &bpsImageAdmissionBudget{}
			held, ok := budget.acquire(127 * 8 << 20)
			require.True(t, ok)
			defer held.release()
			for attempt := 0; attempt < 3; attempt++ {
				reservation, ok := budget.acquire(8 << 20)
				require.True(t, ok)
				closed := false
				req := httptest.NewRequest(http.MethodPost, "/responses", nil)
				req.ContentLength = -1
				req.Header.Set("Content-Encoding", encoding)
				req.Body = &boundaryClosingBody{Reader: bytes.NewReader(wire), closed: &closed}
				_, err := httputil.ReadRequestBodyWithPreallocLimitAndBudget(req, bpsImageMaxBodyBytes, reservation.readBudget(bpsImageMaxBodyBytes))
				require.ErrorIs(t, err, errBPSImageRequestBusy)
				require.NoError(t, req.Body.Close())
				require.True(t, closed)
				require.Equal(t, int64(1024<<20), budget.bytes, "overflow probe never grows retained reservations")
				reservation.release()
				require.Equal(t, int64(1016<<20), budget.bytes)
				require.Equal(t, 1, budget.requests)
			}
			held.release()
			require.Zero(t, budget.bytes)
			require.Zero(t, budget.requests)
			fresh, ok := budget.acquire(8 << 20)
			require.True(t, ok)
			fresh.release()
		})
	}
}

type boundaryClosingBody struct {
	io.Reader
	closed *bool
}

func (b *boundaryClosingBody) Close() error { *b.closed = true; return nil }
