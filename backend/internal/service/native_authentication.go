package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// RecentAccountAuthenticationTTL is the account-management proof window.
const RecentAccountAuthenticationTTL = 30 * time.Minute

type nativeAuthenticationKey struct{}

// NativeAuthenticationProof is set only by server-side completed authentication.
// It is never read from request JSON/headers. Refresh preserves, never renews it.
type NativeAuthenticationProof struct {
	Methods    []string `json:"methods,omitempty"`
	AuthTime   int64    `json:"auth_time,omitempty"`
	Generation string   `json:"generation,omitempty"`
	Epoch      string   `json:"epoch,omitempty"`
}

func WithNativeAuthenticationProof(ctx context.Context, proof NativeAuthenticationProof) context.Context {
	proof.Methods = append([]string(nil), proof.Methods...)
	return context.WithValue(ctx, nativeAuthenticationKey{}, proof)
}
func nativeAuthenticationFromContext(ctx context.Context) NativeAuthenticationProof {
	proof, _ := ctx.Value(nativeAuthenticationKey{}).(NativeAuthenticationProof)
	proof.Methods = append([]string(nil), proof.Methods...)
	return proof
}
func NativeAuthenticationFromClaims(claims *JWTClaims) NativeAuthenticationProof {
	if claims == nil {
		return NativeAuthenticationProof{}
	}
	return NativeAuthenticationProof{Methods: append([]string(nil), claims.AuthenticationMethods...), AuthTime: claims.AuthenticationTime, Generation: claims.AccountGeneration, Epoch: claims.SessionEpoch}
}
func (s *AuthService) RequireRecentAccountAuthentication(ctx context.Context, token string, userID int64, requireMFA bool) (*JWTClaims, error) {
	if s == nil || s.cfg == nil || !s.cfg.AccountCenter.Enabled {
		return nil, ErrServiceUnavailable
	}
	claims, err := s.ValidateTokenContext(ctx, token)
	if err != nil {
		return nil, err
	}
	if claims.UserID != userID {
		return nil, ErrInvalidToken
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive() || user.DeletedAt != nil {
		return nil, ErrUserNotActive
	}
	if claims.TokenVersion != resolvedTokenVersion(user) || claims.AccountGeneration == "" || claims.AccountGeneration != accountCenterGeneration(user, claims.SessionEpoch) {
		return nil, ErrTokenRevoked
	}
	now := time.Now().Unix()
	if claims.AuthenticationTime <= 0 || claims.AuthenticationTime > now+5 || now-claims.AuthenticationTime > int64(RecentAccountAuthenticationTTL/time.Second) {
		return nil, infraerrors.Forbidden("REAUTH_REQUIRED", "recent authentication is required")
	}
	pwd, otp, passkey := false, false, false
	for _, method := range claims.AuthenticationMethods {
		switch method {
		case "pwd":
			pwd = true
		case "otp", "recovery":
			otp = true
		case "passkey":
			passkey = true
		}
	}
	if !passkey && (!pwd || (requireMFA && !otp)) {
		return nil, infraerrors.Forbidden("REAUTH_REQUIRED", "recent authentication factors are required")
	}
	return claims, nil
}

func (s *AuthService) AccountCenterEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.AccountCenter.Enabled
}
