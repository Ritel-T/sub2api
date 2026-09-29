package service

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	ExcelBPSRateLimitResetAtKey = "openai_excel_bps_rate_limit_reset_at"
	ExcelBPSRateLimitReasonKey  = "openai_excel_bps_rate_limit_reason"
	excelBPSDefaultCooldown     = time.Minute
	excelBPSMaxCooldown         = 8 * 24 * time.Hour
)

type excelBPSRateLimitRepository interface {
	ExtendExcelBPSRateLimit(context.Context, int64, time.Time, string) (time.Time, error)
}

func excelBPSExtraTime(account *Account, key string) time.Time {
	if account == nil {
		return time.Time{}
	}
	raw, _ := account.Extra[key].(string)
	value, _ := time.Parse(time.RFC3339Nano, raw)
	return value
}

// Local state closes the persistence/cache propagation gap without mutating the
// shared Account snapshot or blocking the account's native protocol routes.
func (s *OpenAIGatewayService) extendLocalExcelBPSCooldown(id int64, until time.Time) time.Time {
	for {
		previous, loaded := s.excelBPSRateLimits.LoadOrStore(id, until)
		if !loaded {
			return until
		}
		old, valid := previous.(time.Time)
		if !valid {
			s.excelBPSRateLimits.Store(id, until)
			return until
		}
		if !until.After(old) {
			return old
		}
		if s.excelBPSRateLimits.CompareAndSwap(id, old, until) {
			return until
		}
	}
}

func (s *OpenAIGatewayService) excelBPSCooldownUntil(account *Account) time.Time {
	if account == nil {
		return time.Time{}
	}
	now := time.Now()
	until := excelBPSExtraTime(account, ExcelBPSRateLimitResetAtKey)
	if s != nil {
		if raw, ok := s.excelBPSRuntimeCooldownUntil.Load(account.ID); ok {
			runtime, valid := raw.(time.Time)
			if !valid {
				s.excelBPSRuntimeCooldownUntil.Delete(account.ID)
			} else if !runtime.After(now) {
				s.excelBPSRuntimeCooldownUntil.CompareAndDelete(account.ID, runtime)
			} else if runtime.After(until) {
				until = runtime
			}
		}
		if raw, ok := s.excelBPSRateLimits.Load(account.ID); ok {
			local, valid := raw.(time.Time)
			if !valid {
				s.excelBPSRateLimits.Delete(account.ID)
			} else if !local.After(now) {
				s.excelBPSRateLimits.CompareAndDelete(account.ID, local)
			} else if local.After(until) {
				until = local
			}
		}
	}
	if !until.After(now) {
		return time.Time{}
	}
	return until
}

func excelBPSQuotaError(body []byte) gjson.Result {
	if nested := gjson.GetBytes(body, "response.error"); nested.IsObject() {
		return nested
	}
	return gjson.GetBytes(body, "error")
}

func excelBPSQuotaExhausted(body []byte) bool {
	err := excelBPSQuotaError(body)
	return err.Get("code").String() == "usage_limit_reached" || err.Get("type").String() == "usage_limit_reached"
}

