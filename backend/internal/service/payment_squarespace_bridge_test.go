//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalrefundjournal"
	"github.com/Wei-Shaw/sub2api/ent/paymentsyncstate"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func squarespaceBridgeProofFixture() (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument) {
	no := false
	money := func(value string) provider.SquarespaceMoney {
		return provider.SquarespaceMoney{Currency: "GBP", Value: value}
	}
	order := &provider.SquarespaceOrder{ID: "external_order_1", OrderNumber: "1", PaymentState: "PAID", TestMode: &no, GrandTotal: money("10.00"), RefundedTotal: money("0.00"), TopUpReference: "sub2_0123456789abcdef0123456789abcdef", ReferenceStatus: "valid", LineItems: []provider.SquarespaceLineItem{{ID: "line_1", ProductID: "6abc2d02ac3ba7447cdc0752"}}}
	docs := []provider.SquarespaceTransactionDocument{{ID: "doc_1", SalesOrderID: order.ID, Voided: &no, Total: money("10.00"), TotalNetPayment: money("9.55"), Payments: []provider.SquarespacePayment{{ID: "payment_1", ExternalTransactionID: "charge_1", Provider: "SQSP_PAYMENTS", PaidOn: time.Now().UTC().Format(time.RFC3339Nano), Amount: money("10.00"), NetAmount: money("9.55"), RefundedAmount: money("0.00"), ProcessingFees: []provider.SquarespaceProcessingFee{{Amount: money("0.45"), NetAmount: money("0.45"), RefundedAmount: money("0.00")}}}}}}
	return order, docs
}

func TestSquarespaceBridgeRejectsUntrustedPaymentProof(t *testing.T) {
	cases := map[string]func(*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument){
		"test mode": func(o *provider.SquarespaceOrder, _ []provider.SquarespaceTransactionDocument) {
			yes := true
			o.TestMode = &yes
		},
		"unknown test flag": func(o *provider.SquarespaceOrder, _ []provider.SquarespaceTransactionDocument) { o.TestMode = nil },
		"pending": func(o *provider.SquarespaceOrder, _ []provider.SquarespaceTransactionDocument) {
			o.PaymentState = "PENDING"
		},
		"currency": func(o *provider.SquarespaceOrder, _ []provider.SquarespaceTransactionDocument) {
			o.GrandTotal.Currency = "USD"
		},
		"fractional penny": func(o *provider.SquarespaceOrder, _ []provider.SquarespaceTransactionDocument) {
			o.GrandTotal.Value = "10.001"
		},
		"amount mismatch": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].Payments[0].Amount.Value = "9.99"
		},
		"voided": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			yes := true
			d[0].Voided = &yes
		},
		"obsolete guessed gateway": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].Payments[0].Provider = "SQUARESPACE"
		},
		"gateway error": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].PaymentGatewayError = "GATEWAY_FEE_PROCEESING_ERROR"
		},
		"extra payment": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].Payments = append(d[0].Payments, d[0].Payments[0])
		},
		"missing paid time": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].Payments[0].PaidOn = ""
		},
		"wrong order": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].SalesOrderID = "other"
		},
		"fee mismatch": func(_ *provider.SquarespaceOrder, d []provider.SquarespaceTransactionDocument) {
			d[0].Payments[0].ProcessingFees[0].Amount.Value = "0.99"
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			o, d := squarespaceBridgeProofFixture()
			change(o, d)
			proof, code := verifySquarespaceProof(o, d)
			require.Nil(t, proof)
			require.NotEmpty(t, code)
		})
	}
	o, d := squarespaceBridgeProofFixture()
	proof, code := verifySquarespaceProof(o, d)
	require.Empty(t, code)
	require.Equal(t, int64(1000), proof.TotalMinor)
}

