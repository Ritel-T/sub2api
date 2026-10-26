package service

import (
	"context"
	"net/http"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// GrantAccountStepUp exchanges completed native MFA for the existing session
// grant. The token is verified here; no caller-supplied proof or OTP is consumed.
func (s *TotpService) GrantAccountStepUp(ctx context.Context, auth *AuthService, userID int64, sessionID, token string) (time.Duration, error) {
	if !auth.AccountCenterEnabled() {
		return 0, infraerrors.NotFound("ACCOUNT_CENTER_DISABLED", "Account center disabled")
	}
	if s == nil || s.cache == nil || s.userRepo == nil {
		return 0, infraerrors.New(http.StatusServiceUnavailable, "STEP_UP_UNAVAILABLE", "Step-up verification service unavailable")
	}
	claims, err := auth.RequireRecentAccountAuthentication(ctx, token, userID, true)
	if err != nil {
		return 0, err
	}
	// Require the actual native family; never fall back to the legacy user key.
	if sessionID == "" || claims.SessionID == "" || claims.SessionID != sessionID {
		return 0, ErrInvalidToken
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	if user == nil || !user.IsActive() || user.DeletedAt != nil {
		return 0, ErrUserNotActive
	}
	if claims.TokenVersion != resolvedTokenVersion(user) || claims.AccountGeneration != accountCenterGeneration(user, claims.SessionEpoch) {
		return 0, ErrTokenRevoked
	}
	// Preserve the existing sensitive-operation requirement even for passkeys.
	if !user.TotpEnabled {
		return 0, infraerrors.Forbidden("STEP_UP_TOTP_NOT_ENABLED", "This operation requires two-factor authentication; please enable TOTP first")
	}
	// Refresh preserves auth_time. Repeating this exchange cannot move the
	// original authentication deadline, and never grants a full new window.
	ttl := time.Until(time.Unix(claims.AuthenticationTime, 0).Add(5 * time.Minute)).Truncate(time.Second)
	if ttl > 5*time.Minute {
		ttl = 5 * time.Minute
	}
	if ttl > StepUpGrantTTL {
		ttl = StepUpGrantTTL
	}
	if ttl <= 0 {
		return 0, infraerrors.Forbidden("REAUTH_REQUIRED", "recent authentication is required")
	}
	if err := s.cache.SetStepUpGrant(ctx, userID, claims.SessionID, ttl); err != nil {
		return 0, infraerrors.New(http.StatusServiceUnavailable, "STEP_UP_UNAVAILABLE", "Step-up verification service unavailable").WithCause(err)
	}
	return ttl, nil
}
