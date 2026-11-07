package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// This capability is deliberately separate from generic ClearError: it never
// clears rate limits, model restrictions, quality state, overload or timers.
type AccountBalanceRecoveryRepository interface {
	GetAccountBalanceRecoverySnapshot(context.Context, int64) (*AccountBalanceRecoverySnapshot, error)
	RecoverAccountBalanceIfObserved(context.Context, int64, AccountBalanceRecoveryRequest) (*AccountBalanceRecoveryResult, error)
}

type AccountBalanceRecoveryAdmin interface {
	PreviewAccountBalanceRecovery(context.Context, int64) (*AccountBalanceRecoveryPreview, error)
	RecoverAccountBalance(context.Context, int64, AccountBalanceRecoveryRequest) (*AccountBalanceRecoveryResult, error)
}

// Snapshot is internal repository data, never returned by an HTTP handler.
type AccountBalanceRecoverySnapshot struct {
	ID                 int64          `json:"id"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Status             string         `json:"status"`
	Schedulable        bool           `json:"schedulable"`
	ErrorMessage       string         `json:"error_message"`
	UpdatedAt          time.Time      `json:"updated_at"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra"`
	ProxyID            *int64         `json:"proxy_id"`
	ParentAccountID    *int64         `json:"parent_account_id"`
	PendingManualTimer bool           `json:"pending_manual_timer"`
}

type AccountBalanceRecoveryEvidence struct {
	State      string     `json:"state"`
	Remaining  *float64   `json:"remaining,omitempty"`
	Unit       string     `json:"unit,omitempty"`
	ReceivedAt *time.Time `json:"received_at,omitempty"`
	FreshUntil *time.Time `json:"fresh_until,omitempty"`
}

type AccountBalanceRecoveryPreview struct {
	AccountID                  int64                          `json:"account_id"`
	Eligible                   bool                           `json:"eligible"`
	Reason                     string                         `json:"reason"`
	Status                     string                         `json:"status"`
	Schedulable                bool                           `json:"schedulable"`
	ExpectedUpdatedAt          time.Time                      `json:"expected_updated_at"`
	ExpectedFingerprint        string                         `json:"expected_fingerprint"`
	BalanceEvidence            AccountBalanceRecoveryEvidence `json:"balance_evidence"`
	PendingManualTimer         bool                           `json:"pending_manual_timer"`
	IndependentLimitsPreserved bool                           `json:"independent_limits_preserved"`
}

type AccountBalanceRecoveryRequest struct {
	ExpectedUpdatedAt   time.Time `json:"expected_updated_at"`
	ExpectedFingerprint string    `json:"expected_fingerprint"`
	TargetSchedulable   *bool     `json:"target_schedulable"`
	FundingConfirmed    bool      `json:"funding_confirmed"`
	Confirm             bool      `json:"confirm"`
}

type AccountBalanceRecoveryResult struct {
	AccountID                  int64     `json:"account_id"`
	Status                     string    `json:"status"`
	Schedulable                bool      `json:"schedulable"`
	UpdatedAt                  time.Time `json:"updated_at"`
	IndependentLimitsPreserved bool      `json:"independent_limits_preserved"`
	FundingEvidence            string    `json:"funding_evidence"`
}

