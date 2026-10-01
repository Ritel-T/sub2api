package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const internalBalanceProvider = "internal_balance"

var balanceSubscriptionNoncePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{24,64}$`)

type BalanceSubscriptionRequest struct {
	ExpectedUserID   int64   `json:"expected_user_id"`
	PlanID           int64   `json:"plan_id"`
	PurchaseNonce    string  `json:"purchase_nonce"`
	ExpectedPrice    float64 `json:"expected_price"`
	ExpectedCurrency string  `json:"expected_currency"`
	ExpectedGroupID  int64   `json:"expected_group_id"`
	ExpectedDays     int     `json:"expected_validity_days"`
	ExpectedUnit     string  `json:"expected_validity_unit"`
}

type BalanceSubscriptionResult struct {
	OrderID          int64   `json:"order_id"`
	Status           string  `json:"status"`
	SiteCreditAmount float64 `json:"site_credit_amount"`
	Renewed          bool    `json:"renewed"`
}

// BuySubscriptionWithBalance spends prepaid service units, not new cash. It
// deliberately does not call a gateway, create a redeem code or accrue a second
// affiliate rebate for the same funds. RynexAI's CNY1 = $1 credit policy applies
// only to explicitly CNY-priced plans under the existing retail configuration.
func (s *PaymentService) BuySubscriptionWithBalance(ctx context.Context, userID int64, req BalanceSubscriptionRequest) (*BalanceSubscriptionResult, error) {
	if userID <= 0 || req.ExpectedUserID != userID || req.PlanID <= 0 || req.ExpectedGroupID <= 0 || req.ExpectedDays <= 0 || req.ExpectedDays > MaxValidityDays ||
		!balanceSubscriptionValidityUnit(req.ExpectedUnit) || !balanceSubscriptionNoncePattern.MatchString(req.PurchaseNonce) ||
		math.IsNaN(req.ExpectedPrice) || math.IsInf(req.ExpectedPrice, 0) || req.ExpectedPrice <= 0 ||
		req.ExpectedPrice > 1e9 || req.ExpectedCurrency != "CNY" ||
		!decimal.NewFromFloat(req.ExpectedPrice).Equal(decimal.NewFromFloat(req.ExpectedPrice).Round(2)) {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "invalid subscription purchase")
	}
	if s.entClient == nil || s.configService == nil || s.subscriptionSvc == nil {
		return nil, infraerrors.ServiceUnavailable("SUBSCRIPTION_PURCHASE_UNAVAILABLE", "subscription purchase unavailable")
	}
	digest := sha256.Sum256([]byte("rynexai-balance-subscription-v1\x00" + req.PurchaseNonce))
	reference := "bal_sub_" + hex.EncodeToString(digest[:])[:48]
	// Settings repositories can use their own pool connection. Read them before
	// holding a transaction connection; enforce them only for a new purchase,
	// so an already committed nonce can still be recovered after sales close.
	cfg, cfgErr := s.configService.GetPaymentConfig(ctx)
	flags, flagsErr := s.configService.settingRepo.GetMultiple(ctx, []string{SettingKeySubscriptionEnabled})
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	// Serialize purchases against billing and other balance updates. Look up the
	// nonce again after acquiring the user row lock, so concurrent double clicks
	// observe the completed first transaction without another debit or renewal.
	u, err := client.User.Query().Where(user.IDEQ(userID)).ForUpdate().Only(txCtx)
	if err != nil || u.Status != payment.EntityStatusActive {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user account unavailable")
	}
	existing, err := client.PaymentOrder.Query().Where(paymentorder.OutTradeNoEQ(reference)).Only(txCtx)
	if err == nil {
		if existing.UserID != userID || existing.PlanID == nil || *existing.PlanID != req.PlanID ||
			psStringValue(existing.ProviderKey) != internalBalanceProvider || existing.Status != OrderStatusCompleted ||
			existing.OrderType != payment.OrderTypeSubscription || existing.PaymentType != "balance" ||
			existing.Amount != req.ExpectedPrice || existing.SubscriptionGroupID == nil || *existing.SubscriptionGroupID != req.ExpectedGroupID ||
			existing.SubscriptionDays == nil || *existing.SubscriptionDays != psComputeValidityDays(req.ExpectedDays, req.ExpectedUnit) ||
			existing.ProviderSnapshot["plan_currency"] != req.ExpectedCurrency {
			return nil, infraerrors.Conflict("PURCHASE_NONCE_CONFLICT", "purchase reference belongs to another request")
		}
		result := balanceSubscriptionResult(existing)
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, s.invalidateBalanceSubscriptionCaches(userID, *existing.SubscriptionGroupID)
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}
	if cfgErr != nil {
		return nil, cfgErr
	}
	if flagsErr != nil {
		return nil, flagsErr
	}
	if !cfg.Enabled || !cfg.BalanceRetailPricingEnabled || isFalseSettingValue(flags[SettingKeySubscriptionEnabled]) {
		return nil, infraerrors.Forbidden("PAYMENT_DISABLED", "subscription purchase disabled")
	}
	plan, err := client.SubscriptionPlan.Query().Where(subscriptionplan.IDEQ(req.PlanID)).ForShare().Only(txCtx)
	if err != nil || !plan.ForSale {
		return nil, infraerrors.NotFound("PLAN_NOT_AVAILABLE", "plan not available")
	}
	if plan.Currency != "CNY" || plan.Price != req.ExpectedPrice || plan.GroupID != req.ExpectedGroupID ||
		plan.ValidityDays != req.ExpectedDays || plan.ValidityUnit != req.ExpectedUnit {
		return nil, infraerrors.Conflict("PLAN_PRICE_CHANGED", "plan price changed; confirm again")
	}
	days := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	if days <= 0 || days > MaxValidityDays {
		return nil, infraerrors.BadRequest("PLAN_VALIDITY_INVALID", "invalid plan term")
	}
	g, err := client.Group.Query().Where(group.IDEQ(plan.GroupID)).ForShare().Only(txCtx)
	if err != nil || g.Status != payment.EntityStatusActive || g.SubscriptionType != "subscription" {
		return nil, infraerrors.NotFound("GROUP_NOT_FOUND", "subscription group unavailable")
	}
	now := time.Now()
	termBase := now
	currentSub, lookupErr := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(plan.GroupID)).ForUpdate().Only(txCtx)
	if lookupErr != nil && !dbent.IsNotFound(lookupErr) {
		return nil, lookupErr
	}
	if currentSub != nil && currentSub.ExpiresAt.After(termBase) {
		termBase = currentSub.ExpiresAt
	}
	if termBase.AddDate(0, 0, days).After(MaxExpiresAt) {
		return nil, infraerrors.BadRequest("PLAN_VALIDITY_INVALID", "subscription cannot be extended by the full plan term")
	}
	changed, err := client.User.Update().Where(user.IDEQ(userID), user.BalanceGTE(plan.Price)).AddBalance(-plan.Price).Save(txCtx)
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, infraerrors.BadRequest("INSUFFICIENT_BALANCE", "insufficient site credit")
	}
	snapshot := map[string]any{"schema_version": 1, "provider_key": internalBalanceProvider,
		"currency": "USD", "purpose": "subscription_purchase_with_site_credit", "plan_currency": "CNY",
		"plan_price": plan.Price, "site_credit_units": plan.Price, "plan_name": plan.Name}
	o, err := client.PaymentOrder.Create().SetUserID(userID).SetUserEmail(u.Email).SetUserName(u.Username).
		SetAmount(plan.Price).SetPayAmount(0).SetFeeRate(0).SetRechargeCode("").
		SetOutTradeNo(reference).SetPaymentType("balance").SetPaymentTradeNo(reference).
		SetProviderKey(internalBalanceProvider).SetProviderSnapshot(snapshot).
		SetOrderType(payment.OrderTypeSubscription).SetPlanID(plan.ID).
		SetSubscriptionGroupID(plan.GroupID).SetSubscriptionDays(days).
		SetStatus(OrderStatusCompleted).SetExpiresAt(now).SetPaidAt(now).SetCompletedAt(now).
		SetClientIP("").SetSrcHost("").Save(txCtx)
	if err != nil {
		return nil, err
	}
	_, renewed, err := s.subscriptionSvc.assignOrExtendSubscriptionTerm(txCtx, &AssignSubscriptionInput{
		UserID: userID, GroupID: plan.GroupID, ValidityDays: days, Notes: paymentSubscriptionOrderNote(o.ID),
	}, true)
	if err != nil {
		return nil, err
	}
	snapshot["renewed"] = renewed
	if _, err := client.PaymentOrder.UpdateOneID(o.ID).SetProviderSnapshot(snapshot).Save(txCtx); err != nil {
		return nil, err
	}
	for _, action := range []string{"BALANCE_SUBSCRIPTION_DEBIT", "SUBSCRIPTION_ASSIGNED", "SUBSCRIPTION_SUCCESS"} {
		if _, err := client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(o.ID, 10)).
			SetAction(action).SetDetail(fmt.Sprintf("siteCredits=%.2f groupID=%d days=%d", plan.Price, plan.GroupID, days)).
			SetOperator("system").Save(txCtx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &BalanceSubscriptionResult{OrderID: o.ID, Status: OrderStatusCompleted, SiteCreditAmount: plan.Price, Renewed: renewed}, s.invalidateBalanceSubscriptionCaches(userID, plan.GroupID)
}

func balanceSubscriptionResult(o *dbent.PaymentOrder) *BalanceSubscriptionResult {
	renewed, _ := o.ProviderSnapshot["renewed"].(bool)
	return &BalanceSubscriptionResult{OrderID: o.ID, Status: o.Status, SiteCreditAmount: o.Amount, Renewed: renewed}
}

func balanceSubscriptionValidityUnit(unit string) bool {
	switch unit {
	case "day", "days", "week", "weeks", "month", "months":
		return true
	default:
		return false
	}
}

func (s *PaymentService) invalidateBalanceSubscriptionCaches(userID, groupID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var balanceErr error
	if s.redeemService != nil {
		if s.redeemService.authCacheInvalidator != nil {
			s.redeemService.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
		if s.redeemService.billingCacheService != nil {
			balanceErr = s.redeemService.billingCacheService.InvalidateUserBalance(ctx, userID)
		}
	}
	return errors.Join(balanceErr, s.subscriptionSvc.invalidateSubscriptionCaches(userID, groupID))
}
