//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func gelvAlipayServiceOrder(t *testing.T, status, bps string) (*PaymentService, *dbent.PaymentOrder, *mockUserRepo, *paymentFulfillmentRedeemRepo) {
	t.Helper()
	s, o, users, codes := gelvServiceOrder(t, status)
	rate, err := provider.GelvAlipayFeeBPS(map[string]string{"alipayFeeRateBps": bps})
	require.NoError(t, err)
	credit, err := provider.GelvAlipayCredits(1000, rate)
	require.NoError(t, err)
	q := PaymentOrderRetailQuote(o)
	q.CreditByChannel = map[string]float64{gelvAlipayCheckout: credit}
	q.CreditedAmountUSD, q.BaseAmountCNY = credit, credit
	snap := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{InstanceID: "1", ProviderKey: payment.TypeGelv, Config: map[string]string{"partnerId": "partner-test", "productIds": "3,4", "alipayFeeRateBps": bps}}, CreateOrderRequest{PaymentType: payment.TypeGelvAlipay, RetailQuote: q})
	if status != OrderStatusPending {
		snap["gelv_payment_channel"] = gelvAlipayCheckout
		snap["gelv_alipay_trade_no"] = "alipay-service-trade-123"
		snap["gelv_wx_order_id"] = "alipay:alipay-service-trade-123"
		snap["gelv_channel_verification_status"] = "verified"
		snap["gelv_final_credit"] = credit
	}
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetAmount(credit).SetPaymentType(payment.TypeGelvAlipay).SetProviderSnapshot(snap).SetPaidAt(time.Now()).Save(context.Background())
	require.NoError(t, err)
	return s, o, users, codes
}
func gelvAlipayMetadata(o *dbent.PaymentOrder) map[string]string {
	m := gelvServiceMetadata(o, gelvAlipayCheckout)
	m["checkout_channel"] = gelvAlipayCheckout
	m["wx_order_id"] = ""
	m["alipay_trade_no"] = "alipay-service-trade-123"
	m["refund_pending"] = "false"
	delete(m, "fee_rate_bps")
	return m
}

func TestGelvAlipayFrozenAgreementAndUnknownPlatformFee(t *testing.T) {
	for _, bps := range []string{"0", "50", "333"} {
		t.Run(bps, func(t *testing.T) {
			s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusPending, bps)
			m := gelvAlipayMetadata(o)
			// A reported platform rate is independent of the partner agreement.
			m["fee_rate_bps"] = "9876"
			for i := 0; i < 3; i++ {
				require.NoError(t, s.confirmPayment(context.Background(), o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
			}
			saved, err := s.entClient.PaymentOrder.Get(context.Background(), o.ID)
			require.NoError(t, err)
			require.Equal(t, o.Amount, users.getByIDUser.Balance)
			require.Len(t, codes.useCalls, 1)
			require.Equal(t, "alipay:"+m["alipay_trade_no"], saved.ProviderSnapshot["gelv_wx_order_id"])
			require.Equal(t, m["alipay_trade_no"], saved.ProviderSnapshot["gelv_alipay_trade_no"])
			require.NotContains(t, saved.ProviderSnapshot, "gelv_platform_fee_fen")
			require.Equal(t, bps, saved.ProviderSnapshot["gelv_alipay_fee_bps"])
			m["settlement_state"] = "2"
			require.NoError(t, s.syncGelvEvidence(context.Background(), saved, m))
			saved, err = s.entClient.PaymentOrder.Get(context.Background(), o.ID)
			require.NoError(t, err)
			require.Equal(t, "alipay:"+m["alipay_trade_no"], saved.ProviderSnapshot["gelv_wx_order_id"])
			require.NotContains(t, saved.ProviderSnapshot, "gelv_platform_fee_fen")
		})
	}
}

func TestGelvAlipayRejectsWrongScopeAndFrozenCalculation(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"checkout_channel", ""}, {"checkout_channel", "wechat_virtual"}, {"payment_channel", "wechat_virtual"}, {"alipay_trade_no", ""}, {"channel_verified", "false"}, {"channel_verification_status", "conflict"}, {"refunded_minor", "1"}, {"refund_pending", "true"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusPending, "0")
			m := gelvAlipayMetadata(o)
			m[tc.key] = tc.value
			require.Error(t, s.confirmPayment(context.Background(), o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
			require.Zero(t, users.getByIDUser.Balance)
			require.Zero(t, codes.createCalls)
		})
	}
	for _, bps := range []string{"", "10000", "50"} {
		t.Run("frozen_rate_"+bps, func(t *testing.T) {
			_, o, _, _ := gelvAlipayServiceOrder(t, OrderStatusPending, "0")
			o.ProviderSnapshot["gelv_alipay_fee_bps"] = bps
			_, err := gelvFinalCredit(o, gelvAlipayMetadata(o))
			require.Error(t, err)
		})
	}
}