func TestSquarespaceRefundTargetUsesCumulativeExactCredit(t *testing.T) {
	for _, tc := range []struct{ credit, total, refund, want int64 }{{2000000000, 1000, 200, 400000000}, {2000000000, 1000, 400, 800000000}, {2000000000, 1000, 1000, 2000000000}, {100000000, 3, 1, 33333333}, {100000000, 3, 2, 66666667}, {100000000, 3, 3, 100000000}} {
		got, err := squarespaceRefundTarget(tc.credit, tc.total, tc.refund)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
	_, err := squarespaceRefundTarget(100, 100, 101)
	require.Error(t, err)
	_, err = squarespaceFloatUnits(math.NaN(), 100)
	require.Error(t, err)
	_, err = squarespaceFloatUnits(1.001, 100)
	require.Error(t, err)
}

type squarespaceBridgeCacheStub struct {
	calls int
	fail  bool
}

func (c *squarespaceBridgeCacheStub) InvalidateUserBalance(context.Context, int64) error {
	c.calls++
	if c.fail {
		return errors.New("cache unavailable")
	}
	return nil
}

func squarespaceBridgeDBFixture(t *testing.T) (*SquarespacePaymentBridge, *dbent.PaymentOrder, *provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, *mockUserRepo) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().SetEmail("bridge-test@example.com").SetPasswordHash("hash").SetUsername("bridge-test").SetBalance(15).Save(ctx)
	require.NoError(t, err)
	cfg := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}, []byte("0123456789abcdef0123456789abcdef"))
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey("squarespace").SetConfig(`{"websiteId":"site_1","productId":"6abc2d02ac3ba7447cdc0752","referenceFieldLabel":"RynexAI top-up reference","payLinkUrl":"https://test.squarespace.com/pay","currency":"GBP"}`).SetSupportedTypes("squarespace").Save(ctx)
	require.NoError(t, err)
	remote, docs := squarespaceBridgeProofFixture()
	now := time.Now().UTC()
	quote := RetailQuote{PaymentClaimMode: "reference", ProductID: "6abc2d02ac3ba7447cdc0752", CreditedAmountUSD: 20, BaseAmountGBP: 9.5, IncludedCostGBP: .5, TotalAmountGBP: 10, PayAmount: 10, Currency: "GBP", FX: map[string]float64{"GBP": 1, "USD": 2, "CNY": 10}, FXSource: "test", FXAsOf: now, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), CheckoutReference: remote.TopUpReference}
	raw, _ := json.Marshal(quote)
	var quoteMap map[string]any
	require.NoError(t, json.Unmarshal(raw, &quoteMap))
	snapshot := map[string]any{"schema_version": 1, "provider_instance_id": strconv.FormatInt(inst.ID, 10), "provider_key": "squarespace", "merchant_id": "site_1", "product_id": "6abc2d02ac3ba7447cdc0752", "currency": "GBP", "retail_quote": quoteMap}
	local, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(20).SetPayAmount(10).SetRechargeCode("BRIDGE-RECHARGE").SetOutTradeNo(remote.TopUpReference).SetPaymentType("squarespace").SetProviderKey("squarespace").SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).SetProviderSnapshot(snapshot).SetPaymentTradeNo("").SetOrderType("balance").SetStatus(OrderStatusCompleted).SetExpiresAt(quote.ExpiresAt).SetClientIP("127.0.0.1").SetSrcHost("test.example.com").Save(ctx)
	require.NoError(t, err)
	calls := 0
	repo := &mockUserRepo{deductAvailableBalanceFn: func(ctx context.Context, id int64, requested float64) (float64, error) {
		calls++
		tx := dbent.TxFromContext(ctx)
		require.NotNil(t, tx)
		u, err := tx.User.Get(ctx, id)
		if err != nil {
			return 0, err
		}
		amount := math.Min(requested, math.Max(u.Balance, 0))
		_, err = tx.User.UpdateOneID(id).AddBalance(-amount).Save(ctx)
		return amount, err
	}}
	usedBy := user.ID
	redeemRepo := &paymentOrderLifecycleRedeemRepo{codesByCode: map[string]*RedeemCode{local.RechargeCode: {ID: 1, Code: local.RechargeCode, Type: RedeemTypeBalance, Value: local.Amount, Status: StatusUsed, UsedBy: &usedBy}}}
	redeemSvc := NewRedeemService(redeemRepo, repo, nil, nil, nil, client, nil, nil)
	paymentSvc := &PaymentService{entClient: client, configService: cfg, userRepo: repo, redeemService: redeemSvc}
	bridge := NewSquarespacePaymentBridge(paymentSvc, nil, nil)
	bridge.SetRefundCacheInvalidators(&squarespaceBridgeCacheStub{}, nil)
	_ = calls
	return bridge, local, remote, docs, repo
}

