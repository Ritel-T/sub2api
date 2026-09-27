package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/tidwall/gjson"
)

const openAIExcelBPSCooldownReason = "bps_rate_limited"

type openAIExcelBPSNativeFallbackContextKey struct{}
type openAIExcelBPSCooldownServiceContextKey struct{}
type openAIExcelBPSPreviousResponseCanMoveContextKey struct{}

func withExcelBPSPreviousResponseCanMove(ctx context.Context, canMove bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIExcelBPSPreviousResponseCanMoveContextKey{}, canMove)
}

func excelBPSPreviousResponseCanMove(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	canMove, _ := ctx.Value(openAIExcelBPSPreviousResponseCanMoveContextKey{}).(bool)
	return canMove
}

func excelBPSRequiredResponseOwnerCooldownError() error {
	return fmt.Errorf("%w: %s on required response owner", ErrNoAvailableAccounts, openAIExcelBPSCooldownReason)
}

// Keep only the structural routing decision, never request contents or tool arguments.
func withOpenAIExcelBPSRequestRoute(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, openAIExcelBPSNativeFallbackContextKey{}, basispoints.NativeFallbackReason(body))
}

func openAIExcelBPSRequestUsesBPS(ctx context.Context, account *Account, requestedModel string) bool {
	if forward, ok := openAIForwardModelFromContext(ctx); ok && forward.model != "" {
		requestedModel = forward.model
	}
	if !account.IsExcelBPSEnabledForModel(requestedModel) {
		return false
	}
	if ctx != nil && !account.IsExcelBPSOmitUnsupportedToolsEnabled() && !account.excelBPSRequiredForModel(requestedModel) {
		if reason, _ := ctx.Value(openAIExcelBPSNativeFallbackContextKey{}).(string); reason != "" {
			return false
		}
	}
	return true
}

// Legacy eligibility predicates are shared with non-service callers. Carry the
// service through scheduling so they also observe the immediate local deadline.
func (s *OpenAIGatewayService) withExcelBPSCooldownContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if existing, _ := ctx.Value(openAIExcelBPSCooldownServiceContextKey{}).(*OpenAIGatewayService); existing == s {
		return ctx
	}
	return context.WithValue(ctx, openAIExcelBPSCooldownServiceContextKey{}, s)
}

func isOpenAIExcelBPSCooldownBlocked(ctx context.Context, account *Account, requestedModel string) bool {
	if !openAIExcelBPSRequestUsesBPS(ctx, account, requestedModel) {
		return false
	}
	var service *OpenAIGatewayService
	if ctx != nil {
		service, _ = ctx.Value(openAIExcelBPSCooldownServiceContextKey{}).(*OpenAIGatewayService)
	}
	return !service.excelBPSCooldownUntil(account).IsZero()
}

func (s *OpenAIGatewayService) isOpenAIExcelBPSCooldownBlocked(ctx context.Context, account *Account, requestedModel string) bool {
	return openAIExcelBPSRequestUsesBPS(ctx, account, requestedModel) && !s.excelBPSCooldownUntil(account).IsZero()
}

// ExcelBPSCooldownError is returned before dispatch; callers may fail over only
// when their continuation and output-commit rules permit another account.
type ExcelBPSCooldownError struct {
	ResetAt time.Time
}

func (e *ExcelBPSCooldownError) Error() string {
	return "basispoints account is temporarily rate limited"
}

// checkExcelBPSCooldownBeforeDispatch closes the selection/queue race with a
// bounded fresh read. It writes neither a response nor account state.
func (s *OpenAIGatewayService) checkExcelBPSCooldownBeforeDispatch(ctx context.Context, account *Account, body []byte) error {
	return s.checkExcelBPSCooldownBeforeDispatchForGroup(ctx, account, body, 0, false)
}

func (s *OpenAIGatewayService) checkExcelBPSCooldownBeforeDispatchForGroup(ctx context.Context, account *Account, body []byte, groupID int64, enforceGroup bool) error {
	if s == nil || account == nil {
		return fmt.Errorf("BPS cooldown account unavailable")
	}
	ctx = WithOpenAIResponsesRequestCapabilities(ctx, body)
	model := gjson.GetBytes(body, "model").String()
	ctx = WithOpenAIForwardModel(ctx, model, false)
	// Use the same primary, bounded reader and fixture policy as turn admission.
	// BPS deadlines are not part of that account-wide eligibility or route hash.
	latest, err := s.admitOpenAITurnWithGroup(ctx, account, model, groupID, enforceGroup)
	if err != nil {
		return err
	}
	usesBPS := openAIExcelBPSRequestUsesBPS(ctx, latest, model)
	if openAITurnRouteFingerprint(latest) != openAITurnRouteFingerprint(account) ||
		usesBPS != openAIExcelBPSRequestUsesBPS(ctx, account, model) {
		return denyOpenAITurn("account_binding_changed")
	}
	if usesBPS {
		if resetAt := s.excelBPSCooldownUntil(latest); !resetAt.IsZero() {
			return &ExcelBPSCooldownError{ResetAt: resetAt}
		}
	}
	return nil
}

// A cooling required owner must not turn into a load-balancer miss: its stored
// response history cannot be sent to a different account. Preserve the binding.
func (s *OpenAIGatewayService) checkOpenAIExcelBPSResponseOwner(ctx context.Context, groupID *int64, responseID, requestedModel string) error {
	if s == nil || strings.TrimSpace(responseID) == "" {
		return nil
	}
	store := s.getOpenAIWSStateStore()
	if store == nil {
		return nil
	}
	accountID, err := store.GetResponseAccount(ctx, derefGroupID(groupID), strings.TrimSpace(responseID))
	if err != nil || accountID <= 0 {
		return nil
	}
	var account *Account
	if s.accountRepo != nil {
		account, err = s.accountRepo.GetByID(ctx, accountID)
	} else {
		account, err = s.getSchedulableAccount(ctx, accountID)
	}
	if err != nil || account == nil {
		return fmt.Errorf("%w: required response owner unavailable", ErrNoAvailableAccounts)
	}
	if s.isOpenAIExcelBPSCooldownBlocked(ctx, account, requestedModel) {
		return excelBPSRequiredResponseOwnerCooldownError()
	}
	return nil
}
