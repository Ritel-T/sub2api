package service

import (
	"context"
	"net/http"
	"regexp"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// NativeRateLimitClearObservation identifies the native cooldown generation
// observed before a successful probe. Identity gates bind that probe
// to the same proxy and access token without accepting a raw token.
type NativeRateLimitClearObservation struct {
	RateLimitedAt     time.Time
	RateLimitResetAt  time.Time
	ProxyID           *int64
	AccessTokenSHA256 string
}

// NativeRateLimitRecoveryRepository is deliberately separate from the broad
// ClearRateLimit operation, which also clears unrelated runtime protections.
type NativeRateLimitRecoveryRepository interface {
	ClearNativeRateLimitIfObserved(context.Context, int64, NativeRateLimitClearObservation) (bool, error)
}

var nativeRateLimitTokenHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *RateLimitService) ClearNativeRateLimitIfObserved(ctx context.Context, id int64, observed NativeRateLimitClearObservation) (bool, error) {
	if id <= 0 || observed.RateLimitedAt.IsZero() || observed.RateLimitResetAt.IsZero() ||
		(observed.ProxyID != nil && *observed.ProxyID <= 0) ||
		(observed.AccessTokenSHA256 != "" && !nativeRateLimitTokenHashPattern.MatchString(observed.AccessTokenSHA256)) {
		return false, infraerrors.New(http.StatusBadRequest, "INVALID_NATIVE_RATE_LIMIT_OBSERVATION", "Invalid native rate-limit observation")
	}
	if s == nil {
		return false, infraerrors.New(http.StatusServiceUnavailable, "NATIVE_RATE_LIMIT_RECOVERY_UNAVAILABLE", "Native rate-limit recovery unavailable")
	}
	repo, ok := s.accountRepo.(NativeRateLimitRecoveryRepository)
	if !ok {
		return false, infraerrors.New(http.StatusServiceUnavailable, "NATIVE_RATE_LIMIT_RECOVERY_UNAVAILABLE", "Native rate-limit recovery unavailable")
	}
	// No broad runtime/cache clearing: the repository updates only the native
	// timestamps and synchronizes the resulting scheduler snapshot.
	return repo.ClearNativeRateLimitIfObserved(ctx, id, observed)
}