func TestSquarespaceBridgeBindingEnforcesOneExternalPerLocalAndOnePayment(t *testing.T) {
	ctx := context.Background()
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	ledger, err := bridge.observeOrder(ctx, "site_1", remote)
	require.NoError(t, err)
	ledger, err = squarespaceSeededTestBind(t, bridge, ctx, ledger, local, remote, proof, "reference")
	require.NoError(t, err)
	require.Equal(t, local.ID, *ledger.LocalOrderID)
	_, err = squarespaceSeededTestBind(t, bridge, ctx, ledger, local, remote, proof, "reference")
	require.NoError(t, err)
	other := *remote
	other.ID = "external_order_2"
	other.OrderNumber = "2"
	ledger2, err := bridge.observeOrder(ctx, "site_1", &other)
	require.NoError(t, err)
	_, err = squarespaceSeededTestBind(t, bridge, ctx, ledger2, local, &other, proof, "reference")
	require.Error(t, err)
	require.True(t, dbent.IsConstraintError(err))
	reread, err := bridge.client.PaymentExternalOrder.Get(ctx, ledger2.ID)
	require.NoError(t, err)
	require.Nil(t, reread.LocalOrderID)
	count, err := bridge.client.PaymentExternalPayment.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestSquarespaceBridgeRefundsCumulativeDeltaAndDebt(t *testing.T) {
	ctx := context.Background()
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	ledger, err := bridge.observeOrder(ctx, "site_1", remote)
	require.NoError(t, err)
	ledger, err = squarespaceSeededTestBind(t, bridge, ctx, ledger, local, remote, proof, "reference")
	require.NoError(t, err)
	for _, cumulative := range []int64{200, 400, 400, 1000, 1000} {
		copyProof := *proof
		copyProof.Payments = append([]verifiedSquarespacePaymentProof(nil), proof.Payments...)
		copyProof.Payments[0].RefundedMinor = cumulative
		copyProof.RefundedMinor = cumulative
		require.NoError(t, bridge.applyObservedRefund(ctx, ledger, &copyProof, "REFUNDED"))
		ledger, err = bridge.client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
	}
	require.Equal(t, int64(2000000000), ledger.RefundTargetUsdUnits)
	require.Equal(t, int64(1500000000), ledger.RecoveredUsdUnits)
	require.Equal(t, int64(500000000), ledger.DebtUsdUnits)
	require.Equal(t, "REFUND_BALANCE_SHORTFALL", ledger.AnomalyCode)
	user, err := bridge.client.User.Get(ctx, local.UserID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
	count, err := bridge.client.PaymentExternalRefundJournal.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, count)
	journals, err := bridge.client.PaymentExternalRefundJournal.Query().Order(dbent.Asc(paymentexternalrefundjournal.FieldID)).All(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(400000000), journals[0].DeltaTargetUsdUnits)
	require.Equal(t, int64(400000000), journals[1].DeltaTargetUsdUnits)
	require.Equal(t, int64(1200000000), journals[2].DeltaTargetUsdUnits)
	stale := *proof
	stale.RefundedMinor = 200
	require.NoError(t, bridge.applyObservedRefund(ctx, ledger, &stale, "REFUNDED"))
	user, err = bridge.client.User.Get(ctx, local.UserID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
}

func TestSquarespaceBridgeRefundRollbackAndCacheRetry(t *testing.T) {
	t.Run("audit failure rolls back user and journal", func(t *testing.T) {
		ctx := context.Background()
		bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
		proof, code := verifySquarespaceProof(remote, docs)
		require.Empty(t, code)
		ledger, err := bridge.observeOrder(ctx, "site_1", remote)
		require.NoError(t, err)
		ledger, err = squarespaceSeededTestBind(t, bridge, ctx, ledger, local, remote, proof, "reference")
		require.NoError(t, err)
		bridge.client.PaymentAuditLog.Use(func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) { return nil, errors.New("audit failed") })
		})
		proof.RefundedMinor = 200
		proof.Payments[0].RefundedMinor = 200
		require.Error(t, bridge.applyObservedRefund(ctx, ledger, proof, "REFUNDED"))
		user, err := bridge.client.User.Get(ctx, local.UserID)
		require.NoError(t, err)
		require.Equal(t, 15.0, user.Balance)
		count, err := bridge.client.PaymentExternalRefundJournal.Query().Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	})
	t.Run("cache retry never deducts again", func(t *testing.T) {
		ctx := context.Background()
		bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
		proof, code := verifySquarespaceProof(remote, docs)
		require.Empty(t, code)
		ledger, err := bridge.observeOrder(ctx, "site_1", remote)
		require.NoError(t, err)
		ledger, err = squarespaceSeededTestBind(t, bridge, ctx, ledger, local, remote, proof, "reference")
		require.NoError(t, err)
		cache := &squarespaceBridgeCacheStub{fail: true}
		bridge.SetRefundCacheInvalidators(cache, nil)
		proof.RefundedMinor = 200
		proof.Payments[0].RefundedMinor = 200
		require.Error(t, bridge.applyObservedRefund(ctx, ledger, proof, "REFUNDED"))
		ledger, err = bridge.client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
		require.Equal(t, "CACHE_INVALIDATION_PENDING", ledger.AnomalyCode)
		cache.fail = false
		require.NoError(t, bridge.applyObservedRefund(ctx, ledger, proof, "REFUNDED"))
		user, err := bridge.client.User.Get(ctx, local.UserID)
		require.NoError(t, err)
		require.Equal(t, 11.0, user.Balance)
		require.Equal(t, 2, cache.calls)
	})
}

