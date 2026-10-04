//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type borrowAccountTestRepo struct {
	AccountRepository
	account *Account
}

type borrowTrackedTokenCache struct {
	*openAITokenCacheStub
	deleted []string
}

func (c *borrowTrackedTokenCache) DeleteAccessToken(ctx context.Context, key string) error {
	c.deleted = append(c.deleted, key)
	return c.openAITokenCacheStub.DeleteAccessToken(ctx, key)
}

func (r *borrowAccountTestRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *borrowAccountTestRepo) GetOpenAITurnAdmission(context.Context, int64) (*Account, *Account, error) {
	return r.account, nil, nil
}
func (r *borrowAccountTestRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	copy := *r.account
	copy.Credentials = maps.Clone(credentials)
	r.account = &copy
	return nil
}

type borrowAccountTestUpstream struct {
	*borrowSafetyProvider
	request       *http.Request
	profile       *tlsfingerprint.Profile
	err           error
	calls         int
	verifyRequest *http.Request
	verifyProfile *tlsfingerprint.Profile
}

func (p *borrowAccountTestUpstream) VerifyAstraGatewayTargetForModelWithTLS(_ context.Context, req *http.Request, _ string, _ int64, _ int, _ string, profile *tlsfingerprint.Profile) error {
	p.verifyRequest, p.verifyProfile = req, profile
	return nil
}

func (p *borrowAccountTestUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	p.calls++
	p.request, p.profile = req, profile
	if p.err != nil {
		return nil, p.err
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"status\":\"completed\"}}\n\n"))}, nil
}

func TestBorrowAccountTestUsesProviderTokenAndRefreshedAccount(t *testing.T) {
	gateway, selected, provider := borrowSafetyService()
	selected.Credentials = maps.Clone(selected.Credentials)
	selected.Credentials["access_token"] = "stale-fixture-token"
	latest := *selected
	latest.Credentials = maps.Clone(selected.Credentials)
	latest.Credentials["access_token"] = "fresh-fixture-token"
	latest.Credentials["user_agent"] = "codex_cli_rs/0.160.0 (Windows 11; x86_64)"
	latest.Credentials["header_overrides"] = map[string]any{"X-Account-Test-Fixture": "preserved"}
	selected.Credentials["user_agent"] = latest.Credentials["user_agent"]
	selected.Credentials["header_overrides"] = latest.Credentials["header_overrides"]
	latest.Extra = maps.Clone(selected.Extra)
	latest.Extra["enable_tls_fingerprint"] = true
	selected.Extra = latest.Extra
	repo := &borrowAccountTestRepo{account: &latest}
	cache := newOpenAITokenCacheStub()
	cache.tokens[OpenAITokenCacheKey(selected)] = "fresh-fixture-token"
	gateway.openAITokenProvider = NewOpenAITokenProvider(repo, cache, nil)
	gateway.accountRepo = repo
	upstream := &borrowAccountTestUpstream{borrowSafetyProvider: provider}
	gateway.httpUpstream = upstream
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway, tlsFPProfileService: &TLSFingerprintProfileService{}}
	c, rec := newTestContext()
	require.NoError(t, svc.testOpenAIAccountConnection(c, selected, "gpt-6-astra", "", AccountTestModeDefault))
	require.Greater(t, cache.getCalled, int32(0), "manual test must use the token provider rather than raw snapshot credentials")
	require.Equal(t, "Bearer fresh-fixture-token", upstream.request.Header.Get("Authorization"))
	require.Nil(t, upstream.profile, "OpenAI currently uses no Anthropic-only TLS profile")
	require.Empty(t, upstream.request.Header.Get("X-Account-Test-Fixture"), "OAuth preserves the existing header-override eligibility restriction")
	require.Contains(t, upstream.request.Header.Get("User-Agent"), "Windows")
	model, required := GatewayBorrowRequiredModelFromContext(upstream.request.Context())
	require.True(t, required)
	require.Equal(t, "gpt-6-astra", model)
	require.True(t, AccountTestUsedGatewayBorrow(c))
	require.Contains(t, rec.Body.String(), `"channel":"gateway_borrow"`)
	require.Contains(t, rec.Body.String(), `"success":true`)
	svc.cfg = gateway.cfg
	require.NoError(t, svc.verifyGatewayBorrowTargetForModel(context.Background(), latest.ID, "gpt-6-astra"))
	for _, header := range []string{"Authorization", "ChatGPT-Account-ID", "User-Agent", "Originator", "Version"} {
		require.Equal(t, upstream.request.Header.Get(header), upstream.verifyRequest.Header.Get(header), header+" differs between warming and manual test")
	}
	require.Equal(t, upstream.profile, upstream.verifyProfile)
}

