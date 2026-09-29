package middleware

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func bpsImageCompressedTestBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	var writer io.WriteCloser
	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&compressed)
	case "deflate":
		writer = zlib.NewWriter(&compressed)
	case "zstd":
		encoder, err := zstd.NewWriter(&compressed, zstd.WithEncoderConcurrency(1))
		require.NoError(t, err)
		writer = encoder
	default:
		t.Fatalf("unsupported test encoding %s", encoding)
	}
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func TestExcelBPSImageAdmissionCompressedSmallBodyUnderOccupiedBudget(t *testing.T) {
	// 72 ongoing small streams occupy 576 MiB of the 1024 MiB budget.
	// A small compressed request must use its actual size, not another 512 MiB.
	for _, encoding := range []string{"gzip", "zstd", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			entered := make(chan struct{}, 72)
			release := make(chan struct{})
			var once sync.Once
			var wg sync.WaitGroup
			t.Cleanup(func() { once.Do(func() { close(release) }); wg.Wait() })
			router := bpsImageTestRouter(bpsImageTestSettings{enabled: true, bodyLimitMiB: 64, budgetMiB: 1024, maxRequests: 128}, func(c *gin.Context) {
				if c.GetHeader("Hold") == "true" {
					entered <- struct{}{}
					<-release
				} else {
					body, err := io.ReadAll(c.Request.Body)
					if err != nil || string(body) != `{"model":"gpt-6-astra","input":"hello"}` {
						c.Status(http.StatusBadRequest)
						return
					}
				}
				c.Status(http.StatusNoContent)
			})
			for i := 0; i < 72; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					request := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader("small"))
					request.Header.Set("Hold", "true")
					router.ServeHTTP(httptest.NewRecorder(), request)
				}()
			}
			for i := 0; i < 72; i++ {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("existing stream did not reach the handler")
				}
			}
			compressed := bpsImageCompressedTestBody(t, encoding, []byte(`{"model":"gpt-6-astra","input":"hello"}`))
			request := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewReader(compressed))
			request.Header.Set("Content-Encoding", encoding)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			require.Equal(t, http.StatusNoContent, result.Code, result.Body.String())
		})
	}
}

func TestExcelBPSImageAdmissionCompressedGrowthStopsAtAggregateBudget(t *testing.T) {
	for _, encoding := range []string{"gzip", "zstd", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			entered := make(chan struct{}, 30)
			release := make(chan struct{})
			var once sync.Once
			var wg sync.WaitGroup
			t.Cleanup(func() { once.Do(func() { close(release) }); wg.Wait() })
			// Existing streams reserve 240 MiB; a 40 MiB decoded request
			// would need 320 MiB, exceeding the remaining 272 MiB.
			router := bpsImageTestRouter(bpsImageTestSettings{enabled: true, bodyLimitMiB: 64, budgetMiB: 512, maxRequests: 32}, func(c *gin.Context) {
				if c.GetHeader("Hold") == "true" {
					entered <- struct{}{}
					<-release
				} else if c.GetHeader("Must-Reject") == "true" {
					t.Error("body exceeding the aggregate budget reached downstream")
				}
				c.Status(http.StatusNoContent)
			})
			for i := 0; i < 30; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					request := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader("small"))
					request.Header.Set("Hold", "true")
					router.ServeHTTP(httptest.NewRecorder(), request)
				}()
			}
			for i := 0; i < 30; i++ {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("existing stream did not reach the handler")
				}
			}
			compressed := bpsImageCompressedTestBody(t, encoding, []byte(strings.Repeat("x", 40<<20)))
			request := httptest.NewRequest(http.MethodPost, "/responses", bytes.NewReader(compressed))
			request.Header.Set("Content-Encoding", encoding)
			request.Header.Set("Must-Reject", "true")
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			require.Equal(t, http.StatusServiceUnavailable, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), "basispoints_image_request_busy")
			require.Equal(t, "1", result.Header().Get("Retry-After"))
			// Failed decoding must release all of its intermediate reservation.
			next := httptest.NewRecorder()
			router.ServeHTTP(next, httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader("small")))
			require.Equal(t, http.StatusNoContent, next.Code)
		})
	}
}
