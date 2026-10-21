//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestSquarespacePauseBlocksBeforeDependencies(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	ctx := context.Background()
	s := &PaymentService{}
	_, err := s.CreateOrder(ctx, CreateOrderRequest{PaymentType: "squarespace"})
	require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
	claims := &SquarespaceClaimService{}
	_, err = claims.RequestChallenge(ctx, 1, 1, "1", "synthetic@example.test")
	require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
	_, err = claims.Claim(ctx, 1, "synthetic", "123456")
	require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
	cfg := &PaymentConfigService{}
	require.ErrorIs(t, cfg.ImportSquarespaceOAuth(ctx, "site", "client", "secret", 0, nil), payment.ErrSquarespacePaymentsPaused)
	require.ErrorIs(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site", "client", 0, nil), payment.ErrSquarespacePaymentsPaused)
	require.ErrorIs(t, cfg.SaveSquarespaceOAuthCredentials(ctx, "site", "client", "secret"), payment.ErrSquarespacePaymentsPaused)
	source := &squarespaceDurableTokenSource{}
	_, err = source.AccessToken(ctx)
	require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
	_, _, err = s.FindSquarespaceReceiptOrder(ctx, nil, "1")
	require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
	require.ErrorIs(t, s.BindVerifiedReceiptClaim(ctx, nil, nil, nil), payment.ErrSquarespacePaymentsPaused)
	_, err = s.ReconcileBoundSquarespaceReceipt(ctx, nil)
	require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
	require.ErrorIs(t, s.HandlePaymentNotification(ctx, &payment.PaymentNotification{Status: payment.NotificationStatusSuccess}, "squarespace"), payment.ErrSquarespacePaymentsPaused)
}

func TestSquarespacePausePreservesOrdersAndSyncState(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("pause@example.test").SetPasswordHash("hash").SetUsername("pause").Save(ctx)
	require.NoError(t, err)
	cfg := &PaymentConfigService{entClient: client}
	s := &PaymentService{entClient: client, configService: cfg}
	now := time.Now()
	_, err = client.PaymentSyncState.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").SetTokenVersion(7).SetRotationPhase("unknown").SetEncryptedOauthTokens("synthetic-preserve").SetLastErrorCode("ORIGINAL_ERROR").SetLeaseOwner("original").SetLeaseUntil(now.Add(time.Minute)).Save(ctx)
	require.NoError(t, err)
	var orders []*dbent.PaymentOrder
	for i, status := range []string{OrderStatusPending, OrderStatusPaid, OrderStatusFailed, OrderStatusCompleted, OrderStatusRefundPending} {
		order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(10).SetPayAmount(1).SetRechargeCode(fmt.Sprintf("pause_%d", i)).SetOutTradeNo(fmt.Sprintf("pause_%d", i)).SetClientIP("127.0.0.1").SetSrcHost("synthetic.test").SetPaymentTradeNo("").SetPaymentType("squarespace").SetProviderKey("squarespace").SetOrderType(payment.OrderTypeBalance).SetStatus(status).SetPayURL("https://synthetic.test/checkout").SetQrCode("synthetic-qr").SetExpiresAt(now.Add(-time.Hour)).Save(ctx)
		require.NoError(t, err)
		orders = append(orders, order)
	}
	snapshot := func() string {
		o, e := client.PaymentOrder.Query().All(ctx)
		require.NoError(t, e)
		st, e := client.PaymentSyncState.Query().All(ctx)
		require.NoError(t, e)
		audit, e := client.PaymentAuditLog.Query().Count(ctx)
		require.NoError(t, e)
		ext, e := client.PaymentExternalOrder.Query().Count(ctx)
		require.NoError(t, e)
		claims, e := client.PaymentExternalPayment.Query().Count(ctx)
		require.NoError(t, e)
		journal, e := client.PaymentExternalRefundJournal.Query().Count(ctx)
		require.NoError(t, e)
		raw, e := json.Marshal([]any{o, st, audit, ext, claims, journal})
		require.NoError(t, e)
		return string(raw)
	}
	before := snapshot()
	bridge := NewSquarespacePaymentBridge(s, nil, nil)
	bridge.sourceFactory = func(context.Context, map[string]string) (squarespaceBridgeSource, error) {
		t.Fatal("upstream source created")
		return nil, nil
	}
	bridge.Start()
	require.NoError(t, bridge.RunOnce(ctx))
	bridge.Stop()
	require.ErrorIs(t, bridge.syncWebsite(ctx, nil, nil), payment.ErrSquarespacePaymentsPaused)
	require.ErrorIs(t, bridge.processExternalOrder(ctx, nil, "", nil, nil, nil), payment.ErrSquarespacePaymentsPaused)
	require.ErrorIs(t, bridge.applyObservedRefund(ctx, nil, nil, ""), payment.ErrSquarespacePaymentsPaused)
	for _, o := range orders {
		require.True(t, s.paymentOrderPaused(ctx, o))
		_, e := s.cancelCore(ctx, o, OrderStatusCancelled, "test", "test")
		require.ErrorIs(t, e, payment.ErrSquarespacePaymentsPaused)
		require.Empty(t, s.checkPaid(ctx, o))
		require.ErrorIs(t, s.confirmPayment(ctx, o.ID, "synthetic", 1, "squarespace", nil), payment.ErrSquarespacePaymentsPaused)
		require.ErrorIs(t, s.ExecuteBalanceFulfillment(ctx, o.ID), payment.ErrSquarespacePaymentsPaused)
		require.ErrorIs(t, s.ExecuteSubscriptionFulfillment(ctx, o.ID), payment.ErrSquarespacePaymentsPaused)
		require.ErrorIs(t, s.RetryFulfillment(ctx, o.ID), payment.ErrSquarespacePaymentsPaused)
		_, _, e = s.PrepareRefund(ctx, o.ID, 1, "test", true, true)
		require.ErrorIs(t, e, payment.ErrSquarespacePaymentsPaused)
		_, e = s.ExecuteRefund(ctx, &RefundPlan{OrderID: o.ID, Order: o})
		require.ErrorIs(t, e, payment.ErrSquarespacePaymentsPaused)
		_, e = s.QueryAndFinalizeRefund(ctx, o.ID)
		require.ErrorIs(t, e, payment.ErrSquarespacePaymentsPaused)
	}
	n, err := s.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	status, err := cfg.SquarespaceOAuthStatus(ctx, "site_1")
	require.NoError(t, err)
	require.True(t, status.PaymentsPaused)
	_, err = cfg.SquarespaceOAuthStatus(ctx, "missing_site")
	require.NoError(t, err)
	safe, err := s.GetOrder(ctx, orders[0].ID, user.ID)
	require.NoError(t, err)
	require.Nil(t, safe.PayURL)
	require.Nil(t, safe.QrCode)
	require.NotNil(t, orders[0].PayURL)
	require.Equal(t, before, snapshot(), "pause changed business rows, state, credentials, leases or audit")
	// Start/RunOnce must not issue even a database read.
	require.NoError(t, client.Close())
	require.NoError(t, bridge.RunOnce(ctx))
}