func TestGelvAlipayRefundHoldBlocksAllRetryStatesAndAuthoritativeClear(t *testing.T) {
	for _, status := range []string{OrderStatusPending, OrderStatusPaid, OrderStatusFailed, OrderStatusRecharging, OrderStatusCompleted} {
		t.Run(status, func(t *testing.T) {
			s, o, users, codes := gelvAlipayServiceOrder(t, status, "50")
			ctx := context.Background()
			m := gelvAlipayMetadata(o)
			m["refund_pending"] = "true"
			p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPending, Amount: 10, Metadata: m}}
			recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, p)
			require.NoError(t, err)
			require.False(t, recovered)
			saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
			require.NoError(t, err)
			require.True(t, PaymentOrderRefundReconciliationPending(saved))
			require.Equal(t, "0", saved.ProviderSnapshot["gelv_refunded_minor"])
			require.Equal(t, status, saved.Status)
			require.Zero(t, saved.RefundAmount)
			require.Error(t, s.ExecuteBalanceFulfillment(ctx, o.ID))
			require.Error(t, s.RetryFulfillment(ctx, o.ID))
			require.Zero(t, users.getByIDUser.Balance)
			require.Zero(t, codes.createCalls)
			// Missing refund flag cannot clear the hold.
			delete(m, "refund_pending")
			require.NoError(t, s.clearGelvRefundHold(ctx, saved, m))
			require.True(t, PaymentOrderRefundReconciliationPending(saved))
			m["refund_pending"] = "false"
			m["refunded_minor"] = "100"
			require.NoError(t, s.clearGelvRefundHold(ctx, saved, m))
			require.True(t, PaymentOrderRefundReconciliationPending(saved))
			m["refunded_minor"] = "0"
			require.NoError(t, s.clearGelvRefundHold(ctx, saved, m))
			require.False(t, PaymentOrderRefundReconciliationPending(saved))
			saved, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
			require.NoError(t, err)
			require.Equal(t, status, saved.Status)
			require.Zero(t, users.getByIDUser.Balance)
			if status != OrderStatusCompleted {
				_, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetUpdatedAt(time.Now().Add(-10 * time.Minute)).Save(ctx)
				require.NoError(t, err)
				require.NoError(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
				require.Equal(t, o.Amount, users.getByIDUser.Balance)
				require.Len(t, codes.useCalls, 1)
			}
		})
	}
}

