//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
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

type targetQuotaCASRepo struct {
	AccountRepository
	account       *Account
	writes, reads int
	casReject     bool
	onRead        func(int)
}

func (r *targetQuotaCASRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead(r.reads)
	}
	copy := *r.account
	copy.Credentials = maps.Clone(r.account.Credentials)
	return &copy, nil
}
func (r *targetQuotaCASRepo) SetRateLimitedIfUnchanged(_ context.Context, id int64, updated time.Time, limited, reset *time.Time, until time.Time) (bool, error) {
	if r.casReject {
		return false, nil
	}
	if id != r.account.ID || !updated.Equal(r.account.UpdatedAt) {
		return false, nil
	}
	if (reset == nil) != (r.account.RateLimitResetAt == nil) || (reset != nil && !reset.Equal(*r.account.RateLimitResetAt)) {
		return false, nil
	}
	r.writes++
	copy := *r.account
	copy.RateLimitResetAt = &until
	r.account = &copy
	return true, nil
}

type targetQuotaVerifier struct {
	HTTPUpstream
	status     int
	body       string
	headers    http.Header
	calls      int
	lastResult *OpenAICodexStateProbeResult
	onSend     func(*http.Request)
}

func (u *targetQuotaVerifier) VerifyAstraGatewayTargetForModelWithTLS(ctx context.Context, req *http.Request, proxy string, id int64, concurrency int, _ string, profile *tlsfingerprint.Profile) error {
	req = req.Clone(ctx)
	req.Header.Set("Cookie", "__oailb=target-quota")
	result := ProbeOpenAICodexBorrowAccountQualityRoute(ctx, borrowQualityProbeUpstream{call: func(actual *http.Request) (*http.Response, error) {
		u.calls++
		if u.onSend != nil {
			u.onSend(actual)
		}
		return &http.Response{StatusCode: u.status, Header: u.headers, Body: io.NopCloser(strings.NewReader(u.body))}, nil
	}}, req, proxy, id, concurrency, profile, false)
	u.lastResult = result
	return errors.New(result.Failure)
}

func targetQuotaFixture(t *testing.T) (*AccountTestService, *targetQuotaCASRepo, *targetQuotaVerifier) {
	t.Helper()
	gateway, account, _ := borrowSafetyService()
	account.UpdatedAt = time.Now().Add(-time.Minute)
	settings := gateway.cfg.AstraRouting(t.Context())
	settings.AutoQuality, settings.QualityMode = true, config.GatewayBorrowAccountQualityMode
	gateway.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	repo := &targetQuotaCASRepo{account: account}
	upstream := &targetQuotaVerifier{status: 429, headers: http.Header{}, body: `{"error":{"type":"usage_limit_reached","resets_in_seconds":3600}}`}
	gateway.accountRepo = repo
	return &AccountTestService{cfg: gateway.cfg, accountRepo: repo, httpUpstream: upstream, openaiGatewayService: gateway}, repo, upstream
}

func TestBorrowTargetRealQuotaResetStopsSubsequentWarm(t *testing.T) {
	for _, kind := range []string{"http_error_reset", "http_headers_reset", "sse_error_reset", "sse_rate_limit_error_reset"} {
		t.Run(kind, func(t *testing.T) {
			s, repo, upstream := targetQuotaFixture(t)
			beforeSnapshot := s.AstraGatewayStatus(t.Context())
			require.Len(t, beforeSnapshot.Targets, 2)
			require.NotEmpty(t, automaticBorrowWarmPlan(beforeSnapshot, s.cfg.AstraRouting(t.Context()).CookiePool, time.Now()), "this same live account is scheduled before the real quota rejection")
			if kind == "http_headers_reset" {
				upstream.body = `{"error":{"type":"rate_limit_error"}}`
				upstream.headers.Set("x-codex-primary-used-percent", "100")
				upstream.headers.Set("x-codex-primary-reset-after-seconds", "3600")
				upstream.headers.Set("x-codex-primary-window-minutes", "300")
			}
			if kind == "sse_rate_limit_error_reset" {
				upstream.status = 200
				upstream.body = "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"rate_limit_error\",\"resets_in_seconds\":3600}}}\n\n"
			}
			if kind == "sse_error_reset" {
				upstream.status = 200
				upstream.body = "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":3600}}}\n\n"
			}
			require.Error(t, s.verifyGatewayBorrowTargetForModel(automaticBorrowTargetContext(t.Context()), repo.account.ID, "gpt-6-astra"))
			require.Equal(t, 1, repo.writes, "only the fresh target 429/usage-limit response can persist its reset")
			require.NotNil(t, repo.account.RateLimitResetAt)
			require.NotNil(t, upstream.lastResult.ConfirmedQuotaResetAt)
			require.WithinDuration(t, *repo.account.RateLimitResetAt, *upstream.lastResult.ConfirmedQuotaResetAt, time.Millisecond)
			snapshot := s.AstraGatewayStatus(t.Context())
			require.Empty(t, automaticBorrowWarmPlan(snapshot, s.cfg.AstraRouting(t.Context()).CookiePool, time.Now()), "the next background cycle prepares no donor while all targets are genuinely quota-blocked")
			require.WithinDuration(t, time.Now().Add(time.Hour), *repo.account.RateLimitResetAt, 2*time.Second)
			require.ErrorContains(t, s.verifyGatewayBorrowTargetForModel(automaticBorrowTargetContext(t.Context()), repo.account.ID, "gpt-6-astra"), "target_account_rate_limited")
			require.Equal(t, 1, upstream.calls, "warm scheduling must stop until real reset")
			for _, target := range snapshot.Targets {
				require.Equal(t, "blocked", target.State)
				require.Equal(t, "target_account_rate_limited", target.Reason)
				require.Equal(t, *repo.account.RateLimitResetAt, *target.RetryAt)
			}
			// Simulate the natural reset deadline passing without clearing quality.
			past := time.Now().Add(-time.Second)
			copy := *repo.account
			copy.RateLimitResetAt = &past
			repo.account = &copy
			require.NotEmpty(t, automaticBorrowWarmPlan(s.AstraGatewayStatus(t.Context()), s.cfg.AstraRouting(t.Context()).CookiePool, time.Now()))
			_ = s.verifyGatewayBorrowTargetForModel(automaticBorrowTargetContext(t.Context()), repo.account.ID, "gpt-6-astra")
			require.Equal(t, 2, upstream.calls, "natural reset permits one new bounded observation")
		})
	}
}