func TestSquarespacePauseHistoricalMarkersAndOtherProviders(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	s := &PaymentService{entClient: client}
	legacy, err := client.PaymentProviderInstance.Create().SetProviderKey("squarespace").SetName("historical").SetConfig("{}").SetSupportedTypes("squarespace").SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	instanceID := fmt.Sprint(legacy.ID)
	require.True(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{ProviderInstanceID: &instanceID}))
	require.True(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{ProviderSnapshot: map[string]any{"provider_key": "squarespace"}}))
	require.True(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{}))
	require.True(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{PaymentType: "unknown_method"}))
	unknownKey := "unknownKey"
	require.True(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{ProviderKey: &unknownKey, PaymentType: payment.TypeGelv}))
	gelv := payment.TypeGelv
	require.False(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{ProviderKey: &gelv, PaymentType: gelv}))
	require.NoError(t, requireSquarespaceProductionAccess(&PaymentConfig{}, 1, &payment.InstanceSelection{ProviderKey: gelv}))
	cfg := &PaymentConfig{Enabled: true, EnabledTypes: []string{"squarespace", gelv}, MerchantTestUserIDs: []int64{1}}
	visible := s.PaymentConfigForUser(ctx, cfg, 1)
	require.Equal(t, []string{gelv}, visible.EnabledTypes)
	methods := s.FilterPaymentMethodsForUser(ctx, cfg, 1, map[string]MethodLimits{"squarespace": {}, gelv: {}})
	require.Contains(t, methods, gelv)
	require.NotContains(t, methods, "squarespace")
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "false")
	require.False(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{ProviderInstanceID: &instanceID}))
	require.NoError(t, requireSquarespaceProductionAccess(&PaymentConfig{}, 1, &payment.InstanceSelection{ProviderKey: "squarespace", Config: map[string]string{"productionApproved": "true"}}))
}

