package service

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// AccountSecurityResetRepository is an optional capability of the recovery
// repository. The base recovery interface stays unchanged.
type AccountSecurityResetRepository interface {
	ResetAccountSecurity(ctx context.Context, targetID int64, newEpoch string) error
}

// AccountSecurityResetNoteMaxBytes bounds the manual verification narrative.
const AccountSecurityResetNoteMaxBytes = 1000

var (
	ErrAccountSecurityResetInput   = infraerrors.BadRequest("ACCOUNT_SECURITY_RESET_CONFIRMATION_REQUIRED", "explicit confirmation and a verification note of 1 to 1000 bytes are required")
	ErrAccountSecurityResetStorage = infraerrors.New(http.StatusServiceUnavailable, "ACCOUNT_SECURITY_RESET_STORAGE_UNAVAILABLE", "account security reset storage unavailable")
	ErrAccountSecurityResetUnknown = infraerrors.New(http.StatusServiceUnavailable, "ACCOUNT_SECURITY_RESET_OUTCOME_UNKNOWN", "reset outcome requires verification; session revocation remains in force if committed")
)

func (s *AuthService) requireAccountCenterEnabled() error {
	if !s.AccountCenterEnabled() {
		return ErrServiceUnavailable
	}
	return nil
}

// cleanRedisRefreshSessions deliberately does not write the durable epoch.
// ResetAccountSecurity already committed that fence in its SQL transaction.
func (s *AuthService) cleanRedisRefreshSessions(ctx context.Context, userID int64) error {
	if s.refreshTokenCache == nil {
		return nil
	}
	return s.refreshTokenCache.DeleteUserRefreshTokens(ctx, userID)
}

// ResetAccountSecurity requires an active administrator and the existing recent
// native proof policy, including password plus a valid backup recovery factor.
// Notes are validation input only; the existing request audit owns retention.
func (s *AuthService) ResetAccountSecurity(ctx context.Context, operatorID, targetID int64, nativeToken string, confirmed bool, note string) error {
	if err := s.requireAccountCenterEnabled(); err != nil {
		return err
	}
	if operatorID <= 0 || targetID <= 0 || !confirmed || !utf8.ValidString(note) || len(note) > AccountSecurityResetNoteMaxBytes || strings.TrimSpace(note) == "" {
		return ErrAccountSecurityResetInput
	}
	if s.userRepo == nil {
		return ErrServiceUnavailable
	}
	operator, err := s.userRepo.GetByID(ctx, operatorID)
	if err != nil {
		return ErrServiceUnavailable
	}
	if operator == nil || operator.ID != operatorID || operator.Role != RoleAdmin || !operator.IsActive() || operator.DeletedAt != nil {
		return infraerrors.Forbidden("ACCOUNT_SECURITY_RESET_ADMIN_REQUIRED", "active administrator required")
	}
	if _, err := s.RequireRecentAccountAuthentication(ctx, nativeToken, operatorID, operator.TotpEnabled); err != nil {
		return err
	}
	repo, ok := s.recoveryCodes.(AccountSecurityResetRepository)
	if !ok || repo == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return ErrServiceUnavailable
	}
	epoch, err := randomHexString(16)
	if err != nil {
		return ErrServiceUnavailable
	}
	if err := repo.ResetAccountSecurity(ctx, targetID, epoch); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrUserNotFound
		}
		if errors.Is(err, ErrAccountSecurityResetUnknown) {
			return ErrAccountSecurityResetUnknown
		}
		return ErrAccountSecurityResetStorage
	}
	if err := s.cleanRedisRefreshSessions(ctx, targetID); err != nil {
		return ErrAccountSecurityResetUnknown
	}
	return nil
}

// The audit retains the bounded verification method, never the raw request or
// known credential-shaped strings. Operators must not put secrets in the note.
func AccountSecurityResetAuditNote(note string) string {
	note = strings.Join(strings.Fields(note), " ")
	// Mask explicitly labeled secret values before candidate scanning can merge
	// adjacent words. A secret-bearing clause is omitted through its delimiter.
	note = regexp.MustCompile(`(?i)(password|pwd|密码|口令|totp|otp|验证码|恢复码|recovery_code|token|secret|密钥)\s*[:=：]\s*[^,;]+`).ReplaceAllString(note, "$1=[redacted]")
	// Scan whole normalized candidates before label masking/truncation so a
	// spaced/hyphenated code cannot be partially retained as ordinary prose.
	runs := regexp.MustCompile(`(?i)[a-z2-7]+(?:[- ]+[a-z2-7]+)*`)
	words := regexp.MustCompile(`(?i)[a-z2-7]+`)
	note = runs.ReplaceAllStringFunc(note, func(run string) string {
		indexes := words.FindAllStringIndex(run, -1)
		masked := make([]bool, len(run))
		for i := range indexes {
			count := 0
			for j := i; j < len(indexes); j++ {
				count += indexes[j][1] - indexes[j][0]
				if count > 32 {
					break
				}
				if count == 32 {
					candidate := run[indexes[i][0]:indexes[j][1]]
					if _, err := HashAccountRecoveryCode(candidate); err == nil {
						for k := indexes[i][0]; k < indexes[j][1]; k++ {
							masked[k] = true
						}
					}
					break
				}
			}
		}
		var out strings.Builder
		hidden := false
		for i := range len(run) {
			if masked[i] {
				if !hidden {
					out.WriteString("[recovery code redacted]")
				}
				hidden = true
			} else {
				out.WriteByte(run[i])
				hidden = false
			}
		}
		return out.String()
	})

	note = regexp.MustCompile(`[A-Za-z0-9_./+=-]{24,}`).ReplaceAllString(note, "[opaque value redacted]")
	runes := []rune(note)
	if len(runes) > 128 {
		note = string(runes[:128])
	}
	return note
}