func TestGelvAlipayConfigAvailabilityQuoteAndSameInstance(t *testing.T) {
	for _, bps := range []string{"missing", "", "0", "50"} {
		t.Run(bps, func(t *testing.T) {
			s, _, _, _ := gelvServiceOrder(t, OrderStatusPending)
			key := []byte(strings.Repeat("q", 32))
			ctx := context.Background()
			cfgSvc := NewPaymentConfigService(s.entClient, nil, key)
			config := map[string]string{"partnerId": "partner-test", "partnerSecret": strings.Repeat("fake-test-secret-", 3), "productIds": "3,4,5,6,7,8"}
			if bps != "missing" {
				config["alipayFeeRateBps"] = bps
			}
			_, err := cfgSvc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeGelv, Name: "shared", Config: config, SupportedTypes: []string{payment.TypeGelv, payment.TypeGelvAlipay}, Enabled: true, PaymentMode: "qrcode"})
			require.NoError(t, err)
			s.configService = cfgSvc
			s.loadBalancer = payment.NewDefaultLoadBalancer(s.entClient, key)
			s.resumeService = NewPaymentResumeService(key)
			limits, err := cfgSvc.GetAvailableMethodLimits(ctx)
			require.NoError(t, err)
			require.Contains(t, limits.Methods, payment.TypeGelv)
			available := bps == "0" || bps == "50"
			_, ok := limits.Methods[payment.TypeGelvAlipay]
			require.Equal(t, available, ok)
			direct, err := cfgSvc.GetMethodLimits(ctx, []string{payment.TypeGelvAlipay})
			require.NoError(t, err)
			if !available {
				require.Empty(t, direct)
				_, err = s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: 10}, &PaymentConfig{})
				require.Error(t, err)
				return
			}
			require.Len(t, direct, 1)
			require.Len(t, direct[0].TopupOptions, 6)
			amount := direct[0].TopupOptions[0].Credits
			req := CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: amount}
			cfg := &PaymentConfig{MinAmount: 10, MaxAmount: 500, BalanceRetailPricingEnabled: true, BalanceRetailQuoteTTLSeconds: 900}
			response, err := s.quoteGelvPayment(ctx, req, cfg)
			require.NoError(t, err)
			require.Equal(t, map[string]float64{gelvAlipayCheckout: amount}, response.RetailQuote.CreditByChannel)
			require.Equal(t, 10.0, response.RetailQuote.PayAmount)
			claims, err := s.parseRetailQuote(response.QuoteToken)
			require.NoError(t, err)
			require.Equal(t, payment.TypeGelvAlipay, claims.PaymentType)
			require.Equal(t, payment.TypeGelv, claims.ProviderKey)
			require.Equal(t, bps, claims.ProviderIdentity["gelv_alipay_fee_bps"])
			require.NoError(t, validateRetailQuoteRequest(claims, req))
			req.PaymentType = payment.TypeGelv
			require.Error(t, validateRetailQuoteRequest(claims, req))
			require.Len(t, limits.Methods[payment.TypeGelvAlipay].TopupOptions, 6)
		})
	}
}

type gelvAlipayRecoveryProvider struct {
	paymentOrderLifecycleQueryProvider
	request payment.CreatePaymentRequest
	created *payment.CreatePaymentResponse
}

func (p *gelvAlipayRecoveryProvider) CreatePayment(_ context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	p.request = req
	return p.created, nil
}
func TestGelvAlipayUnknownCreateRecoversSameChannelWithPayURLOnly(t *testing.T) {
	s, o, users, _ := gelvAlipayServiceOrder(t, OrderStatusPending, "0")
	ctx := context.Background()
	m := gelvAlipayMetadata(o)
	externalID := o.PaymentTradeNo
	var err error
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetPaymentTradeNo("").SetExpiresAt(time.Now().Add(time.Minute)).Save(ctx)
	require.NoError(t, err)
	p := &gelvAlipayRecoveryProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: externalID, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: m}}, created: &payment.CreatePaymentResponse{TradeNo: externalID, PayURL: "https://alar1c.cn/partner-pay/#id=" + externalID, Metadata: m}}
	recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, p)
	require.NoError(t, err)
	require.True(t, recovered)
	require.Equal(t, payment.TypeGelvAlipay, p.request.PaymentType)
	require.Equal(t, o.OutTradeNo, p.request.OrderID)
	require.Equal(t, "10.00", p.request.Amount)
	saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, p.created.PayURL, psStringValue(saved.PayURL))
	require.Empty(t, psStringValue(saved.QrCode))
	require.Equal(t, 10.0, users.getByIDUser.Balance)
	count, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestGelvAlipayHeldResponseHidesAllArtifactsWithoutMutatingEntity(t *testing.T) {
	s, o, _, _ := gelvAlipayServiceOrder(t, OrderStatusPaid, "0")
	artifact := "checkout-token"
	o.PayURL, o.QrCode, o.QrCodeImg = &artifact, &artifact, &artifact
	o.ProviderSnapshot["gelv_refund_pending"] = "true"
	safe := s.paymentOrderForResponse(context.Background(), o)
	require.Nil(t, safe.PayURL)
	require.Nil(t, safe.QrCode)
	require.Nil(t, safe.QrCodeImg)
	require.True(t, PaymentOrderRefundReconciliationPending(safe))
	require.NotNil(t, o.PayURL)
}