func TestSquarespacePauseLegacyResumeAndUnknownBindings(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("legacy-pause@example.test").SetPasswordHash("hash").SetUsername("legacy-pause").Save(ctx)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey("squarespace").SetName("disabled-legacy").SetConfig("{}").SetSupportedTypes("card").SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	resume := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	s := &PaymentService{entClient: client, resumeService: resume}
	for i, binding := range []string{fmt.Sprint(inst.ID), "99999999", "malformed", fmt.Sprint(inst.ID)} {
		order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(10).SetPayAmount(1).SetRechargeCode(fmt.Sprint("legacy-pause-", i)).SetOutTradeNo(fmt.Sprint("legacy_pause_", i)).SetPaymentTradeNo("").SetPaymentType("card").SetProviderInstanceID(binding).SetStatus(OrderStatusPending).SetOrderType(payment.OrderTypeBalance).SetExpiresAt(time.Now().Add(-time.Minute)).SetClientIP("127.0.0.1").SetSrcHost("synthetic.test").SetPayURL("https://synthetic.test/checkout").SetQrCode("synthetic-qr").Save(ctx)
		require.NoError(t, err)
		if i == 3 {
			order, err = client.PaymentOrder.UpdateOneID(order.ID).SetProviderKey(payment.TypeGelv).SetProviderSnapshot(map[string]any{"provider_key": payment.TypeGelv, "provider_instance_id": binding}).Save(ctx)
			require.NoError(t, err)
		}
		token, err := resume.CreateToken(ResumeTokenClaims{OrderID: order.ID, UserID: user.ID, ProviderInstanceID: binding, PaymentType: "card", CanonicalReturnURL: "https://synthetic.test/result"})
		require.NoError(t, err)
		safe, err := s.GetPublicOrderByResumeToken(ctx, token)
		require.NoError(t, err)
		require.Nil(t, safe.PayURL)
		require.Nil(t, safe.QrCode)
		safe, err = s.GetOrderByID(ctx, order.ID)
		require.NoError(t, err)
		require.Nil(t, safe.PayURL)
		_, err = s.cancelCore(ctx, order, OrderStatusCancelled, "test", "test")
		require.ErrorIs(t, err, payment.ErrSquarespacePaymentsPaused)
		require.ErrorIs(t, s.ExecuteBalanceFulfillment(ctx, order.ID), payment.ErrSquarespacePaymentsPaused)
		stored, err := client.PaymentOrder.Get(ctx, order.ID)
		require.NoError(t, err)
		require.Equal(t, OrderStatusPending, stored.Status)
		require.NotNil(t, stored.PayURL)
	}
	listed, _, err := s.AdminListOrders(ctx, user.ID, OrderListParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, listed, 4)
	for _, order := range listed {
		require.Nil(t, order.PayURL)
	}
	n, err := s.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	// A valid Gelv binding remains available while an inconsistent one is inert.
	gelv, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeGelv).SetName("gelv-active").SetConfig("{}").SetSupportedTypes(payment.TypeGelv).SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	id := fmt.Sprint(gelv.ID)
	key := payment.TypeGelv
	normal := &dbent.PaymentOrder{ProviderInstanceID: &id, ProviderKey: &key, PaymentType: key}
	require.False(t, s.paymentOrderPaused(ctx, normal))
	mismatch := *normal
	mismatch.ProviderSnapshot = map[string]any{"provider_key": key, "provider_instance_id": "different"}
	require.True(t, s.paymentOrderPaused(ctx, &mismatch))
	require.NoError(t, client.Close())
	require.True(t, s.paymentOrderPaused(ctx, normal), "a database error must remain unknown even for an explicit non-Square identity")
	require.True(t, s.paymentOrderPaused(ctx, &dbent.PaymentOrder{ProviderInstanceID: &id}), "database uncertainty must never allow an unidentified bound order to mutate")
}

func TestSquarespacePausePreservesFrozenNonSquareRecovery(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	// This existing financial fixture deliberately has a frozen Gelv snapshot
	// whose current provider instance is absent. Prove exact credit/idempotency
	// still hold under the production pause, without contacting any gateway.
	s, order, users, codes := gelvServiceOrder(t, OrderStatusExpired)
	require.False(t, s.paymentOrderPaused(context.Background(), order))
	metadata := gelvServiceMetadata(order, "wechat_virtual")
	for i := 0; i < 3; i++ {
		require.NoError(t, s.confirmPayment(context.Background(), order.ID, order.PaymentTradeNo, 10, payment.TypeGelv, metadata))
	}
	saved, err := s.entClient.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Equal(t, 1, codes.createCalls)
	require.Len(t, codes.useCalls, 1)
}