func excelBPSRateLimitDeadline(account *Account, headers http.Header, body []byte, now time.Time) (time.Time, string) {
	until := time.Time{}
	reason := "rate_limited"
	add := func(candidate time.Time) {
		if candidate.After(now) && candidate.After(until) {
			until = candidate
		}
	}
	if snapshot := ParseCodexRateLimitHeaders(headers); snapshot != nil {
		if quota := snapshot.Normalize(); quota != nil {
			if quota.Used5hPercent != nil && *quota.Used5hPercent >= 100 {
				reason = "quota_exhausted"
				if quota.Reset5hSeconds != nil && *quota.Reset5hSeconds > 0 {
					add(now.Add(time.Duration(min(*quota.Reset5hSeconds, int(excelBPSMaxCooldown/time.Second))) * time.Second))
				}
			}
			if quota.Used7dPercent != nil && *quota.Used7dPercent >= 100 {
				reason = "quota_exhausted"
				if quota.Reset7dSeconds != nil && *quota.Reset7dSeconds > 0 {
					add(now.Add(time.Duration(min(*quota.Reset7dSeconds, int(excelBPSMaxCooldown/time.Second))) * time.Second))
				}
			}
		}
	}
	err := excelBPSQuotaError(body)
	if excelBPSQuotaExhausted(body) {
		reason = "quota_exhausted"
	}
	if reason == "quota_exhausted" || err.Get("type").String() == "rate_limit_exceeded" || err.Get("code").String() == "rate_limit_exceeded" {
		if seconds := err.Get("resets_at").Int(); seconds > 0 {
			add(time.Unix(seconds, 0))
		}
		if seconds := err.Get("resets_in_seconds").Int(); seconds > 0 {
			add(now.Add(time.Duration(min(seconds, int64(excelBPSMaxCooldown/time.Second))) * time.Second))
		}
	}
	if raw := strings.TrimSpace(headers.Get("Retry-After")); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
			add(now.Add(time.Duration(min(seconds, int64(excelBPSMaxCooldown/time.Second))) * time.Second))
		} else if date, err := http.ParseTime(raw); err == nil {
			add(date)
		}
	}
	// A recent complete quota snapshot can supply a missing reset, but an
	// ordinary endpoint throttle must not inherit an unrelated quota window.
	updated := excelBPSExtraTime(account, "codex_usage_updated_at")
	if reason == "quota_exhausted" && until.IsZero() && account != nil && !updated.After(now) && now.Sub(updated) <= 5*time.Minute {
		for _, window := range []string{"5h", "7d"} {
			used, _ := account.Extra["codex_"+window+"_used_percent"].(float64)
			if used >= 100 {
				add(excelBPSExtraTime(account, "codex_"+window+"_reset_at"))
			}
		}
	}
	if until.IsZero() {
		until = now.Add(excelBPSDefaultCooldown)
	}
	if limit := now.Add(excelBPSMaxCooldown); until.After(limit) {
		until = limit
	}
	return until, reason
}

func (s *OpenAIGatewayService) recordExcelBPSRateLimit(ctx context.Context, account *Account, headers http.Header, body []byte) time.Time {
	// Observation probes must not change the account's BPS scheduling state.
	// Their errors remain inconclusive quality evidence.
	if isQualityObservation(ctx) {
		return time.Time{}
	}
	until, reason := excelBPSRateLimitDeadline(account, headers, body, time.Now())
	until = s.extendLocalExcelBPSCooldown(account.ID, until)
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if repo, ok := s.accountRepo.(excelBPSRateLimitRepository); ok {
		stored, err := repo.ExtendExcelBPSRateLimit(stateCtx, account.ID, until, reason)
		if err != nil {
			slog.ErrorContext(stateCtx, "excel_bps_cooldown_persistence_failed", "account_id", account.ID)
		} else {
			until = s.extendLocalExcelBPSCooldown(account.ID, stored)
		}
	}
	slog.WarnContext(stateCtx, "excel_bps_rate_limited", "account_id", account.ID, "reason", reason, "reset_at", until)
	return until
}

func excelBPSCooldownFailover(until time.Time) *UpstreamFailoverError {
	seconds := max(1, int(time.Until(until).Seconds()+1))
	return &UpstreamFailoverError{
		StatusCode:       http.StatusTooManyRequests,
		ResponseBody:     []byte(`{"error":{"type":"rate_limit_error","code":"basispoints_rate_limited","message":"Excel BPS account is cooling down; retry after the reset time"}}`),
		ResponseHeaders:  http.Header{"Retry-After": {strconv.Itoa(seconds)}},
		ClientStatusCode: http.StatusTooManyRequests,
		ClientMessage:    "Excel BPS account is cooling down; retry after the reset time",
		Scope:            GatewayFailureScopeAccount,
	}
}