func TestBorrowAccountTestFailsClosedAndReturnsStableReason(t *testing.T) {
	for _, reason := range []string{"target_quality_failed", "target_probe_rate_limited", "target_validation_in_progress"} {
		t.Run(reason, func(t *testing.T) {
			gateway, account, provider := borrowSafetyService()
			repo := &borrowAccountTestRepo{account: account}
			upstream := &borrowAccountTestUpstream{borrowSafetyProvider: provider, err: errors.New(reason)}
			gateway.accountRepo, gateway.httpUpstream = repo, upstream
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway}
			c, rec := newTestContext()
			require.Error(t, svc.testOpenAIAccountConnection(c, account, "gpt-6-astra", "", AccountTestModeDefault))
			require.Equal(t, 1, upstream.calls, "a rejected route may not retry a native request")
			require.Contains(t, rec.Body.String(), `"code":"`+reason+`"`)
			require.NotContains(t, rec.Body.String(), `"success":true`)
		})
	}
}

func TestBorrowAccountTestUnavailableRouteNeverDispatches(t *testing.T) {
	gateway, account, provider := borrowSafetyService()
	provider.snapshot.Targets = nil
	repo := &borrowAccountTestRepo{account: account}
	upstream := &borrowAccountTestUpstream{borrowSafetyProvider: provider}
	gateway.accountRepo, gateway.httpUpstream = repo, upstream
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway}
	c, rec := newTestContext()
	require.Error(t, svc.testOpenAIAccountConnection(c, account, "gpt-6-astra", "", AccountTestModeDefault))
	require.Zero(t, upstream.calls)
	require.Contains(t, rec.Body.String(), `"code":"gateway_borrow_route_not_ready"`)
}

func TestBorrowAccountTestRefreshesExpiredTokenBeforeSending(t *testing.T) {
	gateway, account, provider := borrowSafetyService()
	account.Credentials = maps.Clone(account.Credentials)
	account.Credentials["access_token"] = "expired-fixture-token"
	account.Credentials["refresh_token"] = "fixture-refresh-token"
	account.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &borrowAccountTestRepo{account: account}
	cache := newOpenAITokenCacheStub()
	refreshes := 0
	executor := &dynamicRefreshExecutor{canRefresh: true, cacheKey: OpenAITokenCacheKey(account), needsRefreshFunc: func() bool { return refreshes == 0 }, refreshFunc: func(_ context.Context, latest *Account) (map[string]any, error) {
		refreshes++
		credentials := maps.Clone(latest.Credentials)
		credentials["access_token"] = "refreshed-fixture-token"
		credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
		return credentials, nil
	}}
	tokens := NewOpenAITokenProvider(repo, cache, nil)
	tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
	gateway.openAITokenProvider, gateway.accountRepo = tokens, repo
	upstream := &borrowAccountTestUpstream{borrowSafetyProvider: provider}
	gateway.httpUpstream = upstream
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway}
	c, _ := newTestContext()
	require.NoError(t, svc.testOpenAIAccountConnection(c, account, "gpt-6-astra", "", AccountTestModeDefault))
	require.Equal(t, 1, refreshes)
	require.Equal(t, "Bearer refreshed-fixture-token", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "refreshed-fixture-token", repo.account.GetOpenAIAccessToken())
}

func TestBorrowAccountTestInvalidatesOnlyStaleAccountCacheOnce(t *testing.T) {
	for _, deleteFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "refresh cache", true: "failed delete rejects"}[deleteFails], func(t *testing.T) {
			gateway, account, provider := borrowSafetyService()
			account.Credentials = maps.Clone(account.Credentials)
			account.Credentials["access_token"] = "fresh-persisted-token"
			account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
			repo := &borrowAccountTestRepo{account: account}
			cache := &borrowTrackedTokenCache{openAITokenCacheStub: newOpenAITokenCacheStub()}
			cache.tokens[OpenAITokenCacheKey(account)] = "old-cached-token"
			cache.tokens["openai:account:999"] = "unrelated-token"
			if deleteFails {
				cache.deleteErr = errors.New("fixture delete unavailable")
			}
			gateway.openAITokenProvider = NewOpenAITokenProvider(repo, cache, nil)
			gateway.accountRepo = repo
			upstream := &borrowAccountTestUpstream{borrowSafetyProvider: provider}
			gateway.httpUpstream = upstream
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway}
			c, rec := newTestContext()
			err := svc.testOpenAIAccountConnection(c, account, "gpt-6-astra", "", AccountTestModeDefault)
			require.Equal(t, []string{OpenAITokenCacheKey(account)}, cache.deleted)
			require.Equal(t, "unrelated-token", cache.tokens["openai:account:999"])
			if deleteFails {
				require.Error(t, err)
				require.Zero(t, upstream.calls)
				require.Contains(t, rec.Body.String(), "\"code\":\"account_binding_changed\"")
				require.Equal(t, int32(1), cache.getCalled, "failed delete cannot retry the stale provider cache")
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, "Bearer fresh-persisted-token", upstream.request.Header.Get("Authorization"))
				require.Equal(t, int32(2), cache.getCalled, "provider is retried only once")
			}
		})
	}
}
