package service

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// CodexProbeUsageObservation carries only quota headers returned by an upstream
// request and the account identity used for that request. It grants no other
// account mutation or scheduling recovery.
type CodexProbeUsageObservation struct {
	ObservedAt        time.Time
	Headers           map[string]string
	ProxyID           *int64
	AccessTokenSHA256 string
}

// CodexProbeUsageSnapshotRepository keeps this atomic operation outside the
// general AccountRepository interface and its unrelated implementations.
type CodexProbeUsageSnapshotRepository interface {
	UpdateCodexUsageSnapshotIfObserved(context.Context, int64, CodexProbeUsageObservation, *string, map[string]any) (bool, error)
}

type CodexProbeUsageResult struct {
	Updated bool
	Quota   map[string]any
}

func invalidCodexProbeUsage() error {
	return infraerrors.New(http.StatusBadRequest, "INVALID_CODEX_USAGE_OBSERVATION", "Invalid Codex usage observation")
}

// validatedCodexProbeHeaders rejects arbitrary extra writes, nonfinite numbers,
// unsupported headers and values that would overflow reset-time calculation.
func validatedCodexProbeHeaders(raw map[string]string) (http.Header, error) {
	if len(raw) == 0 || len(raw) > 7 {
		return nil, invalidCodexProbeUsage()
	}
	headers := make(http.Header)
	for key, value := range raw {
		name := strings.ToLower(key)
		if headers.Get(name) != "" || value == "" || len(value) > 64 {
			return nil, invalidCodexProbeUsage()
		}
		switch name {
		case "x-codex-primary-used-percent", "x-codex-secondary-used-percent", "x-codex-primary-over-secondary-limit-percent":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 ||
				(name != "x-codex-primary-over-secondary-limit-percent" && number > 100) {
				return nil, invalidCodexProbeUsage()
			}
		case "x-codex-primary-reset-after-seconds", "x-codex-secondary-reset-after-seconds":
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < 0 || number > int64(math.MaxInt64/int64(time.Second)) {
				return nil, invalidCodexProbeUsage()
			}
		case "x-codex-primary-window-minutes", "x-codex-secondary-window-minutes":
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < 0 {
				return nil, invalidCodexProbeUsage()
			}
		default:
			return nil, invalidCodexProbeUsage()
		}
		headers.Set(name, value)
	}
	return headers, nil
}

// UpdateCodexUsageSnapshotFromProbe uses the same parser and normalization as a
// normal gateway response. Freshness and identity are checked before an atomic
// compare-and-merge; a concurrent normal response wins without being overwritten.
func (s *RateLimitService) UpdateCodexUsageSnapshotFromProbe(ctx context.Context, id int64, observed CodexProbeUsageObservation) (CodexProbeUsageResult, error) {
	result := CodexProbeUsageResult{}
	_, offset := observed.ObservedAt.Zone()
	now := time.Now().UTC()
	if id <= 0 || observed.ObservedAt.IsZero() || offset != 0 || observed.ObservedAt.After(now.Add(30*time.Second)) ||
		(observed.ProxyID != nil && *observed.ProxyID <= 0) ||
		!nativeRateLimitTokenHashPattern.MatchString(observed.AccessTokenSHA256) {
		return result, invalidCodexProbeUsage()
	}
	headers, err := validatedCodexProbeHeaders(observed.Headers)
	if err != nil {
		return result, err
	}
	if now.Sub(observed.ObservedAt) > 10*time.Minute {
		return result, nil
	}
	if s == nil || s.accountRepo == nil {
		return result, infraerrors.New(http.StatusServiceUnavailable, "CODEX_USAGE_SNAPSHOT_UNAVAILABLE", "Codex usage snapshot persistence unavailable")
	}
	repo, ok := s.accountRepo.(CodexProbeUsageSnapshotRepository)
	if !ok {
		return result, infraerrors.New(http.StatusServiceUnavailable, "CODEX_USAGE_SNAPSHOT_UNAVAILABLE", "Codex usage snapshot persistence unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return result, err
	}
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() || account.GetCredential("access_token") == "" {
		return result, nil
	}
	var expectedUpdatedAt *string
	if previous, exists := account.Extra["codex_usage_updated_at"]; exists && previous != nil {
		value, ok := previous.(string)
		if !ok {
			return result, nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil || !observed.ObservedAt.After(parsed) {
			return result, nil
		}
		expectedUpdatedAt = &value
	}
	snapshot := ParseCodexRateLimitHeaders(headers)
	if snapshot == nil {
		return result, invalidCodexProbeUsage()
	}
	snapshot.UpdatedAt = observed.ObservedAt.UTC().Format(time.RFC3339)
	result.Quota = buildCodexUsageExtraUpdates(snapshot, observed.ObservedAt)
	result.Updated, err = repo.UpdateCodexUsageSnapshotIfObserved(ctx, id, observed, expectedUpdatedAt, result.Quota)
	return result, err
}