func TestSquarespaceTokensEncryptedCASAndUnknownRotationFailsClosed(t *testing.T) {
	bridge, _, _, _, _ := squarespaceBridgeDBFixture(t)
	ctx := context.Background()
	cfg := bridge.config
	require.NoError(t, cfg.SaveSquarespaceOAuthCredentials(ctx, "site_1", "client_id", "client_secret"))
	now := time.Now().UTC()
	pair := &provider.SquarespaceOAuthTokenPair{AccessToken: "access_secret", RefreshToken: "refresh_secret", AccessTokenExpiresAt: fmt.Sprint(now.Add(time.Hour).Unix()), RefreshTokenExpiresAt: fmt.Sprint(now.Add(7 * 24 * time.Hour).Unix()), TokenType: "bearer"}
	require.NoError(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site_1", "client_id", 0, pair))
	row, err := cfg.squarespaceSyncState(ctx, "site_1")
	require.NoError(t, err)
	require.NotContains(t, row.EncryptedOauthTokens, "access_secret")
	require.NotContains(t, row.EncryptedOauthCredentials, "client_secret")
	encoded, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "encrypted_oauth_tokens")
	require.NotContains(t, string(encoded), "client_secret")
	require.Error(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site_1", "client_id", 0, pair))
	source, err := cfg.squarespaceTokenSource(ctx, "site_1")
	require.NoError(t, err)
	access, err := source.AccessToken(ctx)
	require.NoError(t, err)
	require.Equal(t, pair.AccessToken, access)
	_, err = cfg.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID)).SetRotationPhase("unknown").Save(ctx)
	require.NoError(t, err)
	_, err = source.AccessToken(ctx)
	require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
}

