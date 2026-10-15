package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func (s *PaymentConfigService) validateGelvInstance(ctx context.Context, id int64, key, types string, refund bool) error {
	if key != payment.TypeGelv {
		return nil
	}
	if types != payment.TypeGelv || refund {
		return infraerrors.BadRequest("INVALID_GELV_CAPABILITY", "mini-program supports balance QR payments and channel manual refunds only")
	}
	count, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKeyEQ(payment.TypeGelv), paymentproviderinstance.EnabledEQ(true), paymentproviderinstance.IDNEQ(id)).Count(ctx)
	if err != nil {
		return err
	}
	if count != 0 {
		return infraerrors.Conflict("GELV_MULTIPLE_INSTANCES", "only one mini-program provider may be enabled")
	}
	return nil
}

// quoteGelvPayment keeps the shared mini-program's six fixed cash products.
// Site credit includes the separately configured fee, never GBP conversion.
func (s *PaymentService) quoteGelvPayment(ctx context.Context, req CreateOrderRequest, cfg *PaymentConfig) (*RetailQuoteResponse, error) {
	instances, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKeyEQ(payment.TypeGelv), paymentproviderinstance.EnabledEQ(true)).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(instances) != 1 {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel is not configured")
	}
	config, err := s.configService.decryptConfig(instances[0].Config)
	if err != nil {
		return nil, err
	}
	options, err := provider.GelvCreditOptions(config)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("GELV_CREDITS_NOT_CONFIGURED", "mini-program credit amounts are not configured")
	}
	cash := 0.0
	for _, option := range options {
		if decimal.NewFromFloat(option.Credits).Equal(decimal.NewFromFloat(req.Amount)) {
			cash = option.CashCNY
			break
		}
	}
	if cash == 0 {
		return nil, infraerrors.BadRequest("GELV_DENOMINATION_REQUIRED", "choose one of the configured mini-program recharge tiers")
	}
	if (cfg.MinAmount > 0 && cash < cfg.MinAmount) || (cfg.MaxAmount > 0 && cash > cfg.MaxAmount) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "cash denomination is outside the configured range")
	}
	sel, err := s.selectCreateOrderInstance(ctx, req, cfg, cash)
	if err != nil {
		return nil, err
	}
	if sel.ProviderKey != payment.TypeGelv {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel changed")
	}
	if _, err = provider.NewGelv(sel.InstanceID, sel.Config); err != nil {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel is not configured")
	}
	now := time.Now().UTC().Truncate(time.Second)
	ttl := cfg.BalanceRetailQuoteTTLSeconds
	if ttl < 60 || ttl > 900 {
		ttl = 900
	}
	reference, err := newRetailCheckoutReference()
	if err != nil {
		return nil, err
	}
	minor, _ := payment.AmountToMinorUnit(payment.FormatAmountForCurrency(cash, "CNY"), "CNY")
	android, _ := provider.GelvCredits(minor, "wechat_virtual")
	ios, _ := provider.GelvCredits(minor, "apple_iap")
	quote := &RetailQuote{PricingMode: "gelv_channel_cny", CreditByChannel: map[string]float64{"wechat_virtual": android, "apple_iap": ios}, CreditedAmountUSD: req.Amount, PricingBasisCurrency: "CNY", BaseAmountCNY: req.Amount, PayAmount: cash, Currency: "CNY", FX: map[string]float64{}, FXSource: "fixed_cny", FXAsOf: now, IssuedAt: now, ExpiresAt: now.Add(time.Duration(ttl) * time.Second), CheckoutReference: reference}
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: req.UserID, PaymentType: payment.TypeGelv, ProviderInstanceID: sel.InstanceID, ProviderKey: sel.ProviderKey, ProviderIdentity: buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{}), Quote: quote}
	token, err := s.signRetailQuote(claims)
	if err != nil {
		return nil, err
	}
	return &RetailQuoteResponse{QuoteToken: token, RetailQuote: quote}, nil
}

func validateGelvMetadata(o *dbent.PaymentOrder, m map[string]string) error {
	bad := func() error { return fmt.Errorf("gelv payment scope or amount mismatch") }
	if o == nil || o.ProviderSnapshot == nil || len(m) == 0 {
		return bad()
	}
	if m["partner_id"] == "" || m["partner_id"] != psSnapshotStringValue(o.ProviderSnapshot["merchant_id"]) || m["partner_order_id"] != o.OutTradeNo || m["gelv_order_id"] == "" || (o.PaymentTradeNo != "" && m["gelv_order_id"] != o.PaymentTradeNo) || m["currency"] != "CNY" {
		return bad()
	}
	matched := false
	for _, id := range strings.Split(psSnapshotStringValue(o.ProviderSnapshot["gelv_product_ids"]), ",") {
		if strings.TrimSpace(id) == m["product_id"] {
			matched = true
		}
	}
	minor, err := payment.AmountToMinorUnit(payment.FormatAmountForCurrency(o.PayAmount, "CNY"), "CNY")
	if !matched || err != nil || m["amount_minor"] != strconv.FormatInt(minor, 10) {
		return bad()
	}
	return nil
}

