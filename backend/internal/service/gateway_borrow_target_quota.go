package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type automaticBorrowTargetContextKey struct{}
type targetQuotaObserverKey struct{}
type targetQuotaObserver func(context.Context, *http.Request, time.Time, time.Time) *time.Time

func automaticBorrowTargetContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, automaticBorrowTargetContextKey{}, true)
}

// Native configuration proxy identity fences concurrent edits. The actual
// borrowed exit may be the leased donor route; its ownership stays in the
// repository validator and is not confused with the target's native ProxyID.
func (s *AccountTestService) withAutomaticBorrowTargetQuotaObserver(ctx context.Context, account *Account, headers http.Header) context.Context {
	automatic, _ := ctx.Value(automaticBorrowTargetContextKey{}).(bool)
	if !automatic || s == nil || s.cfg == nil || !s.cfg.AstraRouting(ctx).AutoQuality || account == nil {
		return ctx
	}
	setter, ok := s.accountRepo.(interface {
		SetRateLimitedIfUnchanged(context.Context, int64, time.Time, *time.Time, *time.Time, time.Time) (bool, error)
	})
	if !ok {
		return ctx
	}
	baseline := *account
	fingerprint := gatewayBorrowCredentialFingerprint(account)
	proxyID := int64(0)
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	auth, workspace := headers.Get("Authorization"), headers.Get("ChatGPT-Account-ID")
	observer := targetQuotaObserver(func(callCtx context.Context, actual *http.Request, observed, until time.Time) *time.Time {
		if callCtx.Err() != nil || actual == nil || actual.URL == nil || actual.URL.String() != chatgptCodexURL ||
			actual.Header.Get("Authorization") != auth || actual.Header.Get("ChatGPT-Account-ID") != workspace ||
			!until.After(time.Now()) || time.Since(observed) < 0 || time.Since(observed) > 10*time.Minute {
			return nil
		}
		fresh, err := s.accountRepo.GetByID(callCtx, baseline.ID)
		if err != nil || fresh == nil || !fresh.IsOpenAIOAuth() || fresh.Status != StatusActive || !fresh.Schedulable || gatewayBorrowCredentialFingerprint(fresh) != fingerprint ||
			"Bearer "+fresh.GetOpenAIAccessToken() != auth || fresh.GetCredential("chatgpt_account_id") != workspace {
			return nil
		}
		currentProxyID := int64(0)
		if fresh.ProxyID != nil {
			currentProxyID = *fresh.ProxyID
		}
		if currentProxyID != proxyID {
			return nil
		}
		if fresh.RateLimitResetAt != nil && fresh.RateLimitResetAt.After(until) {
			copy := *fresh.RateLimitResetAt
			return &copy
		}
		// A response from before an admin clear/new quota generation cannot
		// re-arm that generation, even after an independent identity reread.
		if !fresh.UpdatedAt.Equal(baseline.UpdatedAt) {
			return nil
		}
		updated, err := setter.SetRateLimitedIfUnchanged(callCtx, fresh.ID, fresh.UpdatedAt, fresh.RateLimitedAt, fresh.RateLimitResetAt, until)
		if err != nil || !updated {
			return nil
		}
		slog.Info("automatic_borrow_target_quota_observed", "account_id", fresh.ID, "reset_at", until)
		return &until
	})
	return context.WithValue(ctx, targetQuotaObserverKey{}, observer)
}

// Only the current rejection's controlled metadata reaches the private quota
// observer. Arbitrary headers, raw bodies and snapshot-only percentages do not.
func observeAutomaticBorrowTargetQuota(ctx context.Context, req *http.Request, status int, headers http.Header, body []byte, observed time.Time) *time.Time {
	observer, _ := ctx.Value(targetQuotaObserverKey{}).(targetQuotaObserver)
	if observer == nil || ctx.Err() != nil {
		return nil
	}
	payload := body
	if status == http.StatusOK {
		payload = openAICodexStateStreamErrorPayload(body)
	}
	code := openAICodexProbeFailureCode(payload)
	if status != http.StatusTooManyRequests && !(status == http.StatusOK && (code == "usage_limit_reached" || code == "rate_limit_exceeded" || code == "rate_limit_error")) {
		return nil
	}
	filtered := map[string]string{}
	for _, name := range []string{"x-codex-primary-used-percent", "x-codex-primary-reset-after-seconds", "x-codex-primary-window-minutes", "x-codex-secondary-used-percent", "x-codex-secondary-reset-after-seconds", "x-codex-secondary-window-minutes", "x-codex-primary-over-secondary-limit-percent"} {
		if value := headers.Get(name); value != "" {
			filtered[name] = value
		}
	}
	var reset *time.Time
	if len(filtered) > 0 {
		if safe, err := validatedCodexProbeHeaders(filtered); err == nil {
			parsedAt := time.Now()
			if until := calculateOpenAI429ResetTime(safe); until != nil {
				bound := observed.Add(until.Sub(parsedAt))
				reset = &bound
			}
		}
	}
	if reset == nil {
		errorObject := gjson.GetBytes(payload, "error")
		if !errorObject.IsObject() {
			errorObject = gjson.GetBytes(payload, "response.error")
		}
		kind := errorObject.Get("type").String()
		if kind != "usage_limit_reached" && kind != "rate_limit_exceeded" && kind != "rate_limit_error" {
			kind = errorObject.Get("code").String()
		}
		if kind != "usage_limit_reached" && kind != "rate_limit_exceeded" && kind != "rate_limit_error" {
			return nil
		}
		if kind == "rate_limit_error" {
			kind = "rate_limit_exceeded"
		} // Exact confirmed code, numeric fields only.
		sanitized := map[string]any{"type": kind}
		for _, field := range []string{"resets_at", "resets_in_seconds"} {
			value := strings.TrimSpace(errorObject.Get(field).String())
			if value == "" {
				continue
			}
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number <= 0 || (field == "resets_at" && number >= 4102444800) || (field == "resets_in_seconds" && number > 4102444800-observed.Unix()) {
				return nil
			}
			sanitized[field] = number
		}
		wire, _ := json.Marshal(map[string]any{"error": sanitized})
		parsedAt := time.Now()
		if timestamp := parseOpenAIRateLimitResetTime(wire); timestamp != nil {
			bound := time.Unix(*timestamp, 0)
			if _, absolute := sanitized["resets_at"]; !absolute {
				bound = observed.Add(bound.Sub(parsedAt))
			}
			reset = &bound
		}
	}
	if reset == nil || !reset.After(time.Now()) {
		return nil
	}
	return observer(ctx, req, observed, *reset)
}
