package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const turnstileAuthAction = "auth"

var (
	ErrTurnstileVerificationFailed = infraerrors.BadRequest("TURNSTILE_VERIFICATION_FAILED", "turnstile verification failed")
	ErrTurnstileNotConfigured      = infraerrors.ServiceUnavailable("TURNSTILE_NOT_CONFIGURED", "turnstile not configured")
	ErrTurnstileInvalidSecretKey   = infraerrors.BadRequest("TURNSTILE_INVALID_SECRET_KEY", "invalid turnstile secret key")
)

// TurnstileVerifier 验证 Turnstile token 的接口
type TurnstileVerifier interface {
	VerifyToken(ctx context.Context, secretKey, token, remoteIP string) (*TurnstileVerifyResponse, error)
}

// TurnstileReplayStore atomically claims a SHA-256 token digest across instances.
// A claim is retained even when verification fails or its outcome is unknown.
type TurnstileReplayStore interface {
	ClaimToken(ctx context.Context, tokenHash string) (bool, error)
}

// TurnstileService Turnstile 验证服务
type TurnstileService struct {
	settingService *SettingService
	verifier       TurnstileVerifier
	replayStore    TurnstileReplayStore
	now            func() time.Time
}

// TurnstileVerifyResponse Cloudflare Turnstile 验证响应
type TurnstileVerifyResponse struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	ErrorCodes  []string `json:"error-codes"`
	Action      string   `json:"action"`
	CData       string   `json:"cdata"`
}

// NewTurnstileService 创建 Turnstile 服务实例
func NewTurnstileService(settingService *SettingService, verifier TurnstileVerifier) *TurnstileService {
	return &TurnstileService{
		settingService: settingService,
		verifier:       verifier,
		now:            time.Now,
	}
}

// NewTurnstileServiceWithReplayStore is the production Wire constructor. Strict
// deployments also reject requests at runtime if this dependency is missing.
func NewTurnstileServiceWithReplayStore(settingService *SettingService, verifier TurnstileVerifier, replayStore TurnstileReplayStore) *TurnstileService {
	svc := NewTurnstileService(settingService, verifier)
	svc.replayStore = replayStore
	return svc
}

// VerifyToken 验证 Turnstile token
func (s *TurnstileService) VerifyToken(ctx context.Context, token string, remoteIP string) error {
	if s == nil || s.settingService == nil {
		return ErrTurnstileNotConfigured
	}
	// 检查是否启用 Turnstile
	if !s.settingService.IsTurnstileEnabled(ctx) {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Disabled, skipping verification")
		return nil
	}

	// 获取 Secret Key
	secretKey := s.settingService.GetTurnstileSecretKey(ctx)
	return s.VerifyTokenWithSecret(ctx, secretKey, token, remoteIP)
}

func (s *TurnstileService) VerifyTokenWithSecret(ctx context.Context, secretKey, token, remoteIP string) error {
	if strings.TrimSpace(secretKey) == "" {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Secret key not configured")
		return ErrTurnstileNotConfigured
	}

	// Cloudflare accepts tokens up to 2048 characters. Reject invalid input
	// before making the external verification request.
	if token == "" || token != strings.TrimSpace(token) || len(token) > 2048 {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Token has invalid length or surrounding whitespace")
		return ErrTurnstileVerificationFailed
	}

	logger.LegacyPrintf("service.turnstile", "[Turnstile] Verifying token for IP: %s", remoteIP)
	if s == nil || s.verifier == nil {
		return ErrTurnstileNotConfigured
	}
	hostnames, err := s.expectedContextHostnames()
	if err != nil {
		return err
	}
	if s.replayStore == nil {
		if s.settingService.cfg.Turnstile.StrictContext {
			return ErrTurnstileNotConfigured
		}
	} else {
		digest := sha256.Sum256([]byte(token))
		claimed, err := s.replayStore.ClaimToken(ctx, hex.EncodeToString(digest[:]))
		if err != nil {
			logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Replay protection unavailable")
			return ErrServiceUnavailable
		}
		if !claimed {
			logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Token was already used")
			return ErrTurnstileVerificationFailed
		}
	}
	// Never release the claim: a timed-out upstream request may have consumed
	// the token, and a failed business operation must obtain a fresh proof.
	result, err := s.verifier.VerifyToken(ctx, secretKey, token, remoteIP)
	if err != nil {
		logger.LegacyPrintf("service.turnstile", "[Turnstile] Request failed: %v", err)
		return fmt.Errorf("send request: %w", err)
	}

	if result == nil {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Verification returned an empty response")
		return ErrTurnstileVerificationFailed
	}
	if !result.Success {
		logger.LegacyPrintf("service.turnstile", "[Turnstile] Verification failed, error codes: %v", result.ErrorCodes)
		return ErrTurnstileVerificationFailed
	}
	if !matchesTurnstileContext(result, hostnames) {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Verification hostname or action did not match")
		return ErrTurnstileVerificationFailed
	}
	if s.settingService.cfg.Turnstile.StrictContext && !isTurnstileChallengeFresh(result.ChallengeTS, s.now()) {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Verification challenge time is invalid or expired")
		return ErrTurnstileVerificationFailed
	}

	logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Verification successful")
	return nil
}