func gelvFinalCredit(o *dbent.PaymentOrder, m map[string]string) (float64, error) {
	if err := validateGelvMetadata(o, m); err != nil {
		return 0, err
	}
	if m["gelv_status"] != "paid" || strings.TrimSpace(m["wx_order_id"]) == "" || m["refunded_minor"] != "0" || m["channel_verified"] != "true" {
		return 0, fmt.Errorf("gelv payment channel is not verified")
	}
	if resolved := psSnapshotStringValue(o.ProviderSnapshot["gelv_payment_channel"]); resolved != "" && resolved != m["payment_channel"] {
		return 0, fmt.Errorf("gelv settled payment channel changed")
	}
	if wxID := psSnapshotStringValue(o.ProviderSnapshot["gelv_wx_order_id"]); wxID != "" && wxID != m["wx_order_id"] {
		return 0, fmt.Errorf("gelv settled platform payment changed")
	}
	q := PaymentOrderRetailQuote(o)
	if q == nil || q.PricingMode != "gelv_channel_cny" || q.CheckoutReference != o.OutTradeNo {
		return 0, fmt.Errorf("gelv payment has no frozen channel pricing")
	}
	credit, ok := q.CreditByChannel[m["payment_channel"]]
	if !ok || !validRetailNumber(credit) {
		return 0, fmt.Errorf("gelv payment channel has no frozen credit amount")
	}
	return credit, nil
}

// ReconcileGelvOrders polls durable local orders, independently of the browser.
// A cancelled/expired local intent can still have an externally paid order.
// Active and historical lanes have separate budgets so refund scans cannot
// starve pending payments. All historical bound orders remain auditable.
func (s *PaymentService) ReconcileGelvOrders(ctx context.Context) (int, error) {
	now := time.Now()
	active := []string{OrderStatusPending, OrderStatusPaid, OrderStatusRecharging, OrderStatusFailed}
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.ProviderKeyEQ(payment.TypeGelv), paymentorder.StatusIn(active...), paymentorder.Or(paymentorder.StatusNEQ(OrderStatusRecharging), paymentorder.UpdatedAtLTE(now.Add(-paymentFulfillmentLeaseDuration)))).Order(dbent.Asc(paymentorder.FieldUpdatedAt)).Limit(15).All(ctx)
	if err != nil {
		return 0, err
	}
	history, err := s.entClient.PaymentOrder.Query().Where(paymentorder.ProviderKeyEQ(payment.TypeGelv), paymentorder.StatusNotIn(active...), paymentorder.PaymentTradeNoNEQ("")).Order(dbent.Asc(paymentorder.FieldUpdatedAt)).Limit(5).All(ctx)
	if err != nil {
		return 0, err
	}
	orders = append(orders, history...)
	recovered := 0
	for _, o := range orders {
		if ctx.Err() != nil {
			return recovered, ctx.Err()
		}
		if s.reconcileGelvOrder(ctx, o) {
			recovered++
		}
	}
	return recovered, nil
}

func (s *PaymentService) reconcileGelvOrder(ctx context.Context, o *dbent.PaymentOrder) bool {
	defer func() {
		// Do not hold a failed order at the front of either scan indefinitely.
		// A live fulfillment lease is never touched by polling bookkeeping.
		touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, _ = s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.StatusNEQ(OrderStatusRecharging)).SetUpdatedAt(time.Now()).Save(touchCtx)
	}()
	prov, err := s.getOrderProvider(ctx, o)
	if err != nil {
		return false
	}
	recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, prov)
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] mini-program order requires reconciliation", "orderID", o.ID, "error", err)
		var upstreamErr *provider.GelvError
		if errors.As(err, &upstreamErr) && upstreamErr.Kind == "paid_channel_unverified" {
			s.recordGelvReview(ctx, o, "GELV_CHANNEL_REVIEW_REQUIRED", map[string]any{"detail": "Paid order needs server-verified paymentChannel before crediting"})
		}
	}
	return recovered
}

func (s *PaymentService) recordGelvReview(ctx context.Context, o *dbent.PaymentOrder, action string, detail map[string]any) {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return
	}
	exists, err := s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionEQ(action), paymentauditlog.DetailEQ(string(encoded))).Exist(ctx)
	if err == nil && !exists {
		s.writeAuditLog(ctx, o.ID, action, payment.TypeGelv, detail)
	}
}

func (s *PaymentService) reconcileGelvOrderWithProvider(ctx context.Context, o *dbent.PaymentOrder, prov payment.Provider) (bool, error) {
	if o.PaymentTradeNo == "" {
		if time.Now().After(o.ExpiresAt) {
			return false, nil
		}
		pr, err := prov.CreatePayment(ctx, payment.CreatePaymentRequest{OrderID: o.OutTradeNo, PaymentType: payment.TypeGelv, Amount: payment.FormatAmountForCurrency(o.PayAmount, "CNY")})
		if err != nil {
			return false, err
		}
		u := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetPaymentTradeNo(pr.TradeNo)
		if pr.QRCode != "" {
			u.SetQrCode(pr.QRCode)
		}
		if _, err = u.Save(ctx); err != nil {
			return false, err
		}
		o.PaymentTradeNo = pr.TradeNo
	}
	resp, err := prov.QueryOrder(ctx, o.PaymentTradeNo)
	if err != nil {
		return false, err
	}
	if resp == nil {
		return false, fmt.Errorf("gelv empty query response")
	}
	if err = validateGelvMetadata(o, resp.Metadata); err != nil {
		return false, err
	}
	if resp.Status == payment.ProviderStatusPaid {
		if o.Status == OrderStatusCompleted {
			return false, nil
		}
		if err = s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, resp.Amount, payment.TypeGelv, resp.Metadata); err != nil {
			return false, err
		}
		return true, nil
	}
	if resp.Status == payment.ProviderStatusRefunded {
		s.recordGelvReview(ctx, o, "GELV_REFUND_REVIEW_REQUIRED", map[string]any{"refunded_minor": resp.Metadata["refunded_minor"], "detail": "Channel refund observed; reconcile site credit and affiliate commission manually"})
	}
	return false, nil
}
