package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrAccountCredentialChangeUnknown = infraerrors.ServiceUnavailable("ACCOUNT_CREDENTIAL_CHANGE_OUTCOME_UNKNOWN", "credential change outcome requires verification; do not automatically replay")

// AccountCredentialChange is returned only after commit, mint and independent
// authority readback. User is the exact transaction snapshot used for minting.
type AccountCredentialChange struct {
	TokenPair
	User *User
}

// accountCredentialAuthority authenticates only the fixed middleware subject.
// Neither request JSON nor an ambient context proof can establish authority.
func (s *AuthService) accountCredentialAuthority(ctx context.Context, userID int64, token string) (*JWTClaims, *User, error) {
	if !s.AccountCenterEnabled() || s.userRepo == nil {
		return nil, nil, ErrServiceUnavailable
	}
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if u == nil || u.ID != userID {
		return nil, nil, ErrInvalidToken
	}
	claims, err := s.RequireRecentAccountAuthentication(ctx, token, userID, u.TotpEnabled)
	if err != nil {
		return nil, nil, err
	}
	if claims.TokenVersion != resolvedTokenVersion(u) || claims.AccountGeneration != accountCenterGeneration(u, claims.SessionEpoch) || claims.Email != u.Email || claims.Role != u.Role {
		return nil, nil, ErrTokenRevoked
	}
	binding := sessionBindingHashFromContext(ctx)
	if binding == "" {
		return nil, nil, ErrInvalidToken
	}
	if claims.BindingHash != "" && claims.BindingHash != binding {
		return nil, nil, ErrSessionBindingMismatch
	}
	return claims, u, nil
}

func (s *AuthService) ChangeAccountSecurityPassword(ctx context.Context, userID int64, token, oldPassword, newPassword string) (*AccountCredentialChange, error) {
	claims, u, err := s.accountCredentialAuthority(ctx, userID, token)
	if err != nil {
		return nil, err
	}
	if !s.CheckPassword(oldPassword, u.PasswordHash) {
		return nil, ErrPasswordIncorrect
	}
	if utf8.RuneCountInString(newPassword) < 6 {
		return nil, infraerrors.BadRequest("PASSWORD_TOO_SHORT", "password must be at least 6 characters")
	}
	if len(newPassword) > 72 {
		return nil, infraerrors.BadRequest("PASSWORD_TOO_LONG", "password must be at most 72 bytes")
	}
	hash, err := s.HashPassword(newPassword)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	return s.changeAccountCredential(ctx, claims, u, hash, "")
}

func (s *AuthService) ChangeAccountSecurityEmail(ctx context.Context, userID int64, token, email, verifyCode, password string) (*AccountCredentialChange, error) {
	claims, u, err := s.accountCredentialAuthority(ctx, userID, token)
	if err != nil {
		return nil, err
	}
	email, err = normalizeEmailForIdentityBinding(email)
	if err != nil {
		return nil, err
	}
	if isReservedEmail(email) {
		return nil, ErrEmailReserved
	}
	if email == strings.ToLower(strings.TrimSpace(u.Email)) {
		return nil, infraerrors.BadRequest("ACCOUNT_CREDENTIAL_UNCHANGED", "email is already bound")
	}
	if strings.TrimSpace(password) == "" {
		return nil, ErrPasswordRequired
	}
	hash := u.PasswordHash
	if hasBindableEmailIdentitySubject(u.Email) {
		if !s.CheckPassword(password, hash) {
			return nil, ErrPasswordIncorrect
		}
	} else {
		if utf8.RuneCountInString(password) < 6 {
			return nil, infraerrors.BadRequest("PASSWORD_TOO_SHORT", "password must be at least 6 characters")
		}
		if len(password) > 72 {
			return nil, infraerrors.BadRequest("PASSWORD_TOO_LONG", "password must be at most 72 bytes")
		}
		hash, err = s.HashPassword(password)
		if err != nil {
			return nil, ErrServiceUnavailable
		}
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return nil, err
	}
	if err := s.ensureEmailIdentityAvailableForUser(ctx, u, email); err != nil {
		return nil, err
	}
	if err := s.VerifyOAuthEmailCode(ctx, email, verifyCode); err != nil {
		return nil, err
	}
	// Explicit security changes never apply signup/first-bind business grants.
	return s.changeAccountCredential(ctx, claims, u, hash, email)
}

