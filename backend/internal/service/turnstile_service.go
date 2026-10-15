package service

import (
	"context"
	"fmt"
	"strings"

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

// TurnstileService Turnstile 验证服务
type TurnstileService struct {
	settingService *SettingService
	verifier       TurnstileVerifier
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
	}
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
	if strings.TrimSpace(token) == "" || len(token) > 2048 {
		logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Token is empty or too long")
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

	logger.LegacyPrintf("service.turnstile", "%s", "[Turnstile] Verification successful")
	return nil
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
