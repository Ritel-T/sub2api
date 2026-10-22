package setup

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBootstrapToken(t *testing.T) {
	t.Run("generated credentials differ", func(t *testing.T) {
		original, existed := os.LookupEnv("SETUP_TOKEN")
		require.NoError(t, os.Unsetenv("SETUP_TOKEN"))
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv("SETUP_TOKEN", original)
			}
		})
		first, generated, err := BootstrapToken()
		require.NoError(t, err)
		require.True(t, generated)
		require.Len(t, first, 64)
		second, _, err := BootstrapToken()
		require.NoError(t, err)
		require.NotEqual(t, first, second)
	})
	t.Run("configured token is retained without log disclosure", func(t *testing.T) {
		t.Setenv("SETUP_TOKEN", strings.Repeat("x", 32))
		token, generated, err := BootstrapToken()
		require.NoError(t, err)
		require.False(t, generated)
		require.Equal(t, strings.Repeat("x", 32), token)
	})
	for _, value := range []string{"", "short", strings.Repeat("x", 257), " " + strings.Repeat("x", 32), strings.Repeat("x", 32) + "\n"} {
		t.Run("invalid configured token", func(t *testing.T) {
			t.Setenv("SETUP_TOKEN", value)
			_, _, err := BootstrapToken()
			require.Error(t, err)
		})
	}
}

func TestSetupEndpointsRequireBootstrapCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("SKIP_SETUP", "false")
	token := strings.Repeat("x", 64)
	r := gin.New()
	RegisterRoutes(r, token)
	for _, endpoint := range []string{"test-db", "test-redis", "install"} {
		for _, credential := range []string{"", "wrong"} {
			request := httptest.NewRequest(http.MethodPost, "/setup/"+endpoint, strings.NewReader("{}"))
			request.Header.Set("X-Setup-Token", credential)
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusUnauthorized, recorder.Code, endpoint)
		}
		request := httptest.NewRequest(http.MethodPost, "/setup/"+endpoint, strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Setup-Token", token)
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, "authorized request must reach input validation")
	}
	status := httptest.NewRecorder()
	r.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
	require.Equal(t, http.StatusOK, status.Code)
	require.NotContains(t, status.Body.String(), token)
	require.NoError(t, os.WriteFile(filepath.Join(GetDataDir(), InstallLockFile), []byte("installed"), 0o600))
	request := httptest.NewRequest(http.MethodPost, "/setup/install", strings.NewReader("{}"))
	request.Header.Set("X-Setup-Token", token)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code, "bootstrap capability expires when installed")
}

func TestBootstrapGuardFailsClosedWithoutConfiguredCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/", bootstrapGuard(""), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, provided := range []string{"", "any-value"} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.Header.Set("X-Setup-Token", provided)
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
}

func TestSetupBodyLimitBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/", setupRequestLimits(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, length := range []int{16, maxSetupBodyBytes, maxSetupBodyBytes + 1} {
		for _, chunked := range []bool{false, true} {
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("x"), length)))
			if chunked {
				request.ContentLength = -1
				request.TransferEncoding = []string{"chunked"}
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, request)
			if length > maxSetupBodyBytes {
				require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			} else {
				require.Equal(t, http.StatusNoContent, recorder.Code)
			}
		}
	}
}

func TestSetupConcurrentRequestLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	started := make(chan struct{}, maxSetupRequests)
	release := make(chan struct{})
	r.POST("/", setupRequestLimits(), func(c *gin.Context) {
		started <- struct{}{}
		<-release
		c.Status(http.StatusNoContent)
	})
	var group sync.WaitGroup
	for range maxSetupRequests {
		group.Go(func() {
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader("{}"))))
		})
	}
	for range maxSetupRequests {
		<-started
	}
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	close(release)
	group.Wait()
}
