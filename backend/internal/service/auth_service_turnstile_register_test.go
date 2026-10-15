//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type turnstileVerifierSpy struct {
	called    int
	lastToken string
	result    *TurnstileVerifyResponse
	err       error
}

func (s *turnstileVerifierSpy) VerifyToken(_ context.Context, _ string, token, _ string) (*TurnstileVerifyResponse, error) {
	s.called++
	s.lastToken = token
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &TurnstileVerifyResponse{Success: true}, nil
}

func newAuthServiceForRegisterTurnstileTest(settings map[string]string, verifier TurnstileVerifier) *AuthService {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Mode: "release",
		},
		Turnstile: config.TurnstileConfig{
			Required: true,
		},
	}

	settingService := NewSettingService(&settingRepoStub{values: settings}, cfg)
	turnstileService := NewTurnstileServiceWithReplayStore(settingService, verifier, &turnstileReplayStoreStub{claimed: true})

	return NewAuthService(
		nil, // entClient
		&userRepoStub{},
		nil, // redeemRepo
		nil, // refreshTokenCache
		cfg,
		settingService,
		nil, // emailService
		turnstileService,
		nil, // emailQueueService
		nil, // promoService
		nil, // defaultSubAssigner
		nil, // affiliateService
		nil, // userPlatformQuotaRepo
	)
}

func TestAuthService_VerifyTurnstileForRegister_SkipWhenEmailVerifyCodeProvided(t *testing.T) {
	verifier := &turnstileVerifierSpy{}
	service := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled:  "true",
		SettingKeyTurnstileEnabled:    "true",
		SettingKeyTurnstileSecretKey:  "secret",
		SettingKeyRegistrationEnabled: "true",
	}, verifier)

	err := service.VerifyTurnstileForRegister(context.Background(), "", "127.0.0.1", "123456")
	require.NoError(t, err)
	require.Equal(t, 0, verifier.called)
}

func TestAuthService_VerifyTurnstileForRegister_RequireWhenVerifyCodeMissing(t *testing.T) {
	verifier := &turnstileVerifierSpy{}
	service := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled: "true",
		SettingKeyTurnstileEnabled:   "true",
		SettingKeyTurnstileSecretKey: "secret",
	}, verifier)

	err := service.VerifyTurnstileForRegister(context.Background(), "", "127.0.0.1", "")
	require.ErrorIs(t, err, ErrTurnstileVerificationFailed)
}

func TestAuthService_VerifyTurnstileForRegister_NoSkipWhenEmailVerifyDisabled(t *testing.T) {
	verifier := &turnstileVerifierSpy{}
	service := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled: "false",
		SettingKeyTurnstileEnabled:   "true",
		SettingKeyTurnstileSecretKey: "secret",
	}, verifier)

	err := service.VerifyTurnstileForRegister(context.Background(), "turnstile-token", "127.0.0.1", "123456")
	require.NoError(t, err)
	require.Equal(t, 1, verifier.called)
	require.Equal(t, "turnstile-token", verifier.lastToken)
}

func TestAuthService_VerifyTurnstileForRegister_StrictContextRequiresFreshProofWithEmailCode(t *testing.T) {
	verifier := &turnstileVerifierSpy{result: &TurnstileVerifyResponse{
		Success: true, Hostname: "app.example.com", Action: "auth", ChallengeTS: time.Now().UTC().Format(time.RFC3339),
	}}
	svc := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled: "true",
		SettingKeyTurnstileEnabled:   "true",
		SettingKeyTurnstileSecretKey: "secret",
	}, verifier)
	svc.cfg.Turnstile.StrictContext = true
	svc.cfg.Turnstile.ExpectedHostnames = []string{"app.example.com"}

	// Email codes may originate from another flow, so supplying one alone must
	// not bypass the final registration's independent Turnstile validation.
	err := svc.VerifyTurnstileForRegister(context.Background(), "", "192.0.2.1", "123456")
	require.ErrorIs(t, err, ErrTurnstileVerificationFailed)
	require.Zero(t, verifier.called)

	err = svc.VerifyTurnstileForRegister(context.Background(), "fresh-registration-token", "192.0.2.1", "123456")
	require.NoError(t, err)
	require.Equal(t, 1, verifier.called)
	require.Equal(t, "fresh-registration-token", verifier.lastToken)
}

func TestAuthService_VerifyCaptchaForRegister_StrictContextRejectsReplayedProofWithEmailCode(t *testing.T) {
	verifier := &turnstileVerifierSpy{result: &TurnstileVerifyResponse{
		Success: false, ErrorCodes: []string{"timeout-or-duplicate"},
	}}
	svc := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled: "true",
		SettingKeyTurnstileEnabled:   "true",
		SettingKeyTurnstileSecretKey: "secret",
	}, verifier)
	svc.cfg.Turnstile.StrictContext = true
	svc.cfg.Turnstile.ExpectedHostnames = []string{"app.example.com"}

	err := svc.VerifyCaptchaForRegister(context.Background(), CaptchaProof{TurnstileToken: "send-code-token"}, "192.0.2.1", "123456")
	require.ErrorIs(t, err, ErrTurnstileVerificationFailed)
	require.Equal(t, 1, verifier.called)
}

func TestAuthService_VerifyCaptchaForRegister_StrictRequiredRejectsDisabledProviderWithEmailCode(t *testing.T) {
	verifier := &turnstileVerifierSpy{}
	svc := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled: "true",
		SettingKeyTurnstileEnabled:   "false",
	}, verifier)
	svc.cfg.Turnstile.StrictContext = true
	svc.cfg.Turnstile.ExpectedHostnames = []string{"app.example.com"}

	err := svc.VerifyCaptchaForRegister(context.Background(), CaptchaProof{}, "192.0.2.1", "123456")
	require.ErrorIs(t, err, ErrTurnstileNotConfigured)
	require.Zero(t, verifier.called)
}

type registerCaptchaProviderErrorRepo struct {
	*settingRepoStub
}

func (r *registerCaptchaProviderErrorRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("provider settings unavailable")
}

func TestAuthService_VerifyCaptchaForRegister_StrictContextRejectsProviderReadFailureWithEmailCode(t *testing.T) {
	verifier := &turnstileVerifierSpy{}
	svc := newAuthServiceForRegisterTurnstileTest(map[string]string{
		SettingKeyEmailVerifyEnabled: "true",
		SettingKeyTurnstileEnabled:   "true",
		SettingKeyTurnstileSecretKey: "secret",
	}, verifier)
	svc.cfg.Turnstile.StrictContext = true
	svc.cfg.Turnstile.ExpectedHostnames = []string{"app.example.com"}
	svc.settingService.settingRepo = &registerCaptchaProviderErrorRepo{
		settingRepoStub: &settingRepoStub{values: map[string]string{SettingKeyEmailVerifyEnabled: "true"}},
	}

	err := svc.VerifyCaptchaForRegister(context.Background(), CaptchaProof{TurnstileToken: "fresh-token"}, "192.0.2.1", "123456")
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.Zero(t, verifier.called)
}