func TestGelvAlipayReplayUsesFrozenAgreementAndBoundInstance(t *testing.T) {
	s, o, _, _ := gelvAlipayServiceOrder(t, OrderStatusPending, "0")
	ctx := context.Background()
	key := []byte(strings.Repeat("r", 32))
	s.configService = NewPaymentConfigService(s.entClient, nil, key)
	currentConfig := map[string]string{"partnerId": "partner-test", "partnerSecret": strings.Repeat("test-secret-", 4), "productIds": "3,4", "alipayFeeRateBps": "50"}
	encrypted, err := s.configService.encryptConfig(currentConfig)
	require.NoError(t, err)
	inst, err := s.entClient.PaymentProviderInstance.Create().SetProviderKey(payment.TypeGelv).SetName("historical").SetConfig(encrypted).SetSupportedTypes(payment.TypeGelv).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	snapshot := map[string]any{}
	for k, v := range o.ProviderSnapshot {
		snapshot[k] = v
	}
	snapshot["provider_instance_id"] = fmt.Sprint(inst.ID)
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetProviderInstanceID(fmt.Sprint(inst.ID)).SetProviderSnapshot(snapshot).Save(ctx)
	require.NoError(t, err)
	selected, err := s.gelvReplaySelection(ctx, o)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(inst.ID), selected.InstanceID)
	require.Equal(t, "0", selected.Config["alipayFeeRateBps"])
	require.Equal(t, payment.TypeGelv, selected.ProviderKey)
	// Persisted checkout PayURL is sufficient: replay returns without invoking a
	// newly selected/currently enabled instance or demanding any QR payload.
	q := PaymentOrderRetailQuote(o)
	_, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetExpiresAt(time.Now().Add(time.Minute)).SetPayURL("https://alar1c.cn/partner-pay/#id=" + o.PaymentTradeNo).Save(ctx)
	require.NoError(t, err)
	o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	result, err := s.retailReplayResponse(ctx, o, CreateOrderRequest{UserID: o.UserID, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: o.Amount, RetailQuote: q}, &PaymentConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, result.PayURL)
	require.Empty(t, result.QRCode)
}

func TestGelvAlipayRetryValidatesPersistedPricingAndReceipt(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value any
	}{{"gelv_alipay_fee_bps", "50"}, {"gelv_final_credit", 9.95}, {"gelv_wx_order_id", ""}, {"gelv_alipay_trade_no", "changed"}, {"gelv_checkout_channel", "wechat_virtual"}, {"gelv_channel_verification_status", "conflict"}, {"gelv_refunded_minor", "1"}} {
		t.Run(tc.key, func(t *testing.T) {
			s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusPaid, "0")
			snapshot := o.ProviderSnapshot
			snapshot[tc.key] = tc.value
			_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetProviderSnapshot(snapshot).Save(context.Background())
			require.NoError(t, err)
			require.Error(t, s.RetryFulfillment(context.Background(), o.ID))
			require.Zero(t, users.getByIDUser.Balance)
			require.Zero(t, codes.createCalls)
		})
	}
}

func TestGelvConfirmedRefundAndAdverseVerificationBlockFailedRetry(t *testing.T) {
	for _, method := range []string{payment.TypeGelv, payment.TypeGelvAlipay} {
		for _, scenario := range []string{"refund", "verification_conflict"} {
			t.Run(method+"/"+scenario, func(t *testing.T) {
				var s *PaymentService
				var o *dbent.PaymentOrder
				var users *mockUserRepo
				var codes *paymentFulfillmentRedeemRepo
				var m map[string]string
				if method == payment.TypeGelv {
					s, o, users, codes = gelvServiceOrder(t, OrderStatusFailed)
					m = gelvServiceMetadata(o, "wechat_virtual")
					snap := o.ProviderSnapshot
					snap["gelv_payment_channel"], snap["gelv_wx_order_id"], snap["gelv_channel_verification_status"], snap["gelv_fee_rate_bps"], snap["gelv_final_credit"] = "wechat_virtual", m["wx_order_id"], "verified", "100", 9.9
					var err error
					o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetAmount(9.9).SetProviderSnapshot(snap).SetPaidAt(time.Now()).Save(context.Background())
					require.NoError(t, err)
				} else {
					s, o, users, codes = gelvAlipayServiceOrder(t, OrderStatusFailed, "50")
					m = gelvAlipayMetadata(o)
				}
				ctx := context.Background()
				status := payment.ProviderStatusRefunded
				m["refund_pending"] = "false"
				if scenario == "refund" {
					m["gelv_status"] = "refunded"
					m["refunded_minor"] = "1000"
				} else {
					status = payment.ProviderStatusPaid
					m["channel_verification_status"] = "conflict"
					m["channel_verified"] = "false"
				}
				p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: status, Amount: 10, Metadata: m}}
				_, err := s.reconcileGelvOrderWithProvider(ctx, o, p)
				if scenario == "refund" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
				require.NoError(t, err)
				require.Equal(t, OrderStatusFailed, saved.Status)
				require.Zero(t, saved.RefundAmount)
				require.True(t, PaymentOrderRefundReconciliationPending(saved))
				require.NotEmpty(t, saved.ProviderSnapshot["gelv_reconciliation_blocked"])
				require.Error(t, s.RetryFulfillment(ctx, o.ID))
				require.Error(t, s.ExecuteBalanceFulfillment(ctx, o.ID))
				require.Zero(t, users.getByIDUser.Balance)
				require.Zero(t, codes.createCalls)
			})
		}
	}
}