// changeAccountCredential owns its transaction. CAS both changes the hash and
// holds the user row until commit, so email alias/identity writes cannot overwrite
// a concurrent password, MFA, administrator or other account security mutation.
func (s *AuthService) changeAccountCredential(ctx context.Context, claims *JWTClaims, before *User, hash, email string) (*AccountCredentialChange, error) {
	if s.entClient == nil || s.refreshTokenCache == nil || dbent.TxFromContext(ctx) != nil {
		return nil, ErrServiceUnavailable
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := s.validateSessionEpoch(txCtx, before.ID, claims.SessionEpoch); err != nil {
		return nil, err
	}
	if err := s.validateAccountSessionFamily(txCtx, before.ID, claims.SessionID, claims.AccountGeneration); err != nil {
		return nil, err
	}
	update := tx.User.Update().Where(
		dbuser.IDEQ(before.ID), dbuser.EmailEQ(before.Email), dbuser.PasswordHashEQ(before.PasswordHash),
		dbuser.RoleEQ(before.Role), dbuser.StatusEQ(StatusActive), dbuser.DeletedAtIsNil(), dbuser.TotpEnabledEQ(before.TotpEnabled),
	).SetPasswordHash(hash)
	if before.TotpSecretEncrypted == nil {
		update.Where(dbuser.TotpSecretEncryptedIsNil())
	} else {
		update.Where(dbuser.TotpSecretEncryptedEQ(*before.TotpSecretEncrypted))
	}
	n, err := update.Save(txCtx)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	if n != 1 {
		return nil, ErrTokenRevoked
	}
	if email != "" {
		snapshot := *before
		if err := s.updateBoundEmailIdentityWithClient(txCtx, tx.Client(), &snapshot, email, hash, false); err != nil {
			return nil, err
		}
	}
	mutated, err := s.userRepo.GetByID(txCtx, before.ID)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	expectedEmail := before.Email
	if email != "" {
		expectedEmail = email
	}
	if mutated == nil || mutated.ID != before.ID || mutated.PasswordHash != hash || mutated.Email != expectedEmail || mutated.Role != before.Role || accountCenterGeneration(mutated, claims.SessionEpoch) == claims.AccountGeneration {
		return nil, ErrServiceUnavailable
	}
	if err := tx.Commit(); err != nil {
		return nil, credentialChangeUnknown("commit")
	}
	// A credential fingerprint fences old AT/RT/families durably even when cache
	// cleanup fails. Do not rotate/reload the epoch: a concurrent revoke must win.
	if err := s.cleanRedisRefreshSessions(ctx, before.ID); err != nil {
		return nil, credentialChangeUnknown("refresh_cleanup")
	}
	return s.mintAccountCredentialChange(ctx, claims, mutated)
}

func credentialChangeUnknown(stage string) error {
	return ErrAccountCredentialChangeUnknown.WithMetadata(map[string]string{"outcome": "unknown", "stage": stage})
}

func (s *AuthService) validateAccountCredentialSnapshot(ctx context.Context, old *JWTClaims, mutated *User) error {
	if err := s.validateSessionEpoch(ctx, mutated.ID, old.SessionEpoch); err != nil {
		return err
	}
	if err := s.validateAccountSessionFamily(ctx, mutated.ID, old.SessionID, old.AccountGeneration); err != nil {
		return err
	}
	current, err := s.userRepo.GetByID(ctx, mutated.ID)
	if err != nil {
		return err
	}
	if current == nil || current.ID != mutated.ID || !current.IsActive() || current.DeletedAt != nil || current.Role != mutated.Role || current.Email != mutated.Email || current.PasswordHash != mutated.PasswordHash || resolvedTokenVersion(current) != resolvedTokenVersion(mutated) || accountCenterGeneration(current, old.SessionEpoch) != accountCenterGeneration(mutated, old.SessionEpoch) {
		return ErrTokenRevoked
	}
	// Recheck after the identity read as well: a revoke may have committed
	// while that read was in flight. Tokens still carry only the captured epoch.
	if err := s.validateSessionEpoch(ctx, mutated.ID, old.SessionEpoch); err != nil {
		return err
	}
	return s.validateAccountSessionFamily(ctx, mutated.ID, old.SessionID, old.AccountGeneration)
}

// Native login historically tolerates index failures. The explicit credential
// endpoints require the complete stored family and surface ambiguous IO instead.
type accountCredentialMintCache struct {
	RefreshTokenCache
	indexErr error
}

func (c *accountCredentialMintCache) AddToUserTokenSet(ctx context.Context, id int64, hash string, ttl time.Duration) error {
	err := c.RefreshTokenCache.AddToUserTokenSet(ctx, id, hash, ttl)
	if err != nil {
		c.indexErr = err
	}
	return err
}
func (c *accountCredentialMintCache) AddToFamilyTokenSet(ctx context.Context, id, hash string, ttl time.Duration) error {
	err := c.RefreshTokenCache.AddToFamilyTokenSet(ctx, id, hash, ttl)
	if err != nil {
		c.indexErr = err
	}
	return err
}

func (s *AuthService) mintAccountCredentialChange(ctx context.Context, old *JWTClaims, mutated *User) (*AccountCredentialChange, error) {
	if err := s.validateAccountCredentialSnapshot(ctx, old, mutated); err != nil {
		return nil, credentialChangeUnknown("authority_before_mint")
	}
	// Only generation changes. The already verified signed factors and their
	// original timestamp survive; neither mutation nor mint renews proof freshness.
	proof := NativeAuthenticationFromClaims(old)
	proof.Generation = accountCenterGeneration(mutated, old.SessionEpoch)
	strictCache := &accountCredentialMintCache{RefreshTokenCache: s.refreshTokenCache}
	mint := *s
	mint.refreshTokenCache = strictCache
	pair, err := mint.generateTokenPairForEpoch(WithNativeAuthenticationProof(ctx, proof), mutated, "", old.SessionEpoch)
	if err != nil || strictCache.indexErr != nil {
		if pair != nil {
			_ = s.refreshTokenCache.DeleteRefreshToken(ctx, hashToken(pair.RefreshToken))
		}
		return nil, credentialChangeUnknown("mint")
	}
	if err := s.validateAccountCredentialSnapshot(ctx, old, mutated); err != nil {
		// This token was never returned. Retire it once, without retrying uncertain IO.
		_ = s.refreshTokenCache.DeleteRefreshToken(ctx, hashToken(pair.RefreshToken))
		return nil, credentialChangeUnknown("authority_after_mint")
	}
	return &AccountCredentialChange{TokenPair: *pair, User: mutated}, nil
}