func TestBorrowTargetQuotaWritesOnlyCurrentAutomaticRejectionEvidence(t *testing.T) {
	for _, kind := range []string{"manual", "no_reset", "transient_headers", "success_snapshot", "overload", "stale_reset", "changed_token", "changed_workspace", "changed_proxy_id", "changed_proxy_url", "disabled", "admin_clear", "cas_rejected", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s, repo, u := targetQuotaFixture(t)
			ctx := automaticBorrowTargetContext(t.Context())
			if kind == "manual" {
				ctx = t.Context()
			}
			switch kind {
			case "no_reset":
				u.body = `{"error":{"type":"usage_limit_reached"}}`
			case "transient_headers":
				u.body = `{"error":{"type":"rate_limit_error"}}`
				u.headers.Set("x-codex-primary-used-percent", "80")
				u.headers.Set("x-codex-primary-reset-after-seconds", "3600")
				u.headers.Set("x-codex-primary-window-minutes", "300")
			case "success_snapshot":
				u.status = 200
				u.body = "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":[{\"content\":[{\"type\":\"output_text\",\"text\":\"21\"}]}]}}\n\n"
				u.headers.Set("x-codex-primary-used-percent", "100")
				u.headers.Set("x-codex-primary-reset-after-seconds", "3600")
				u.headers.Set("x-codex-primary-window-minutes", "300")
			case "overload":
				u.status = 200
				u.body = "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"resets_in_seconds\":3600}}}\n\n"
			case "stale_reset":
				u.body = `{"error":{"type":"usage_limit_reached","resets_at":1}}`
			case "cas_rejected":
				repo.casReject = true
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				u.onSend = func(*http.Request) { cancel() }
			default:
				u.onSend = func(*http.Request) {
					copy := *repo.account
					copy.Credentials = maps.Clone(repo.account.Credentials)
					switch kind {
					case "changed_token":
						copy.Credentials["access_token"] = "changed"
					case "changed_workspace":
						copy.Credentials["chatgpt_account_id"] = "changed"
					case "changed_proxy_id":
						id := int64(888)
						copy.ProxyID = &id
					case "changed_proxy_url":
						copy.Proxy = &Proxy{Protocol: "http", Host: "changed.example", Port: 8080}
					case "disabled":
						copy.Schedulable = false
					case "admin_clear":
						copy.UpdatedAt = time.Now()
						copy.RateLimitResetAt = nil
					}
					repo.account = &copy
				}
			}
			_ = s.verifyGatewayBorrowTargetForModel(ctx, repo.account.ID, "gpt-6-astra")
			require.Zero(t, repo.writes, "no private target observer may write from stale/manual/metadata-free evidence")
			if u.lastResult != nil {
				require.Nil(t, u.lastResult.ConfirmedQuotaResetAt, "rejected/CAS-failed observation never extends private quota quiet")
			}
		})
	}
}

func TestBorrowTargetQuotaPreservesCurrentLongerRestriction(t *testing.T) {
	s, repo, u := targetQuotaFixture(t)
	later := time.Now().Add(2 * time.Hour)
	u.onSend = func(*http.Request) {
		copy := *repo.account
		copy.RateLimitResetAt = &later
		copy.UpdatedAt = time.Now()
		repo.account = &copy
	}
	require.Error(t, s.verifyGatewayBorrowTargetForModel(automaticBorrowTargetContext(t.Context()), repo.account.ID, "gpt-6-astra"))
	require.Zero(t, repo.writes)
	require.Equal(t, later, *repo.account.RateLimitResetAt, "a target rejection cannot shorten a newer independent limit")
	require.NotNil(t, u.lastResult.ConfirmedQuotaResetAt)
	require.Equal(t, later, *u.lastResult.ConfirmedQuotaResetAt)
}

func TestBorrowTargetQuotaMetadataNeverAppearsInPublicProbeJSON(t *testing.T) {
	s, repo, u := targetQuotaFixture(t)
	account, headers, _, err := s.prepareGatewayBorrowAccountIdentity(t.Context(), repo.account, "gpt-6-astra")
	require.NoError(t, err)
	ctx := s.withAutomaticBorrowTargetQuotaObserver(automaticBorrowTargetContext(t.Context()), account, headers)
	template, _ := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, nil)
	template.Header = headers.Clone()
	template.Header.Set("Cookie", "__oailb=secret-route")
	result := ProbeOpenAICodexBorrowAccountQualityRoute(ctx, borrowQualityProbeUpstream{call: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: u.status, Header: u.headers, Body: io.NopCloser(strings.NewReader(u.body))}, nil
	}}, template, "leased-donor-exit", account.ID, 1, nil, false)
	require.NotNil(t, result.ConfirmedQuotaResetAt)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, value := range []string{"ConfirmedQuotaResetAt", "confirmed_quota_reset_at", "secret-route", account.GetOpenAIAccessToken(), "leased-donor-exit", "x-codex-primary"} {
		require.NotContains(t, string(encoded), value)
	}
}