func TestSquarespaceReceiptClaimCannotReusePaymentBeforeQuote(t *testing.T) {
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	proof.PaidAt = PaymentOrderRetailQuote(local).IssuedAt.Add(-time.Second)
	require.Equal(t, "PAYMENT_OUTSIDE_QUOTE", validateSquarespaceQuoteProof(local, remote, proof, "site_1"))
	for _, raw := range []string{"#00001", "1", " 0001 "} {
		number, err := normalizeSquarespaceReceiptNumber(raw)
		require.NoError(t, err)
		require.Equal(t, "1", number)
	}
	_, err := normalizeSquarespaceReceiptNumber("someone@example.com")
	require.Error(t, err)
	count, err := bridge.client.PaymentExternalOrder.Query().Where(paymentexternalorder.LocalOrderIDNotNil()).Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestSquarespaceBridgeMultiplePaymentsPinEveryLegAndTime(t *testing.T) {
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	money := func(value string) provider.SquarespaceMoney {
		return provider.SquarespaceMoney{Currency: "GBP", Value: value}
	}
	first := docs[0].Payments[0]
	first.Amount = money("5.00")
	first.NetAmount = money("4.80")
	first.ProcessingFees = []provider.SquarespaceProcessingFee{{Amount: money("0.20"), NetAmount: money("0.20"), RefundedAmount: money("0.00")}}
	second := first
	second.ID = "payment_2"
	second.ExternalTransactionID = "charge_2"
	second.NetAmount = money("4.75")
	second.ProcessingFees = []provider.SquarespaceProcessingFee{{Amount: money("0.25"), NetAmount: money("0.25"), RefundedAmount: money("0.00")}}
	docs[0].Payments = []provider.SquarespacePayment{first, second}
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	require.Len(t, proof.Payments, 2)
	require.Empty(t, validateSquarespaceQuoteProof(local, remote, proof, "site_1"))
	ledger, err := bridge.observeOrder(context.Background(), "site_1", remote)
	require.NoError(t, err)
	_, err = squarespaceSeededTestBind(t, bridge, context.Background(), ledger, local, remote, proof, "reference")
	require.NoError(t, err)
	count, err := bridge.client.PaymentExternalPayment.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, count)
	docs[0].Payments[0].PaidOn = PaymentOrderRetailQuote(local).IssuedAt.Add(-time.Second).Format(time.RFC3339Nano)
	proof, code = verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	require.Equal(t, "PAYMENT_OUTSIDE_QUOTE", validateSquarespaceQuoteProof(local, remote, proof, "site_1"))
	docs[0].Payments[1].ExternalTransactionID = docs[0].Payments[0].ExternalTransactionID
	_, code = verifySquarespaceProof(remote, docs)
	require.NotEmpty(t, code)
}

