package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	passkeySessionRegistration     = "registration"
	passkeySessionLogin            = "login"
	passkeySessionReauthentication = "account_reauthentication"
	passkeyManagementTTL           = RecentAccountAuthenticationTTL
	passkeySessionTTL              = 5 * time.Minute
	defaultPasskeyName             = "Passkey"
	maxPasskeyNameLength           = 100
)

var (
	ErrPasskeysDisabled = infraerrors.Forbidden("PASSKEY_DISABLED", "passkey authentication is not enabled")
	ErrPasskeyNotFound  = infraerrors.NotFound("PASSKEY_NOT_FOUND", "passkey not found")
	ErrPasskeyExists    = infraerrors.Conflict("PASSKEY_ALREADY_EXISTS", "this passkey is already registered")
	ErrPasskeySession   = infraerrors.BadRequest("PASSKEY_SESSION_INVALID", "passkey session is invalid or expired")
	ErrPasskeyVerify    = infraerrors.Unauthorized("PASSKEY_VERIFICATION_FAILED", "passkey verification failed")
)

// PasskeyCredentialRecord is the persistence representation used by the
// WebAuthn service. Credential contains the complete WebAuthn credential record
// so future library versions can continue to validate and update it.
type PasskeyCredentialRecord struct {
	ID         int64
	UserID     int64
	UserHandle []byte
	Name       string
	Credential webauthn.Credential
	LastUsedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type PasskeyRepository interface {
	EnsureUserHandle(ctx context.Context, userID int64, candidate []byte) ([]byte, error)
	GetUserHandle(ctx context.Context, userID int64) ([]byte, error)
	GetByCredentialID(ctx context.Context, credentialID []byte) (*PasskeyCredentialRecord, error)
	ListByUserID(ctx context.Context, userID int64) ([]PasskeyCredentialRecord, error)
	Create(ctx context.Context, record *PasskeyCredentialRecord) (*PasskeyCredentialRecord, error)
	UpdateCredential(ctx context.Context, userID int64, credential *webauthn.Credential, usedAt time.Time) error
	Rename(ctx context.Context, userID, credentialID int64, name string) error
	Delete(ctx context.Context, userID, credentialID int64) error
}

type PasskeySession struct {
	Kind     string               `json:"kind"`
	UserID   int64                `json:"user_id,omitempty"`
	WebAuthn webauthn.SessionData `json:"webauthn"`
}

type PasskeySessionStore interface {
	Store(ctx context.Context, session *PasskeySession, ttl time.Duration) (string, error)
	Consume(ctx context.Context, token string) (*PasskeySession, error)
}

type PasskeyCredentialSummary struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Backup     bool       `json:"backup"`
}