func isTurnstileChallengeFresh(raw string, now time.Time) bool {
	challengedAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	age := now.Sub(challengedAt)
	return age >= -time.Minute && age <= 5*time.Minute
}

// expectedContextHostnames permits legacy verification only when StrictContext
// is false and the hostname list is absent. Missing service configuration, or an
// explicitly configured list without usable hosts, always fails closed.
func (s *TurnstileService) expectedContextHostnames() ([]string, error) {
	if s.settingService == nil || s.settingService.cfg == nil {
		return nil, ErrTurnstileNotConfigured
	}
	cfg := s.settingService.cfg.Turnstile
	hostnames := make([]string, 0, len(cfg.ExpectedHostnames))
	for _, hostname := range cfg.ExpectedHostnames {
		if expected := strings.TrimSpace(hostname); expected != "" {
			hostnames = append(hostnames, expected)
		}
	}
	if len(hostnames) == 0 && (cfg.StrictContext || len(cfg.ExpectedHostnames) != 0) {
		return nil, ErrTurnstileNotConfigured
	}
	return hostnames, nil
}

func matchesTurnstileContext(result *TurnstileVerifyResponse, hostnames []string) bool {
	if len(hostnames) == 0 {
		return true
	}
	if result.Action != turnstileAuthAction || result.Hostname == "" {
		return false
	}
	for _, hostname := range hostnames {
		if result.Hostname == hostname {
			return true
		}
	}
	return false
}

// IsEnabled 检查 Turnstile 是否启用
func (s *TurnstileService) IsEnabled(ctx context.Context) bool {
	return s.settingService.IsTurnstileEnabled(ctx)
}

// ValidateSecretKey 验证 Turnstile Secret Key 是否有效
func (s *TurnstileService) ValidateSecretKey(ctx context.Context, secretKey string) error {
	if s == nil || s.verifier == nil {
		return ErrTurnstileNotConfigured
	}
	// 发送一个测试token的验证请求来检查secret_key是否有效
	result, err := s.verifier.VerifyToken(ctx, secretKey, "test-validation", "")
	if err != nil {
		return fmt.Errorf("validate secret key: %w", err)
	}
	if result == nil {
		return ErrTurnstileVerificationFailed
	}

	// The deliberately invalid test token proves the secret is accepted only
	// when Cloudflare specifically rejects that token. Other responses, including
	// success or internal errors, do not establish that the secret is valid.
	invalidResponse := false
	unexpectedCode := false
	for _, code := range result.ErrorCodes {
		switch code {
		case "invalid-input-secret", "missing-input-secret":
			return ErrTurnstileInvalidSecretKey
		case "invalid-input-response":
			invalidResponse = true
		default:
			unexpectedCode = true
		}
	}
	if !result.Success && invalidResponse && !unexpectedCode {
		return nil
	}
	return ErrTurnstileVerificationFailed
}