func TestSquarespaceReceiptBindingRequiresPurposeAuthority(t *testing.T) {
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	ctx := context.Background()
	raw := local.ProviderSnapshot["retail_quote"].(map[string]any)
	raw["payment_claim_mode"] = "receipt_otp"
	local, err := bridge.client.PaymentOrder.UpdateOneID(local.ID).SetProviderSnapshot(local.ProviderSnapshot).Save(ctx)
	require.NoError(t, err)
	remote.TopUpReference = ""
	remote.ReferenceStatus = "missing"
	require.NoError(t, ValidateSquarespaceReceiptClaimProof(local, remote, docs))
	require.Error(t, bridge.payment.BindVerifiedReceiptClaim(ctx, local, remote, docs))
	hash, err := SquarespaceReceiptQuoteHash(local)
	require.NoError(t, err)
	for _, authority := range []context.Context{
		withVerifiedSquarespaceReceiptClaim(ctx, local.UserID+1, local.ID, remote.ID, hash),
		withVerifiedSquarespaceReceiptClaim(ctx, local.UserID, local.ID+1, remote.ID, hash),
		withVerifiedSquarespaceReceiptClaim(ctx, local.UserID, local.ID, "other", hash),
		withVerifiedSquarespaceReceiptClaim(ctx, local.UserID, local.ID, remote.ID, "stalehash"),
	} {
		require.Error(t, bridge.payment.BindVerifiedReceiptClaim(authority, local, remote, docs))
	}
	count, err := bridge.client.PaymentExternalOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	old := docs[0].Payments[0].PaidOn
	docs[0].Payments[0].PaidOn = PaymentOrderRetailQuote(local).IssuedAt.Add(-time.Second).Format(time.RFC3339Nano)
	require.Error(t, ValidateSquarespaceReceiptClaimProof(local, remote, docs))
	docs[0].Payments[0].PaidOn = old
}

