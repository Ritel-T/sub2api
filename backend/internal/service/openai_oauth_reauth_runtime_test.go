package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthReauthManagedWorkerNeedsNoEnvironmentToken(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", "")
	t.Setenv("DATA_DIR", t.TempDir())
	s := &OpenAIOAuthReauthService{}
	cfg := &config.Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 4040
	s.configureWorker(cfg, BuildInfo{Version: "2.9.4"})
	defer s.stopWorker()
	require.NotNil(t, s.worker)
	require.Len(t, s.workerToken, 64)
	configured, valid := s.WorkerAuthentication(s.workerToken)
	require.True(t, configured)
	require.True(t, valid)
	_, valid = s.WorkerAuthentication(strings.Repeat("x", 64))
	require.False(t, valid)
	status := s.WorkerStatus()
	require.Equal(t, "managed", status.Mode)
	require.Equal(t, "idle", status.State)
	raw, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(raw), s.workerToken)
	require.Error(t, s.checkWorkerMode(OpenAIOAuthReauthModeEmailOTPURL))
}

func TestOpenAIOAuthReauthPreservesExternalWorker(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", strings.Repeat("a", 32))
	s := &OpenAIOAuthReauthService{}
	s.configureWorker(nil, BuildInfo{Version: "2.9.4"})
	require.Nil(t, s.worker)
	configured, valid := s.WorkerAuthentication(strings.Repeat("a", 32))
	require.True(t, configured)
	require.True(t, valid)
	require.Equal(t, "external_offline", s.WorkerStatus().Reason)
}

func TestOpenAIOAuthReauthShortExplicitTokenDoesNotStartCompetingWorker(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", "short")
	s := &OpenAIOAuthReauthService{}
	s.configureWorker(nil, BuildInfo{Version: "2.9.4"})
	require.Nil(t, s.worker)
	configured, valid := s.WorkerAuthentication("short")
	require.False(t, configured)
	require.False(t, valid)
	require.Equal(t, "external_not_configured", s.WorkerStatus().Reason)
}

// The fork app version must never become the upstream release asset name.
// Intercept the release lookup so this test cannot contact GitHub.
type offlineReauthRuntimeTransport func(*http.Request) (*http.Response, error)

func (f offlineReauthRuntimeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestOpenAIOAuthReauthForkVersionUsesPinnedRuntimeRelease(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", "")
	t.Setenv("DATA_DIR", t.TempDir())
	originalTransport := http.DefaultTransport
	requests := make(chan string, 1)
	http.DefaultTransport = offlineReauthRuntimeTransport(func(r *http.Request) (*http.Response, error) {
		requests <- r.URL.String()
		return nil, errors.New("offline runtime test")
	})
	s := &OpenAIOAuthReauthService{}
	defer func() {
		s.stopWorker()
		http.DefaultTransport = originalTransport
	}()
	s.configureWorker(&config.Config{}, BuildInfo{Version: "2.9.6.1-ritel"})
	s.EnsureWorker()
	select {
	case got := <-requests:
		require.Equal(t, "https://api.github.com/repos/ranxi2001/sub2api/releases/tags/v2.9.6", got)
	case <-time.After(3 * time.Second):
		t.Fatal("managed worker did not request the pinned upstream release")
	}
	s.stopWorker()
	require.NotEqual(t, "release_required", s.WorkerStatus().Reason)
}

func TestOpenAIOAuthReauthOtherBuildsKeepReleaseGate(t *testing.T) {
	for _, version := range []string{"dev", "2.9.6.2-ritel", "2.9.6.1-custom", "2.9.5.1-ritel"} {
		t.Run(version, func(t *testing.T) {
			t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", "")
			t.Setenv("DATA_DIR", t.TempDir())
			s := &OpenAIOAuthReauthService{}
			defer s.stopWorker()
			s.configureWorker(&config.Config{}, BuildInfo{Version: version})
			s.EnsureWorker()
			require.Equal(t, "release_required", s.WorkerStatus().Reason)
		})
	}
}