func TestGelvNewIdenticalHoldInvalidatesDelayedAuthoritativeClear(t *testing.T) {
	s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusPaid, "0")
	ctx := context.Background()
	m := gelvAlipayMetadata(o)
	m["refund_pending"] = "true"
	require.NoError(t, s.holdGelvRefund(ctx, o, m))
	// Snapshot is captured before the delayed clear request starts its query.
	delayedObservation := gelvQueryObservation(o)
	firstGeneration := delayedObservation.ProviderSnapshot["gelv_observation_generation"]
	newer, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.NoError(t, s.holdGelvRefund(ctx, newer, m))
	require.NotEqual(t, firstGeneration, newer.ProviderSnapshot["gelv_observation_generation"])
	m["refund_pending"] = "false"
	require.Error(t, s.clearGelvRefundHold(ctx, delayedObservation, m))
	saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.True(t, PaymentOrderRefundReconciliationPending(saved))
	require.Equal(t, newer.ProviderSnapshot["gelv_observation_generation"], saved.ProviderSnapshot["gelv_observation_generation"])
	require.Error(t, s.RetryFulfillment(ctx, o.ID))
	require.Zero(t, users.getByIDUser.Balance)
	require.Zero(t, codes.createCalls)
	// Only a query started after the latest hold may clear that pending flag.
	fresh := gelvQueryObservation(saved)
	require.NoError(t, s.clearGelvRefundHold(ctx, fresh, m))
	require.False(t, PaymentOrderRefundReconciliationPending(fresh))
}

func TestGelvCompletedRefundEvidenceDoesNotReverseCredit(t *testing.T) {
	s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusCompleted, "50")
	users.getByIDUser.Balance = o.Amount
	m := gelvAlipayMetadata(o)
	m["gelv_status"] = "refunded"
	m["refunded_minor"] = "1000"
	p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusRefunded, Amount: 10, Metadata: m}}
	_, err := s.reconcileGelvOrderWithProvider(context.Background(), o, p)
	require.NoError(t, err)
	saved, err := s.entClient.PaymentOrder.Get(context.Background(), o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	require.Zero(t, saved.RefundAmount)
	require.Equal(t, o.Amount, users.getByIDUser.Balance)
	require.Zero(t, codes.createCalls)
	require.True(t, PaymentOrderRefundReconciliationPending(saved))
}

func TestGelvConfirmedRefundMonotonicAcrossStaleZeroAndPartialObservations(t *testing.T) {
	s, o, users, _ := gelvAlipayServiceOrder(t, OrderStatusFailed, "50")
	ctx := context.Background()
	m := gelvAlipayMetadata(o)
	m["refunded_minor"] = "500"
	require.NoError(t, s.persistGelvReconciliationBlock(ctx, o, "confirmed_refund", m))
	for _, value := range []string{"0", "100"} {
		m["refunded_minor"], m["refund_pending"] = value, "true"
		require.NoError(t, s.holdGelvRefund(ctx, o, m))
		saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
		require.NoError(t, err)
		require.Equal(t, "500", saved.ProviderSnapshot["gelv_refunded_minor"])
		require.Equal(t, "confirmed_refund", saved.ProviderSnapshot["gelv_reconciliation_blocked"])
		// Historical settlement-copy paths must preserve the same maximum.
		copyGelvEvidence(saved.ProviderSnapshot, m)
		require.Equal(t, "500", saved.ProviderSnapshot["gelv_refunded_minor"])
		clear := gelvAlipayMetadata(o)
		require.NoError(t, s.clearGelvRefundHold(ctx, saved, clear))
		require.True(t, PaymentOrderRefundReconciliationPending(saved))
		require.Error(t, s.RetryFulfillment(ctx, o.ID))
		require.Zero(t, users.getByIDUser.Balance)
	}
}