type passkeyUser struct {
	account     *User
	handle      []byte
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte {
	return u.handle
}

func (u *passkeyUser) WebAuthnName() string {
	return u.account.Email
}

func (u *passkeyUser) WebAuthnDisplayName() string {
	if name := strings.TrimSpace(u.account.Username); name != "" {
		return name
	}
	return u.account.Email
}

func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

type PasskeyService struct {
	enabled  bool
	webAuthn *webauthn.WebAuthn
	repo     PasskeyRepository
	sessions PasskeySessionStore
	userRepo UserRepository
}

func NewPasskeyService(
	cfg *config.Config,
	repo PasskeyRepository,
	sessions PasskeySessionStore,
	userRepo UserRepository,
) (*PasskeyService, error) {
	s := &PasskeyService{
		repo:     repo,
		sessions: sessions,
		userRepo: userRepo,
	}
	if cfg == nil || !cfg.WebAuthn.Enabled {
		return s, nil
	}

	instance, err := webauthn.New(&webauthn.Config{
		RPDisplayName: cfg.WebAuthn.RPDisplayName,
		RPID:          cfg.WebAuthn.RPID,
		RPOrigins:     cfg.WebAuthn.RPOrigins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize WebAuthn: %w", err)
	}
	s.enabled = true
	s.webAuthn = instance
	return s, nil
}

func (s *PasskeyService) Enabled() bool {
	return s != nil && s.enabled && s.webAuthn != nil
}

func (s *PasskeyService) requireEnabled() error {
	if !s.Enabled() {
		return ErrPasskeysDisabled
	}
	return nil
}

// verifyPasskeyPassword gates credential enrollment and revocation with the
// account password so a hijacked session cannot silently add or remove
// passkeys. The password is used instead of TOTP step-up so the guard also
// works on deployments without a TOTP encryption key configured.
func verifyPasskeyPassword(user *User, password string) error {
	if password == "" {
		return ErrPasswordRequired
	}
	if user == nil || !user.CheckPassword(password) {
		return ErrPasswordIncorrect
	}
	return nil
}

func (s *PasskeyService) BeginRegistration(
	ctx context.Context,
	userID int64,
	password string,
) (creation *protocol.CredentialCreation, sessionToken string, err error) {
	if err = s.requireEnabled(); err != nil {
		return nil, "", err
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if !user.IsActive() {
		return nil, "", ErrUserNotActive
	}
	if err = verifyPasskeyPassword(user, password); err != nil {
		return nil, "", err
	}

	candidate := make([]byte, 32)
	if _, err = rand.Read(candidate); err != nil {
		return nil, "", fmt.Errorf("generate passkey user handle: %w", err)
	}
	handle, err := s.repo.EnsureUserHandle(ctx, userID, candidate)
	if err != nil {
		return nil, "", err
	}
	waUser, err := s.loadWebAuthnUser(ctx, user, handle)
	if err != nil {
		return nil, "", err
	}

	creation, session, err := s.webAuthn.BeginRegistration(
		waUser,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(waUser.credentials).CredentialDescriptors()),
		webauthn.WithExtensions(protocol.AuthenticationExtensions{"credProps": true}),
		func(options *protocol.PublicKeyCredentialCreationOptions) {
			options.Timeout = int(passkeyManagementTTL.Milliseconds())
		},
	)
	if err != nil {
		return nil, "", fmt.Errorf("begin passkey registration: %w", err)
	}
	session.Expires = time.Now().Add(passkeyManagementTTL)
	sessionToken, err = s.sessions.Store(ctx, &PasskeySession{
		Kind:     passkeySessionRegistration,
		UserID:   userID,
		WebAuthn: *session,
	}, passkeyManagementTTL)
	if err != nil {
		return nil, "", err
	}
	return creation, sessionToken, nil
}

func (s *PasskeyService) FinishRegistration(
	ctx context.Context,
	userID int64,
	sessionToken, name string,
	request *http.Request,
) (*PasskeyCredentialSummary, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	session, err := s.sessions.Consume(ctx, sessionToken)
	if err != nil {
		return nil, err
	}
	if session == nil || session.Kind != passkeySessionRegistration || session.UserID != userID {
		return nil, ErrPasskeySession
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	handle, err := s.repo.GetUserHandle(ctx, userID)
	if err != nil {
		return nil, err
	}
	waUser, err := s.loadWebAuthnUser(ctx, user, handle)
	if err != nil {
		return nil, err
	}
	credential, err := s.webAuthn.FinishRegistration(waUser, session.WebAuthn, request)
	if err != nil {
		return nil, ErrPasskeyVerify
	}

	record, err := s.repo.Create(ctx, &PasskeyCredentialRecord{
		UserID:     userID,
		UserHandle: handle,
		Name:       normalizePasskeyName(name),
		Credential: *credential,
	})
	if err != nil {
		return nil, err
	}
	return passkeySummary(record), nil
}

func (s *PasskeyService) BeginLogin(ctx context.Context) (*protocol.CredentialAssertion, string, error) {
	return s.beginAssertion(ctx, passkeySessionLogin, 0, passkeySessionTTL)
}

// Account management uses a distinct, user-bound challenge; ordinary login stays short.
func (s *PasskeyService) BeginAccountReauthentication(ctx context.Context, userID int64) (*protocol.CredentialAssertion, string, error) {
	if userID <= 0 {
		return nil, "", ErrPasskeySession
	}
	return s.beginAssertion(ctx, passkeySessionReauthentication, userID, passkeyManagementTTL)
}

func (s *PasskeyService) beginAssertion(ctx context.Context, kind string, userID int64, ttl time.Duration) (assertion *protocol.CredentialAssertion, sessionToken string, err error) {
	if err = s.requireEnabled(); err != nil {
		return nil, "", err
	}
	options := []webauthn.LoginOption{webauthn.WithUserVerification(protocol.VerificationRequired)}
	if kind == passkeySessionReauthentication {
		options = append(options, func(options *protocol.PublicKeyCredentialRequestOptions) {
			options.Timeout = int(ttl.Milliseconds())
		})
	}
	assertion, session, err := s.webAuthn.BeginDiscoverableLogin(options...)
	if err != nil {
		return nil, "", fmt.Errorf("begin passkey login: %w", err)
	}
	if kind == passkeySessionReauthentication {
		session.Expires = time.Now().Add(ttl)
	}
	sessionToken, err = s.sessions.Store(ctx, &PasskeySession{
		Kind:     kind,
		UserID:   userID,
		WebAuthn: *session,
	}, ttl)
	if err != nil {
		return nil, "", err
	}
	return assertion, sessionToken, nil
}

func (s *PasskeyService) FinishLogin(ctx context.Context, sessionToken string, request *http.Request) (*User, error) {
	return s.finishAssertion(ctx, passkeySessionLogin, 0, sessionToken, request)
}

func (s *PasskeyService) FinishAccountReauthentication(ctx context.Context, userID int64, sessionToken string, request *http.Request) (*User, error) {
	if userID <= 0 {
		return nil, ErrPasskeySession
	}
	return s.finishAssertion(ctx, passkeySessionReauthentication, userID, sessionToken, request)
}

func (s *PasskeyService) finishAssertion(ctx context.Context, kind string, userID int64, sessionToken string, request *http.Request) (*User, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	session, err := s.sessions.Consume(ctx, sessionToken)
	if err != nil {
		return nil, err
	}
	if session == nil || session.Kind != kind || session.UserID != userID {
		return nil, ErrPasskeySession
	}

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		record, lookupErr := s.repo.GetByCredentialID(ctx, rawID)
		if lookupErr != nil || record == nil || !bytes.Equal(record.UserHandle, userHandle) || (kind == passkeySessionReauthentication && record.UserID != userID) {
			return nil, ErrPasskeyVerify
		}
		account, lookupErr := s.userRepo.GetByID(ctx, record.UserID)
		if lookupErr != nil || account == nil || !account.IsActive() {
			return nil, ErrPasskeyVerify
		}
		return s.loadWebAuthnUser(ctx, account, record.UserHandle)
	}

	validatedUser, credential, err := s.webAuthn.FinishPasskeyLogin(handler, session.WebAuthn, request)
	if err != nil {
		return nil, ErrPasskeyVerify
	}
	waUser, ok := validatedUser.(*passkeyUser)
	if !ok || waUser.account == nil {
		return nil, ErrPasskeyVerify
	}
	if err = s.repo.UpdateCredential(ctx, waUser.account.ID, credential, time.Now().UTC()); err != nil {
		return nil, err
	}
	return waUser.account, nil
}

func (s *PasskeyService) List(ctx context.Context, userID int64) ([]PasskeyCredentialSummary, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	records, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]PasskeyCredentialSummary, 0, len(records))
	for i := range records {
		result = append(result, *passkeySummary(&records[i]))
	}
	return result, nil
}