func TestSquarespaceTokenRotationCommitsBothTokensBeforeReturn(t *testing.T) {
	bridge, _, _, _, _ := squarespaceBridgeDBFixture(t)
	ctx := context.Background()
	cfg := bridge.config
	now := time.Now().UTC()
	require.NoError(t, cfg.SaveSquarespaceOAuthCredentials(ctx, "site_1", "client_id", "client_secret"))
	pair := &provider.SquarespaceOAuthTokenPair{AccessToken: "old_access", RefreshToken: "old_refresh", AccessTokenExpiresAt: fmt.Sprint(now.Add(-time.Minute).Unix()), RefreshTokenExpiresAt: fmt.Sprint(now.Add(time.Hour).Unix()), TokenType: "bearer"}
	require.NoError(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site_1", "client_id", 0, pair))
	source, err := cfg.squarespaceTokenSource(ctx, "site_1")
	require.NoError(t, err)
	refreshed := &provider.SquarespaceOAuthTokenPair{AccessToken: "new_access", RefreshToken: "new_refresh", AccessTokenExpiresAt: fmt.Sprint(now.Add(30 * time.Minute).Unix()), RefreshTokenExpiresAt: fmt.Sprint(now.Add(7 * 24 * time.Hour).Unix()), TokenType: "bearer"}
	source.refresh = func(ctx context.Context, rt string) (*provider.SquarespaceOAuthTokenPair, error) {
		require.Equal(t, pair.RefreshToken, rt)
		state, err := cfg.squarespaceSyncState(ctx, "site_1")
		require.NoError(t, err)
		require.Equal(t, "request_in_flight", state.RotationPhase)
		return refreshed, nil
	}
	got, err := source.AccessToken(ctx)
	require.NoError(t, err)
	require.Equal(t, refreshed.AccessToken, got)
	stored, version, err := cfg.LoadSquarespaceOAuthTokens(ctx, "site_1")
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
	require.Equal(t, *refreshed, *stored)
	state, err := cfg.squarespaceSyncState(ctx, "site_1")
	require.NoError(t, err)
	require.Equal(t, "idle", state.RotationPhase)
}

func TestSquarespaceLostRefreshResponseNeverReplaysOldToken(t *testing.T) {
	bridge, _, _, _, _ := squarespaceBridgeDBFixture(t)
	ctx := context.Background()
	cfg := bridge.config
	now := time.Now().UTC()
	require.NoError(t, cfg.SaveSquarespaceOAuthCredentials(ctx, "site_1", "client_id", "client_secret"))
	pair := &provider.SquarespaceOAuthTokenPair{AccessToken: "old_access", RefreshToken: "old_refresh", AccessTokenExpiresAt: fmt.Sprint(now.Add(-time.Minute).Unix()), RefreshTokenExpiresAt: fmt.Sprint(now.Add(time.Hour).Unix()), TokenType: "bearer"}
	require.NoError(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site_1", "client_id", 0, pair))
	source, err := cfg.squarespaceTokenSource(ctx, "site_1")
	require.NoError(t, err)
	calls := 0
	source.refresh = func(context.Context, string) (*provider.SquarespaceOAuthTokenPair, error) {
		calls++
		return nil, errors.New("lost response")
	}
	_, err = source.AccessToken(ctx)
	require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
	_, err = source.AccessToken(ctx)
	require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
	require.Equal(t, 1, calls)
	state, err := cfg.squarespaceSyncState(ctx, "site_1")
	require.NoError(t, err)
	require.Equal(t, "unknown", state.RotationPhase)
}

type squarespaceCancelSource struct{ entered chan struct{} }

func (s *squarespaceCancelSource) VerifyWebsite(ctx context.Context) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}
func (*squarespaceCancelSource) ListOrders(context.Context, string, string, string) (*provider.SquarespaceOrderPage, error) {
	return nil, errors.New("unexpected order read after canceled verify")
}
func (*squarespaceCancelSource) GetOrder(context.Context, string) (*provider.SquarespaceOrder, error) {
	return nil, errors.New("unexpected order read")
}
func (*squarespaceCancelSource) ListAllTransactionsForOrder(context.Context, string, int) ([]provider.SquarespaceTransactionDocument, error) {
	return nil, errors.New("unexpected transaction read")
}

func TestSquarespaceBridgeStopCancelsActiveRun(t *testing.T) {
	bridge, _, _, _, _ := squarespaceBridgeDBFixture(t)
	source := &squarespaceCancelSource{entered: make(chan struct{})}
	factoryCalls := 0
	bridge.sourceFactory = func(context.Context, map[string]string) (squarespaceBridgeSource, error) {
		factoryCalls++
		return source, nil
	}
	bridge.Start()
	select {
	case <-source.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not enter source verify")
	}
	done := make(chan struct{})
	go func() { bridge.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop waited for the full sync timeout")
	}
	require.Equal(t, 1, factoryCalls)
	count, err := bridge.client.PaymentExternalRefundJournal.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}
func TestSquarespaceBridgeStopBeforeStartDoesNotLaunchRun(t *testing.T) {
	bridge, _, _, _, _ := squarespaceBridgeDBFixture(t)
	factoryCalls := 0
	bridge.sourceFactory = func(context.Context, map[string]string) (squarespaceBridgeSource, error) {
		factoryCalls++
		return nil, errors.New("must not start")
	}
	bridge.Stop()
	bridge.Start()
	bridge.Stop()
	require.Zero(t, factoryCalls)
}

func TestSquarespaceProductProofRejectsOtherGoodsAndMixedCart(t *testing.T) {
	_, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	require.Empty(t, validateSquarespaceQuoteProof(local, remote, proof, "site_1"))
	wrong := *remote
	wrong.LineItems = []provider.SquarespaceLineItem{{ID: "line_other", ProductID: "other_goods"}}
	require.Equal(t, "TOPUP_PRODUCT_MISMATCH", validateSquarespaceQuoteProof(local, &wrong, proof, "site_1"))
	mixed := *remote
	mixed.LineItems = append(append([]provider.SquarespaceLineItem(nil), remote.LineItems...), provider.SquarespaceLineItem{ID: "line_other", ProductID: "other_goods"})
	require.Equal(t, "TOPUP_PRODUCT_MISMATCH", validateSquarespaceQuoteProof(local, &mixed, proof, "site_1"))
	missing := *remote
	missing.LineItems = nil
	require.Equal(t, "TOPUP_PRODUCT_MISMATCH", validateSquarespaceQuoteProof(local, &missing, proof, "site_1"))
	old := local.ProviderSnapshot["product_id"]
	local.ProviderSnapshot["product_id"] = "other_goods"
	require.Equal(t, "TOPUP_PRODUCT_SNAPSHOT_INVALID", validateSquarespaceQuoteProof(local, remote, proof, "site_1"))
	local.ProviderSnapshot["product_id"] = old
}

func TestSquarespaceBoundRefundUsesOriginalProductAfterConfigChanges(t *testing.T) {
	ctx := context.Background()
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	ledger, err := bridge.observeOrder(ctx, "site_1", remote)
	require.NoError(t, err)
	ledger, err = squarespaceSeededTestBind(t, bridge, ctx, ledger, local, remote, proof, "reference")
	require.NoError(t, err)
	instanceID, err := strconv.ParseInt(*local.ProviderInstanceID, 10, 64)
	require.NoError(t, err)
	instance, err := bridge.client.PaymentProviderInstance.Get(ctx, instanceID)
	require.NoError(t, err)
	cfg, err := bridge.config.decryptConfig(instance.Config)
	require.NoError(t, err)
	cfg["productId"] = "0123456789abcdef01234567"
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	instance, err = bridge.client.PaymentProviderInstance.UpdateOneID(instance.ID).SetConfig(string(configJSON)).Save(ctx)
	require.NoError(t, err)
	money := func(value string) provider.SquarespaceMoney {
		return provider.SquarespaceMoney{Currency: "GBP", Value: value}
	}
	refunded := *remote
	refunded.PaymentState = "REFUNDED"
	refunded.RefundedTotal = money("2.00")
	docs[0].TotalNetPayment = money("7.55")
	docs[0].Payments[0].RefundedAmount = money("2.00")
	docs[0].Payments[0].NetAmount = money("7.55")
	docs[0].Payments[0].Refunds = []provider.SquarespaceRefund{{ID: "refund_1", RefundedOn: time.Now().UTC().Format(time.RFC3339Nano), Amount: money("2.00")}}
	bad := refunded
	bad.LineItems = []provider.SquarespaceLineItem{{ID: "line_new", ProductID: cfg["productId"]}}
	require.NoError(t, bridge.processExternalOrder(ctx, instance, "site_1", &bad, docs, ledger))
	unchanged, err := bridge.client.User.Get(ctx, local.UserID)
	require.NoError(t, err)
	require.Equal(t, 15.0, unchanged.Balance)
	rejected, err := bridge.client.PaymentExternalOrder.Get(ctx, ledger.ID)
	require.NoError(t, err)
	require.Equal(t, "TOPUP_PRODUCT_MISMATCH", rejected.AnomalyCode)
	require.NoError(t, bridge.processExternalOrder(ctx, instance, "site_1", &refunded, docs, ledger))
	user, err := bridge.client.User.Get(ctx, local.UserID)
	require.NoError(t, err)
	require.Equal(t, 11.0, user.Balance)
	journalCount, err := bridge.client.PaymentExternalRefundJournal.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, journalCount)
}

func squarespaceSeededTestBind(t *testing.T, bridge *SquarespacePaymentBridge, ctx context.Context, ledger *dbent.PaymentExternalOrder, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, proof *verifiedSquarespaceProof, method string) (*dbent.PaymentExternalOrder, error) {
	t.Helper()
	prior, err := bridge.client.PaymentOrder.Get(ctx, local.ID)
	if err != nil {
		return nil, err
	}
	if prior.Status != OrderStatusPending && prior.Status != OrderStatusExpired {
		_, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(OrderStatusPending).Save(ctx)
		if err != nil {
			return nil, err
		}
	}
	bound, err := bridge.bindExternalOrder(ctx, ledger, local, remote, proof, method)
	if prior.Status != OrderStatusPending && prior.Status != OrderStatusExpired {
		_, restoreErr := bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(prior.Status).Save(ctx)
		if err == nil {
			err = restoreErr
		}
	}
	return bound, err
}