func TestGelvPaidQueryEvidenceErrorsBlockButTransportDoesNot(t *testing.T) {
	for _, kind := range []string{"incomplete_paid_evidence", "invalid_order_scope_or_amount", "inconsistent_refund_state", "invalid_response", "paid_channel_unverified", "transport_result_unknown"} {
		t.Run(kind, func(t *testing.T) {
			s, o, _, _ := gelvAlipayServiceOrder(t, OrderStatusFailed, "0")
			require.NoError(t, s.holdGelvQueryError(context.Background(), o, &provider.GelvError{Kind: kind}))
			saved, err := s.entClient.PaymentOrder.Get(context.Background(), o.ID)
			require.NoError(t, err)
			require.Equal(t, kind != "transport_result_unknown", PaymentOrderRefundReconciliationPending(saved))
			require.Zero(t, saved.RefundAmount)
			require.Equal(t, OrderStatusFailed, saved.Status)
		})
	}
}

func TestGelvRepeatedTerminalRefundObservationDoesNotWrite(t *testing.T) {
	for _, method := range []string{payment.TypeGelv, payment.TypeGelvAlipay} {
		for _, status := range []string{OrderStatusCompleted, OrderStatusRefunded} {
			t.Run(method+"_"+status, func(t *testing.T) {
				var s *PaymentService
				var o *dbent.PaymentOrder
				var users *mockUserRepo
				var codes *paymentFulfillmentRedeemRepo
				if method == payment.TypeGelvAlipay {
					s, o, users, codes = gelvAlipayServiceOrder(t, status, "60")
				} else {
					s, o, users, codes = gelvServiceOrder(t, status)
				}
				ctx := context.Background()
				users.getByIDUser.Balance = o.Amount
				m := gelvServiceMetadata(o, "wechat_virtual")
				if method == payment.TypeGelvAlipay {
					m = gelvAlipayMetadata(o)
				}
				m["gelv_status"], m["refunded_minor"], m["refund_pending"] = "refunded", "1000", "false"
				p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusRefunded, Amount: 10, Metadata: m}}
				recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, p)
				require.NoError(t, err)
				require.False(t, recovered)
				saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
				require.NoError(t, err)
				expected, err := json.Marshal(saved.ProviderSnapshot)
				require.NoError(t, err)
				updatedAt := saved.UpdatedAt
				generation := saved.ProviderSnapshot["gelv_observation_generation"]
				auditCount, err := s.entClient.PaymentAuditLog.Query().Count(ctx)
				require.NoError(t, err)
				require.Positive(t, auditCount)
				mutations := 0
				hook := func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
						mutations++
						return next.Mutate(ctx, m)
					})
				}
				s.entClient.PaymentOrder.Use(hook)
				s.entClient.PaymentAuditLog.Use(hook)
				for i := 0; i < 3; i++ {
					// The first retry deliberately keeps the caller's older
					// snapshot; the locked current row must decide idempotency.
					if i > 0 {
						o = saved
					}
					recovered, err = s.reconcileGelvOrderWithProvider(ctx, o, p)
					require.NoError(t, err)
					require.False(t, recovered)
					saved, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
					require.NoError(t, err)
					actual, err := json.Marshal(saved.ProviderSnapshot)
					require.NoError(t, err)
					require.Equal(t, string(expected), string(actual))
					require.Equal(t, updatedAt, saved.UpdatedAt)
					require.Equal(t, generation, saved.ProviderSnapshot["gelv_observation_generation"])
					require.Equal(t, "1000", saved.ProviderSnapshot["gelv_refunded_minor"])
					require.Equal(t, "confirmed_refund", saved.ProviderSnapshot["gelv_reconciliation_blocked"])
					count, err := s.entClient.PaymentAuditLog.Query().Count(ctx)
					require.NoError(t, err)
					require.Equal(t, auditCount, count)
					require.Zero(t, mutations, "duplicate query must not attempt payment-row or audit writes")
					require.Equal(t, status, saved.Status)
					require.Equal(t, o.Amount, users.getByIDUser.Balance)
					require.Zero(t, codes.createCalls)
					require.Zero(t, saved.RefundAmount)
				}
				require.True(t, PaymentOrderRefundReconciliationPending(saved))
				require.Error(t, s.RetryFulfillment(ctx, saved.ID))
				require.Error(t, s.ExecuteBalanceFulfillment(ctx, saved.ID))
			})
		}
	}
}

