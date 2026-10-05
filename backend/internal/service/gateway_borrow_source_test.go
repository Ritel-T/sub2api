//go:build unit

package service

import (
	"context"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type borrowSourceTestRepo struct {
	AccountRepository
	account       *Account
	reads, writes int
	onRead        func(int)
}

func (r *borrowSourceTestRepo) GetByID(context.Context, int64) (*Account, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead(r.reads)
	}
	return r.account, nil
}
func (r *borrowSourceTestRepo) SetRateLimited(_ context.Context, _ int64, until time.Time) error {
	r.writes++
	copy := *r.account
	copy.RateLimitResetAt = &until
	r.account = &copy
	return nil
}

type borrowSourceTestUpstream struct {
	HTTPUpstream
	calls   int
	status  int
	body    string
	headers http.Header
	request *http.Request
}

func (p *borrowSourceTestUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	p.calls++
	p.request = req
	return &http.Response{StatusCode: p.status, Header: p.headers, Body: io.NopCloser(strings.NewReader(p.body))}, nil
}
func newBorrowSourceTest(t *testing.T) (*AccountTestService, *borrowSourceTestRepo, *borrowSourceTestUpstream) {
	t.Helper()
	gateway, account, _ := borrowSafetyService()
	account.Extra = map[string]any{}
	repo := &borrowSourceTestRepo{account: account}
	upstream := &borrowSourceTestUpstream{status: 200, headers: make(http.Header), body: "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":[{\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"}
	settings := config.AstraRoutingSettings{AutoQuality: true, CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{account.ID}}}
	gateway.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	gateway.accountRepo = repo
	return &AccountTestService{accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway, cfg: gateway.cfg}, repo, upstream
}

func TestAutomaticBorrowSourceKnownRestrictionsNeverSend(t *testing.T) {
	for _, kind := range []string{"native quota", "paused", "overload", "model quota"} {
		t.Run(kind, func(t *testing.T) {
			s, repo, upstream := newBorrowSourceTest(t)
			future := time.Now().Add(time.Hour)
			switch kind {
			case "native quota":
				repo.account.RateLimitResetAt = &future
			case "paused":
				repo.account.Schedulable = false
			case "overload":
				repo.account.OverloadUntil = &future
			case "model quota":
				repo.account.Extra[modelRateLimitsKey] = map[string]any{"gpt-6-astra": map[string]any{"rate_limit_reset_at": future.Format(time.RFC3339)}}
			}
			require.Error(t, s.prepareAstraGatewaySource(context.Background(), repo.account.ID))
			require.Zero(t, upstream.calls)
			require.Zero(t, repo.writes)
		})
	}
}

func TestAutomaticBorrowSourceNewQuotaStopsRepeatedAcquisition(t *testing.T) {
	for _, kind := range []string{"HTTP429 reset", "SSE200 quota no reset"} {
		t.Run(kind, func(t *testing.T) {
			s, repo, upstream := newBorrowSourceTest(t)
			if kind == "HTTP429 reset" {
				upstream.status = 429
				upstream.body = "{\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":3600}}"
			} else {
				upstream.body = "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"usage_limit_reached\"}}}\n\n"
			}
			require.ErrorContains(t, s.prepareAstraGatewaySource(context.Background(), repo.account.ID), "source_account_rate_limited")
			require.ErrorContains(t, s.prepareAstraGatewaySource(context.Background(), repo.account.ID), "source_account_rate_limited")
			require.Equal(t, 1, upstream.calls)
			if kind == "HTTP429 reset" {
				require.Equal(t, 1, repo.writes)
				require.True(t, repo.account.RateLimitResetAt.After(time.Now().Add(50*time.Minute)))
			} else {
				require.Zero(t, repo.writes, "no reset metadata creates only source-local retry")
			}
			require.True(t, IsAstraSourceAcquisition(upstream.request.Context()))
			_, required := GatewayBorrowRequiredModelFromContext(upstream.request.Context())
			require.False(t, required, "a source must not enter target borrowing")
		})
	}
}

func TestAutomaticBorrowSourceChangedIdentityDoesNotSendOrReset(t *testing.T) {
	s, repo, upstream := newBorrowSourceTest(t)
	selected := repo.account
	repo.onRead = func(read int) {
		if read == 2 {
			changed := *selected
			id := int64(987)
			changed.ProxyID = &id
			repo.account = &changed
		}
	}
	require.Error(t, s.prepareAstraGatewaySource(context.Background(), selected.ID))
	require.Zero(t, upstream.calls)
	require.Zero(t, repo.writes)
}

func TestAutomaticBorrowSourceUsesLatestProviderAuth(t *testing.T) {
	s, repo, upstream := newBorrowSourceTest(t)
	repo.account.Credentials = maps.Clone(repo.account.Credentials)
	repo.account.Credentials["access_token"] = "fresh-source-token"
	repo.account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	cache := newOpenAITokenCacheStub()
	cache.tokens[OpenAITokenCacheKey(repo.account)] = "stale-source-token"
	s.openaiGatewayService.openAITokenProvider = NewOpenAITokenProvider(repo, cache, nil)
	require.NoError(t, s.prepareAstraGatewaySource(context.Background(), repo.account.ID))
	require.Equal(t, "Bearer fresh-source-token", upstream.request.Header.Get("Authorization"))
	require.Zero(t, repo.writes)
}

func TestAutomaticBorrowSourceQuotaDoesNotShortenConcurrentLimit(t *testing.T) {
	s, repo, upstream := newBorrowSourceTest(t)
	original := repo.account
	long := time.Now().Add(12 * time.Hour)
	repo.onRead = func(read int) {
		if read == 3 {
			copy := *original
			copy.RateLimitResetAt = &long
			repo.account = &copy
		}
	}
	upstream.status = 429
	upstream.body = "{\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":300}}"
	require.ErrorContains(t, s.prepareAstraGatewaySource(context.Background(), original.ID), "source_account_rate_limited")
	require.Zero(t, repo.writes)
	require.Equal(t, long, *repo.account.RateLimitResetAt)
	require.Equal(t, 1, upstream.calls)
}

func TestAutomaticBorrowSourceRuntimeHidesBlockedCandidate(t *testing.T) {
	s, repo, _ := newBorrowSourceTest(t)
	long := time.Now().Add(time.Hour)
	repo.account.RateLimitResetAt = &long
	expiry := time.Now().Add(time.Minute)
	upstream := &borrowSafetyProvider{snapshot: AstraGatewayRuntime{Sources: []AstraRouteStatus{{AccountID: repo.account.ID, State: "candidate", ExpiresAt: &expiry, RemainingSeconds: 60, Active: true}}}}
	s.httpUpstream = upstream
	status := s.AstraGatewayStatus(context.Background())
	require.Len(t, status.Sources, 1)
	require.Equal(t, "blocked", status.Sources[0].State)
	require.Equal(t, "source_account_rate_limited", status.Sources[0].Reason)
	require.Nil(t, status.Sources[0].ExpiresAt)
	require.False(t, status.Sources[0].Active)
	require.Zero(t, status.Sources[0].RemainingSeconds)
	require.NotNil(t, status.Sources[0].RetryAt)
}
