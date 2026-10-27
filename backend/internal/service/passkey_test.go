package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/require"
)

func TestNormalizePasskeyName(t *testing.T) {
	require.Equal(t, defaultPasskeyName, normalizePasskeyName("   "))
	require.Equal(t, "Laptop", normalizePasskeyName("  Laptop  "))

	longName := strings.Repeat("密", maxPasskeyNameLength+10)
	require.Len(t, []rune(normalizePasskeyName(longName)), maxPasskeyNameLength)
}

func TestPasskeySummaryReportsCurrentBackupState(t *testing.T) {
	record := &PasskeyCredentialRecord{
		Credential: webauthn.Credential{
			Flags: webauthn.CredentialFlags{BackupEligible: true},
		},
	}
	require.False(t, passkeySummary(record).Backup)

	record.Credential.Flags.BackupState = true
	require.True(t, passkeySummary(record).Backup)
}

// 桩仅实现测试所需方法；未桩方法调用即 panic（嵌入 nil 接口）。
type passkeyPwUserRepoStub struct {
	UserRepository
	user *User
}

func (s *passkeyPwUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	return s.user, nil
}

type passkeyPwRepoStub struct {
	PasskeyRepository
	handleCalled bool
	deleteCalled bool
}

func (s *passkeyPwRepoStub) EnsureUserHandle(_ context.Context, _ int64, candidate []byte) ([]byte, error) {
	s.handleCalled = true
	return candidate, nil
}

func (s *passkeyPwRepoStub) ListByUserID(context.Context, int64) ([]PasskeyCredentialRecord, error) {
	return nil, nil
}

func (s *passkeyPwRepoStub) Delete(context.Context, int64, int64) error {
	s.deleteCalled = true
	return nil
}

type passkeyPwSessionStoreStub struct {
	PasskeySessionStore
}

func (s *passkeyPwSessionStoreStub) Store(context.Context, *PasskeySession, time.Duration) (string, error) {
	return "session-token", nil
}

func newPasskeyPwService(t *testing.T, user *User) (*PasskeyService, *passkeyPwRepoStub) {
	t.Helper()
	repo := &passkeyPwRepoStub{}
	svc, err := NewPasskeyService(&config.Config{WebAuthn: config.WebAuthnConfig{
		Enabled:       true,
		RPDisplayName: "Sub2API",
		RPID:          "sub2api.example.com",
		RPOrigins:     []string{"https://sub2api.example.com"},
	}}, repo, &passkeyPwSessionStoreStub{}, &passkeyPwUserRepoStub{user: user})
	require.NoError(t, err)
	return svc, repo
}

// 注册与吊销必须验证账号密码：被窃会话不得静默添加/移除凭据。
// 用密码而非 TOTP step-up，保证未配置 TOTP 加密密钥的部署同样受保护。
func TestPasskeyEnrollmentAndRevocationRequireAccountPassword(t *testing.T) {
	user := &User{ID: 7, Email: "user@example.com", Status: StatusActive}
	require.NoError(t, user.SetPassword("correct-password"))
	svc, repo := newPasskeyPwService(t, user)

	_, _, err := svc.BeginRegistration(context.Background(), user.ID, "")
	require.ErrorIs(t, err, ErrPasswordRequired)
	_, _, err = svc.BeginRegistration(context.Background(), user.ID, "wrong-password")
	require.ErrorIs(t, err, ErrPasswordIncorrect)
	require.False(t, repo.handleCalled)

	creation, token, err := svc.BeginRegistration(context.Background(), user.ID, "correct-password")
	require.NoError(t, err)
	require.NotNil(t, creation)
	require.Equal(t, "session-token", token)
	require.True(t, repo.handleCalled)

	err = svc.Delete(context.Background(), user.ID, 1, "")
	require.ErrorIs(t, err, ErrPasswordRequired)
	err = svc.Delete(context.Background(), user.ID, 1, "wrong-password")
	require.ErrorIs(t, err, ErrPasswordIncorrect)
	require.False(t, repo.deleteCalled)

	require.NoError(t, svc.Delete(context.Background(), user.ID, 1, "correct-password"))
	require.True(t, repo.deleteCalled)
}

// Capture the actual session and TTL sent to the persistence boundary.
type passkeyWindowStore struct {
	PasskeySessionStore
	sessions map[string]*PasskeySession
	ttls     map[string]time.Duration
}

func (s *passkeyWindowStore) Store(_ context.Context, session *PasskeySession, ttl time.Duration) (string, error) {
	token := fmt.Sprintf("challenge-%d", len(s.sessions))
	s.sessions[token], s.ttls[token] = session, ttl
	return token, nil
}
func (s *passkeyWindowStore) Consume(_ context.Context, token string) (*PasskeySession, error) {
	session := s.sessions[token]
	delete(s.sessions, token)
	return session, nil
}
func TestPasskeyManagementWindowKeepsOrdinaryLoginSeparate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		user := &User{ID: 7, Email: "user@example.test", Status: StatusActive}
		require.NoError(t, user.SetPassword("synthetic-password"))
		svc, _ := newPasskeyPwService(t, user)
		store := &passkeyWindowStore{sessions: map[string]*PasskeySession{}, ttls: map[string]time.Duration{}}
		svc.sessions = store
		started := time.Now()
		creation, registration, err := svc.BeginRegistration(ctx, user.ID, "synthetic-password")
		require.NoError(t, err)
		reauthOptions, reauth, err := svc.BeginAccountReauthentication(ctx, user.ID)
		require.NoError(t, err)
		loginOptions, login, err := svc.BeginLogin(ctx)
		require.NoError(t, err)
		require.Equal(t, 1800000, creation.Response.Timeout)
		require.Equal(t, 1800000, reauthOptions.Response.Timeout)
		require.Equal(t, int(svc.webAuthn.Config.Timeouts.Login.Timeout.Milliseconds()), loginOptions.Response.Timeout)
		for _, token := range []string{registration, reauth} {
			require.Equal(t, 30*time.Minute, store.ttls[token])
			require.Equal(t, started.Add(30*time.Minute), store.sessions[token].WebAuthn.Expires)
			require.Equal(t, user.ID, store.sessions[token].UserID)
		}
		require.Equal(t, 5*time.Minute, store.ttls[login])
		require.True(t, store.sessions[login].WebAuthn.Expires.IsZero())
		require.Equal(t, passkeySessionReauthentication, store.sessions[reauth].Kind)
		// Wrong purpose/account must fail before reading any WebAuthn response.
		_, err = svc.FinishLogin(ctx, reauth, nil)
		require.ErrorIs(t, err, ErrPasskeySession)
		_, err = svc.FinishAccountReauthentication(ctx, user.ID, login, nil)
		require.ErrorIs(t, err, ErrPasskeySession)
		_, reauth, err = svc.BeginAccountReauthentication(ctx, user.ID)
		require.NoError(t, err)
		_, err = svc.FinishAccountReauthentication(ctx, user.ID+1, reauth, nil)
		require.ErrorIs(t, err, ErrPasskeySession)
		_, _, err = svc.BeginAccountReauthentication(ctx, 0)
		require.ErrorIs(t, err, ErrPasskeySession)
	})
}
