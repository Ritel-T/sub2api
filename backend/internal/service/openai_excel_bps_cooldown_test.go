package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSCooldownUsesQuotaResetAndRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		headers http.Header
		body    string
		wait    time.Duration
		reason  string
	}{
		{"both windows exhausted", excelBPSQuotaHeaders("100", "100"), "{}", 7 * 24 * time.Hour, "quota_exhausted"},
		{"short window exhausted", excelBPSQuotaHeaders("100", "30"), "{}", 5 * time.Hour, "quota_exhausted"},
		{"unused windows", excelBPSQuotaHeaders("30", "20"), "{}", time.Minute, "rate_limited"},
		{"retry seconds", http.Header{"Retry-After": {"125"}}, "{}", 125 * time.Second, "rate_limited"},
		{"retry HTTP date", http.Header{"Retry-After": {now.Add(2 * time.Hour).Format(http.TimeFormat)}}, "{}", 2 * time.Hour, "rate_limited"},
		{"body relative", nil, "{\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":7200}}", 2 * time.Hour, "quota_exhausted"},
		{"body absolute", nil, fmt.Sprintf("{\"error\":{\"code\":\"usage_limit_reached\",\"resets_at\":\"%d\"}}", now.Add(3*time.Hour).Unix()), 3 * time.Hour, "quota_exhausted"},
		{"SSE quota", nil, "{\"response\":{\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":7200}}}", 2 * time.Hour, "quota_exhausted"},
		{"quota outlasts retry", http.Header{"Retry-After": {"2"}}, "{\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":7200}}", 2 * time.Hour, "quota_exhausted"},
		{"past reset", nil, "{\"error\":{\"type\":\"usage_limit_reached\",\"resets_at\":1}}", time.Minute, "quota_exhausted"},
		{"bounded long reset", nil, "{\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":9223372036854775807}}", 8 * 24 * time.Hour, "quota_exhausted"},
		{"malformed retry", http.Header{"Retry-After": {"NaN"}}, "not json", time.Minute, "rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			until, reason := excelBPSRateLimitDeadline(nil, tc.headers, []byte(tc.body), now)
			require.True(t, now.Add(tc.wait).Equal(until), "expected %s, got %s", now.Add(tc.wait), until)
			require.Equal(t, tc.reason, reason)
		})
	}
}

func TestExcelBPSCooldownSnapshotRequiresRecentExhaustion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	account := excelAccount()
	account.Extra["codex_usage_updated_at"] = now.Add(-time.Minute).Format(time.RFC3339)
	account.Extra["codex_5h_used_percent"] = 100.0
	account.Extra["codex_5h_reset_at"] = now.Add(time.Hour).Format(time.RFC3339)
	body := []byte("{\"error\":{\"type\":\"usage_limit_reached\"}}")
	until, _ := excelBPSRateLimitDeadline(account, nil, body, now)
	require.Equal(t, now.Add(time.Hour), until)
	until, _ = excelBPSRateLimitDeadline(account, nil, []byte("{}"), now)
	require.Equal(t, now.Add(time.Minute), until)
	account.Extra["codex_usage_updated_at"] = now.Add(-6 * time.Minute).Format(time.RFC3339)
	until, _ = excelBPSRateLimitDeadline(account, nil, body, now)
	require.Equal(t, now.Add(time.Minute), until)
}

func TestExcelBPSCooldownConcurrentExtensionAndExpiry(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := excelAccount()
	now := time.Now()
	var wg sync.WaitGroup
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc.extendLocalExcelBPSCooldown(account.ID, now.Add(time.Duration(i)*time.Minute))
		}(i)
	}
	wg.Wait()
	require.Equal(t, now.Add(100*time.Minute), svc.excelBPSCooldownUntil(account))
	account.Extra[ExcelBPSRateLimitResetAtKey] = now.Add(2 * time.Hour).Format(time.RFC3339Nano)
	require.True(t, now.Add(2*time.Hour).Equal(svc.excelBPSCooldownUntil(account)))
	svc.excelBPSRateLimits.Store(account.ID, now.Add(-time.Minute))
	account.Extra[ExcelBPSRateLimitResetAtKey] = now.Add(-time.Minute).Format(time.RFC3339Nano)
	require.True(t, svc.excelBPSCooldownUntil(account).IsZero())
	_, exists := svc.excelBPSRateLimits.Load(account.ID)
	require.False(t, exists)
}

func TestExcelBPSCooldownSurvivesPersistenceFailureAndCancellation(t *testing.T) {
	repo := &excelBPSQuotaRepo{bpsWrites: make(chan excelBPSQuotaWrite, 1), limitErr: errors.New("database unavailable")}
	svc := &OpenAIGatewayService{accountRepo: repo}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	account := excelAccount()
	until := svc.recordExcelBPSRateLimit(ctx, account, http.Header{"Retry-After": {"90"}}, nil)
	require.Equal(t, until, svc.excelBPSCooldownUntil(account))
	write := <-repo.bpsWrites
	require.NoError(t, write.ctxErr)
	require.WithinDuration(t, time.Now().Add(openAIAccountStateUpdateTimeout), write.deadline, time.Second)
	require.True(t, account.Schedulable)
	require.Nil(t, account.RateLimitResetAt)
}