func TestGelvPermanentBlockDuplicateAndNewAdverseEvidence(t *testing.T) {
	for _, reason := range []string{"confirmed_refund", "paid_channel_conflict", "empty_query_response"} {
		t.Run(reason, func(t *testing.T) {
			s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusFailed, "60")
			ctx := context.Background()
			m := gelvAlipayMetadata(o)
			m["refunded_minor"] = "0"
			if reason == "confirmed_refund" {
				m["refunded_minor"] = "500"
			}
			if reason == "paid_channel_conflict" {
				m["channel_verification_status"] = "conflict"
			}
			require.NoError(t, s.persistGelvReconciliationBlock(ctx, o, reason, m))
			original, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
			require.NoError(t, err)
			expected, err := json.Marshal(original.ProviderSnapshot)
			require.NoError(t, err)
			audits, err := s.entClient.PaymentAuditLog.Query().Count(ctx)
			require.NoError(t, err)
			for _, value := range []string{m["refunded_minor"], "0", ""} {
				m["refunded_minor"] = value
				require.NoError(t, s.persistGelvReconciliationBlock(ctx, o, reason, m))
				saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
				require.NoError(t, err)
				actual, err := json.Marshal(saved.ProviderSnapshot)
				require.NoError(t, err)
				require.Equal(t, string(expected), string(actual))
				require.Equal(t, original.UpdatedAt, saved.UpdatedAt)
				count, err := s.entClient.PaymentAuditLog.Query().Count(ctx)
				require.NoError(t, err)
				require.Equal(t, audits, count)
			}
			generation := psSnapshotIntValue(o.ProviderSnapshot["gelv_observation_generation"])
			m["refunded_minor"] = "600"
			require.NoError(t, s.persistGelvReconciliationBlock(ctx, o, reason, m))
			require.Equal(t, "600", o.ProviderSnapshot["gelv_refunded_minor"])
			require.Equal(t, "confirmed_refund", o.ProviderSnapshot["gelv_reconciliation_blocked"])
			require.Equal(t, generation+1, psSnapshotIntValue(o.ProviderSnapshot["gelv_observation_generation"]))
			// New verification evidence is retained even under an irreversible
			// refund block; an identical repeat then becomes a no-op.
			nextVerification := "conflict"
			if m["channel_verification_status"] == nextVerification {
				nextVerification = "verified"
			}
			m["channel_verification_status"] = nextVerification
			require.NoError(t, s.persistGelvReconciliationBlock(ctx, o, "confirmed_refund", m))
			require.Equal(t, generation+2, psSnapshotIntValue(o.ProviderSnapshot["gelv_observation_generation"]))
			require.Equal(t, nextVerification, o.ProviderSnapshot["gelv_observed_verification_status"])
			require.NoError(t, s.persistGelvReconciliationBlock(ctx, o, "confirmed_refund", m))
			require.Equal(t, generation+2, psSnapshotIntValue(o.ProviderSnapshot["gelv_observation_generation"]))
			// Every identical pending hold still invalidates an older clear;
			// stale zero cannot replace the confirmed positive refund.
			m["refund_pending"], m["refunded_minor"] = "true", "0"
			for i := 1; i <= 2; i++ {
				require.NoError(t, s.holdGelvRefund(ctx, o, m))
				require.Equal(t, generation+2+i, psSnapshotIntValue(o.ProviderSnapshot["gelv_observation_generation"]))
				require.Equal(t, "600", o.ProviderSnapshot["gelv_refunded_minor"])
				require.Equal(t, "confirmed_refund", o.ProviderSnapshot["gelv_reconciliation_blocked"])
			}
			require.NoError(t, s.clearGelvRefundHold(ctx, o, gelvAlipayMetadata(o)))
			require.True(t, PaymentOrderRefundReconciliationPending(o))
			require.Error(t, s.RetryFulfillment(ctx, o.ID))
			require.Error(t, s.ExecuteBalanceFulfillment(ctx, o.ID))
			require.Zero(t, users.getByIDUser.Balance)
			require.Zero(t, codes.createCalls)
		})
	}
}
