package service

import (
	"context"
	"strconv"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func merchantTestUser(cfg *PaymentConfig, userID int64) bool {
	if cfg == nil || userID <= 0 {
		return false
	}
	for _, id := range cfg.MerchantTestUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}
func paymentRequestEnabled(cfg *PaymentConfig, req CreateOrderRequest) bool {
	return cfg != nil && (cfg.Enabled || merchantTestUser(cfg, req.UserID) && NormalizeVisibleMethod(req.PaymentType) == "squarespace" && (req.OrderType == "" || req.OrderType == payment.OrderTypeBalance))
}
func requireSquarespaceProductionAccess(cfg *PaymentConfig, userID int64, selection *payment.InstanceSelection) error {
	if selection != nil && selection.ProviderKey == "squarespace" && selection.Config["productionApproved"] != "true" && !merchantTestUser(cfg, userID) {
		return infraerrors.Forbidden("SQUARESPACE_PRODUCTION_NOT_APPROVED", "Squarespace payments are awaiting platform approval")
	}
	return nil
}

// PaymentConfigForUser returns effective access while hiding the merchant test
// allowlist from every authenticated public API response.
func (s *PaymentService) PaymentConfigForUser(ctx context.Context, cfg *PaymentConfig, userID int64) *PaymentConfig {
	out := *cfg
	out.MerchantTestUserIDs = nil
	out.MerchantTestAccess = merchantTestUser(cfg, userID)
	out.Enabled = cfg.Enabled || out.MerchantTestAccess
	// Signed retail quotes define CNY principal and site-credit units one-to-one.
	// Legacy gateway promotions do not apply to that frozen financial contract.
	if cfg.BalanceRetailPricingEnabled {
		out.RechargeBonusTiers = []RechargeBonusTier{}
		out.RechargeBonusNotice = ""
	}
	types := make([]string, 0, len(cfg.EnabledTypes))
	for _, method := range cfg.EnabledTypes {
		if !cfg.Enabled && (!out.MerchantTestAccess || method != "squarespace") {
			continue
		}
		if method == "squarespace" && !out.MerchantTestAccess && !s.squarespaceApprovedAvailable(ctx) {
			continue
		}
		types = append(types, method)
	}
	out.EnabledTypes = types
	return &out
}
func (s *PaymentService) FilterPaymentMethodsForUser(ctx context.Context, cfg *PaymentConfig, userID int64, methods map[string]MethodLimits) map[string]MethodLimits {
	out := map[string]MethodLimits{}
	test := merchantTestUser(cfg, userID)
	for method, limits := range methods {
		if !cfg.Enabled && (!test || method != "squarespace") {
			continue
		}
		if method == "squarespace" && !test && !s.squarespaceApprovedAvailable(ctx) {
			continue
		}
		out[method] = limits
	}
	return out
}
func (s *PaymentService) squarespaceApprovedAvailable(ctx context.Context) bool {
	if s.configService == nil || s.configService.entClient == nil {
		return false
	}
	instances, err := s.configService.ListProviderInstances(ctx)
	if err != nil {
		return false
	}
	for _, inst := range instances {
		if !inst.Enabled || inst.ProviderKey != "squarespace" {
			continue
		}
		cfg, err := s.configService.decryptConfig(inst.Config)
		if err == nil && cfg["productionApproved"] == "true" {
			return true
		}
	}
	return false
}
func (s *PaymentService) validateSquarespaceOrderAccess(ctx context.Context, userID int64, order *dbent.PaymentOrder) error {
	cfg, err := s.configService.GetPaymentConfig(ctx)
	if err != nil {
		return infraerrors.ServiceUnavailable("SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE", "payment access is unavailable")
	}
	req := CreateOrderRequest{UserID: userID, PaymentType: "squarespace", OrderType: payment.OrderTypeBalance}
	if !paymentRequestEnabled(cfg, req) {
		return infraerrors.Forbidden("PAYMENT_DISABLED", "payment system is disabled")
	}
	if order == nil || order.ProviderInstanceID == nil {
		return infraerrors.Forbidden("SQUARESPACE_CLAIM_UNAVAILABLE", "receipt claim is unavailable")
	}
	id, err := strconv.ParseInt(*order.ProviderInstanceID, 10, 64)
	if err != nil {
		return infraerrors.Forbidden("SQUARESPACE_CLAIM_UNAVAILABLE", "receipt claim is unavailable")
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
	if err != nil || inst.ProviderKey != "squarespace" {
		return infraerrors.Forbidden("SQUARESPACE_CLAIM_UNAVAILABLE", "receipt claim is unavailable")
	}
	public, err := s.configService.decryptConfig(inst.Config)
	if err != nil {
		return infraerrors.ServiceUnavailable("SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE", "payment access is unavailable")
	}
	return requireSquarespaceProductionAccess(cfg, userID, &payment.InstanceSelection{ProviderKey: inst.ProviderKey, Config: public})
}