func (s *adminServiceImpl) PreviewAccountBalanceRecovery(ctx context.Context, id int64) (*AccountBalanceRecoveryPreview, error) {
	if principal := adminaccess.FromContext(ctx); principal != nil {
		account, err := s.accountRepo.GetByID(ctx, id)
		if err != nil || account == nil || !principal.Policy.AllowsAccount("accounts.read", scopedAccountResource(account)) {
			return nil, infraerrors.NotFound("SCOPED_ACCOUNT_NOT_FOUND", "account not found")
		}
	}
	repo, ok := s.accountRepo.(AccountBalanceRecoveryRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("BALANCE_RECOVERY_UNAVAILABLE", "balance recovery unavailable")
	}
	snapshot, err := repo.GetAccountBalanceRecoverySnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	return BuildAccountBalanceRecoveryPreview(snapshot, time.Now().UTC())
}
func (s *adminServiceImpl) RecoverAccountBalance(ctx context.Context, id int64, req AccountBalanceRecoveryRequest) (*AccountBalanceRecoveryResult, error) {
	if err := ValidateAccountBalanceRecoveryRequest(req); err != nil {
		return nil, err
	}
	repo, ok := s.accountRepo.(AccountBalanceRecoveryRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("BALANCE_RECOVERY_UNAVAILABLE", "balance recovery unavailable")
	}
	if principal := adminaccess.FromContext(ctx); principal != nil {
		var result *AccountBalanceRecoveryResult
		_, err := s.ScopedAdminWrite(ctx, id, "accounts.recover", nil, func(txctx context.Context, account *Account) (*Account, error) {
			if *req.TargetSchedulable != account.Schedulable && !principal.Policy.AllowsAccount("accounts.status", scopedAccountResource(account)) {
				return nil, infraerrors.Forbidden("SCOPED_ACCOUNT_FORBIDDEN", "scheduling target is not authorized")
			}
			var err error
			result, err = repo.RecoverAccountBalanceIfObserved(txctx, id, req)
			return account, err
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return repo.RecoverAccountBalanceIfObserved(ctx, id, req)
}

func ValidateAccountBalanceRecoveryRequest(req AccountBalanceRecoveryRequest) error {
	hash, err := hex.DecodeString(req.ExpectedFingerprint)
	if !req.Confirm || req.TargetSchedulable == nil || req.ExpectedUpdatedAt.IsZero() || err != nil || len(hash) != sha256.Size {
		return infraerrors.BadRequest("BALANCE_RECOVERY_CONFIRMATION_REQUIRED", "confirm the preview and the exact scheduling target")
	}
	return nil
}

func BuildAccountBalanceRecoveryPreview(s *AccountBalanceRecoverySnapshot, now time.Time) (*AccountBalanceRecoveryPreview, error) {
	p := &AccountBalanceRecoveryPreview{AccountID: s.ID, Status: s.Status, Schedulable: s.Schedulable, ExpectedUpdatedAt: s.UpdatedAt.UTC(), PendingManualTimer: s.PendingManualTimer, IndependentLimitsPreserved: true, BalanceEvidence: AccountBalanceRecoveryEvidence{State: "unknown"}}
	// Include identity, error, version and independent extra state. No credential or
	// upstream message is returned, and a new observation invalidates the preview.
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	p.ExpectedFingerprint = hex.EncodeToString(sum[:])
	a := &Account{ID: s.ID, Platform: s.Platform, Type: s.Type, Credentials: s.Credentials, Extra: s.Extra, ProxyID: s.ProxyID, ParentAccountID: s.ParentAccountID}
	if snap := decodeUpstreamBillingProbeSnapshot(s.Extra); snap != nil && snap.Balance != nil {
		p.BalanceEvidence.ReceivedAt = snap.Balance.ReceivedAt
		p.BalanceEvidence.FreshUntil = snap.Balance.FreshUntil
		b := opsBalanceAccount(a, now)
		switch {
		case b.BalanceStatus == "stale":
			p.BalanceEvidence.State = "stale"
		case b.BalanceStatus == "unsupported":
			p.BalanceEvidence.State = "unsupported"
		case s.Extra[UpstreamBillingProbeEnabledExtraKey] == false:
			// An explicitly disabled probe is not current verification of a top-up.
		case b.Balance != nil:
			p.BalanceEvidence.State = "fresh_positive"
			if *b.Balance <= 0 {
				p.BalanceEvidence.State = "fresh_insufficient"
			}
			p.BalanceEvidence.Remaining = b.Balance
			if len(b.Unit) <= 32 {
				p.BalanceEvidence.Unit = b.Unit
			}
		}
	}
	reason := "eligible"
	switch {
	case s.Platform != PlatformAnthropic || s.Type != AccountTypeAPIKey || s.ParentAccountID != nil || a.IsPoolMode():
		reason = "unsupported_account"
	case s.Status != StatusError:
		reason = "not_balance_error"
	case !isRecoverableAnthropicBalanceError(s.ErrorMessage):
		reason = "not_balance_error"
	case s.PendingManualTimer:
		reason = "pending_manual_timer"
	case p.BalanceEvidence.State == "fresh_insufficient":
		reason = "balance_still_insufficient"
	}
	p.Reason = reason
	p.Eligible = reason == "eligible"
	return p, nil
}

// Only native balance-specific isolation messages qualify. A generic 402 may
// mean another billing/entitlement issue and never authorizes this recovery.
func isRecoverableAnthropicBalanceError(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if !strings.HasPrefix(lower, "payment required (402):") && !strings.HasPrefix(lower, "credit balance exhausted (400):") {
		return false
	}
	// The native default combines balance and billing uncertainty; it is not proof.
	if lower == "payment required (402): insufficient balance or billing issue" {
		return false
	}
	for _, phrase := range []string{"insufficient balance", "insufficient account balance", "insufficient credit", "credit balance", "balance is insufficient", "balance depleted", "balance exhausted", "balance has been exhausted", "余额不足", "余额已用尽", "余额耗尽", "账户欠费", "账号欠费"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func CheckAccountBalanceRecoveryObservation(s *AccountBalanceRecoverySnapshot, req AccountBalanceRecoveryRequest, now time.Time) (*AccountBalanceRecoveryPreview, error) {
	if err := ValidateAccountBalanceRecoveryRequest(req); err != nil {
		return nil, err
	}
	p, err := BuildAccountBalanceRecoveryPreview(s, now)
	if err != nil {
		return nil, err
	}
	if !s.UpdatedAt.Equal(req.ExpectedUpdatedAt) || p.ExpectedFingerprint != req.ExpectedFingerprint {
		return nil, infraerrors.New(http.StatusConflict, "BALANCE_RECOVERY_CHANGED", "account or preview changed; inspect again")
	}
	if !p.Eligible {
		return nil, infraerrors.New(http.StatusConflict, "BALANCE_RECOVERY_BLOCKED", p.Reason)
	}
	if p.BalanceEvidence.State != "fresh_positive" && !req.FundingConfirmed {
		return nil, infraerrors.BadRequest("BALANCE_RECOVERY_FUNDING_CONFIRMATION_REQUIRED", "no current positive balance evidence; explicit top-up confirmation required")
	}
	return p, nil
}