func (s *PasskeyService) Rename(ctx context.Context, userID, credentialID int64, name string) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	return s.repo.Rename(ctx, userID, credentialID, normalizePasskeyName(name))
}

func (s *PasskeyService) Delete(ctx context.Context, userID, credentialID int64, password string) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if err = verifyPasskeyPassword(user, password); err != nil {
		return err
	}
	return s.repo.Delete(ctx, userID, credentialID)
}

func (s *PasskeyService) loadWebAuthnUser(
	ctx context.Context,
	user *User,
	handle []byte,
) (*passkeyUser, error) {
	records, err := s.repo.ListByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(records))
	for i := range records {
		credentials = append(credentials, records[i].Credential)
	}
	return &passkeyUser{account: user, handle: handle, credentials: credentials}, nil
}

func normalizePasskeyName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultPasskeyName
	}
	runes := []rune(name)
	if len(runes) > maxPasskeyNameLength {
		name = string(runes[:maxPasskeyNameLength])
	}
	return name
}

func passkeySummary(record *PasskeyCredentialRecord) *PasskeyCredentialSummary {
	return &PasskeyCredentialSummary{
		ID:         record.ID,
		Name:       record.Name,
		CreatedAt:  record.CreatedAt,
		LastUsedAt: record.LastUsedAt,
		Backup:     record.Credential.Flags.BackupState,
	}
}

var _ webauthn.User = (*passkeyUser)(nil)
