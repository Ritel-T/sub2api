//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type gelvLatencyProvider struct {
	paymentOrderLifecycleQueryProvider
	query  func(context.Context, string) (*payment.QueryOrderResponse, error)
	create func(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error)
}

func (p *gelvLatencyProvider) QueryOrder(ctx context.Context, ref string) (*payment.QueryOrderResponse, error) {
	return p.query(ctx, ref)
}
func (p *gelvLatencyProvider) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if p.create == nil {
		panic("unexpected creation")
	}
	return p.create(ctx, req)
}

func gelvLatencyBind(t *testing.T, s *PaymentService, o *dbent.PaymentOrder, p payment.Provider) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	inst, err := s.entClient.PaymentProviderInstance.Create().SetProviderKey(payment.TypeGelv).SetName("latency-test").SetConfig("{}").SetSupportedTypes(payment.TypeGelv).SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	snapshot := make(map[string]any)
	for k, v := range o.ProviderSnapshot {
		snapshot[k] = v
	}
	snapshot["provider_instance_id"] = fmt.Sprint(inst.ID)
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetProviderInstanceID(fmt.Sprint(inst.ID)).SetProviderSnapshot(snapshot).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	s.loadBalancer = &captureLoadBalancer{}
	t.Cleanup(replacePaymentProviderFactoryForTest(t, p))
	return o
}

func gelvLatencyClone(t *testing.T, s *PaymentService, o *dbent.PaymentOrder, suffix, status string) *dbent.PaymentOrder {
	t.Helper()
	snapshot := make(map[string]any)
	for k, v := range o.ProviderSnapshot {
		snapshot[k] = v
	}
	// Metadata is generated from the fresh order in tests which need validation.
	v, err := s.entClient.PaymentOrder.Create().SetUserID(o.UserID).SetUserEmail(o.UserEmail).SetUserName(o.UserName).
		SetAmount(o.Amount).SetPayAmount(o.PayAmount).SetFeeRate(0).SetRechargeCode("GELV-LATENCY-" + suffix).
		SetOutTradeNo("sub2_latency_" + suffix).SetPaymentType(o.PaymentType).SetPaymentTradeNo("gelv-" + suffix).
		SetProviderKey(payment.TypeGelv).SetProviderInstanceID(psStringValue(o.ProviderInstanceID)).SetProviderSnapshot(snapshot).
		SetOrderType(payment.OrderTypeBalance).SetStatus(status).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("site.example.com").Save(context.Background())
	require.NoError(t, err)
	return v
}

func TestGelvLatencySelectorsKeepActiveAndHistoryDisjoint(t *testing.T) {
	s, o, _, _ := gelvServiceOrder(t, OrderStatusPending)
	ctx := context.Background()
	stale := time.Now().Add(-paymentFulfillmentLeaseDuration - time.Minute)
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetUpdatedAt(stale).Save(ctx)
	require.NoError(t, err)
	expectedActive := map[int64]bool{o.ID: true}
	expectedHistory := map[int64]bool{}
	for _, status := range []string{OrderStatusPaid, OrderStatusFailed, OrderStatusRecharging, OrderStatusCompleted, OrderStatusCancelled, OrderStatusExpired, OrderStatusRefundPending, OrderStatusRefunded} {
		other := gelvLatencyClone(t, s, o, status, status)
		_, err = s.entClient.PaymentOrder.UpdateOneID(other.ID).SetUpdatedAt(stale).Save(ctx)
		require.NoError(t, err)
		switch status {
		case OrderStatusPaid, OrderStatusFailed, OrderStatusRecharging:
			expectedActive[other.ID] = true
		default:
			expectedHistory[other.ID] = true
		}
	}
	live := gelvLatencyClone(t, s, o, "live-lease", OrderStatusRecharging)
	blocked := gelvLatencyClone(t, s, o, "blocked-complete", OrderStatusCompleted)
	snapshot := blocked.ProviderSnapshot
	snapshot["gelv_reconciliation_blocked"] = "confirmed_refund"
	_, err = s.entClient.PaymentOrder.UpdateOneID(blocked.ID).SetProviderSnapshot(snapshot).Save(ctx)
	require.NoError(t, err)
	expectedHistory[blocked.ID] = true
	for i, hold := range []map[string]any{{"gelv_reconciliation_blocked": "paid_evidence_mismatch"}, {"gelv_refund_pending": "true"}, {"gelv_refund_pending": true}} {
		held := gelvLatencyClone(t, s, o, fmt.Sprintf("active-held-%d", i), OrderStatusFailed)
		snap := held.ProviderSnapshot
		for k, v := range hold {
			snap[k] = v
		}
		_, err = s.entClient.PaymentOrder.UpdateOneID(held.ID).SetProviderSnapshot(snap).Save(ctx)
		require.NoError(t, err)
		expectedHistory[held.ID] = true
	}
	unbound := gelvLatencyClone(t, s, o, "unbound-expired", OrderStatusExpired)
	_, err = s.entClient.PaymentOrder.UpdateOneID(unbound.ID).SetPaymentTradeNo("").Save(ctx)
	require.NoError(t, err)
	foreign := gelvLatencyClone(t, s, o, "foreign", OrderStatusPending)
	_, err = s.entClient.PaymentOrder.UpdateOneID(foreign.ID).SetProviderKey(payment.TypeAlipay).Save(ctx)
	require.NoError(t, err)
	for _, active := range []bool{true, false} {
		got, err := s.gelvOrdersForLane(ctx, active, 50)
		require.NoError(t, err)
		ids := map[int64]bool{}
		for _, v := range got {
			ids[v.ID] = true
		}
		if active {
			require.Equal(t, expectedActive, ids)
		} else {
			require.Equal(t, expectedHistory, ids)
		}
		require.NotContains(t, ids, live.ID)
	}
	// Fresh state, not the candidate captured by a previous cycle, controls work.
	_, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusCompleted).Save(ctx)
	require.NoError(t, err)
	require.False(t, s.reconcileGelvLaneOrder(ctx, o.ID, true))
}

func TestGelvLatencySlowOrErrorFirstDoesNotStarveAndRotates(t *testing.T) {
	for _, slow := range []bool{true, false} {
		t.Run(fmt.Sprint(slow), func(t *testing.T) {
			s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
			secondSeen := make(chan struct{}, 1)
			var running, peak atomic.Int64
			var mu sync.Mutex
			calls := map[string]int{}
			firstRef := o.PaymentTradeNo
			p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
			p.query = func(ctx context.Context, ref string) (*payment.QueryOrderResponse, error) {
				n := running.Add(1)
				defer running.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				mu.Lock()
				calls[ref]++
				mu.Unlock()
				if ref != firstRef {
					select {
					case secondSeen <- struct{}{}:
					default:
					}
				}
				if slow && ref == firstRef {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return nil, errors.New("test upstream failure")
			}
			o = gelvLatencyBind(t, s, o, p)
			for i := 0; i < 8; i++ {
				gelvLatencyClone(t, s, o, fmt.Sprint(i), OrderStatusPending)
			}
			selected, err := s.gelvOrdersForLane(context.Background(), true, gelvActiveScanLimit)
			require.NoError(t, err)
			require.Len(t, selected, gelvActiveScanLimit)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); _, _ = s.ReconcileGelvActiveOrders(ctx) }()
			select {
			case <-secondSeen:
			case <-time.After(time.Second):
				t.Fatal("first order starved later orders")
			}
			<-done
			require.LessOrEqual(t, peak.Load(), int64(gelvActiveConcurrency))
			mu.Lock()
			for _, count := range calls {
				require.Equal(t, 1, count)
			}
			require.Len(t, calls, gelvActiveScanLimit)
			mu.Unlock()
			next, err := s.gelvOrdersForLane(context.Background(), true, gelvActiveScanLimit)
			require.NoError(t, err)
			require.NotEqual(t, selected[0].ID, next[0].ID, "attempts, including failures, must rotate behind untouched orders")
			require.Zero(t, users.getByIDUser.Balance)
			require.Zero(t, codes.createCalls)
		})
	}
}

func TestGelvLatencyConcurrentSweepsFulfillOnlyOnce(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	metadata := gelvServiceMetadata(o, "wechat_virtual")
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		calls.Add(1)
		close(started)
		<-release
		return &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: metadata}, nil
	}
	o = gelvLatencyBind(t, s, o, p)
	cache := &fakeLeaderLockCache{}
	ctx := context.WithValue(context.Background(), gelvReconcileLocksKey{}, gelvReconcileLocks{cache: cache})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.ReconcileGelvActiveOrders(ctx) }()
	<-started
	for i := 0; i < 4; i++ {
		n, err := s.ReconcileGelvActiveOrders(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
	}
	require.Equal(t, int64(1), calls.Load())
	close(release)
	<-done
	saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Len(t, codes.useCalls, 1)
	require.Equal(t, 1, codes.createCalls)
	n, err := s.ReconcileGelvActiveOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, int64(1), calls.Load())
}

func TestGelvLatencyPollDeadlineDoesNotCancelCommittedFulfillment(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		return &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: gelvServiceMetadata(o, "wechat_virtual")}, nil
	}
	o = gelvLatencyBind(t, s, o, p)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	users.updateBalanceFn = func(fulfillCtx context.Context, _ int64, amount float64) error {
		<-ctx.Done()
		if err := fulfillCtx.Err(); err != nil {
			return err
		}
		users.getByIDUser.Balance += amount
		return nil
	}
	n, _ := s.ReconcileGelvActiveOrders(ctx)
	require.Equal(t, 1, n)
	saved, err := s.entClient.PaymentOrder.Get(context.Background(), o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Len(t, codes.useCalls, 1)
}

func TestGelvLatencyHistoryCannotPollActiveInFlightAfterStateChange(t *testing.T) {
	s, o, _, _ := gelvServiceOrder(t, OrderStatusPending)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		calls.Add(1)
		close(started)
		<-release
		return nil, errors.New("test")
	}
	o = gelvLatencyBind(t, s, o, p)
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.ReconcileGelvActiveOrders(context.Background()) }()
	<-started
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusCompleted).Save(context.Background())
	require.NoError(t, err)
	n, err := s.ReconcileGelvOrders(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, int64(1), calls.Load())
	close(release)
	<-done
	// Across instances the independent cycle locks still share a per-order guard.
	cache := &fakeLeaderLockCache{}
	key := fmt.Sprintf("payment:gelv:order:%d:poll", o.ID)
	_, _ = cache.TryAcquireLeaderLock(context.Background(), key, "peer", time.Minute)
	ctx := context.WithValue(context.Background(), gelvReconcileLocksKey{}, gelvReconcileLocks{cache: cache})
	require.False(t, s.reconcileGelvLaneOrder(ctx, o.ID, false))
	require.Equal(t, int64(1), calls.Load())
}

func TestGelvLatencyIdleAndAggregateLogs(t *testing.T) {
	s, o, _, _ := gelvServiceOrder(t, OrderStatusCompleted)
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	n, err := s.ReconcileGelvActiveOrders(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, output.String())
	_, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusPending).Save(context.Background())
	require.NoError(t, err)
	_, err = s.ReconcileGelvActiveOrders(context.Background())
	require.NoError(t, err)
	require.Contains(t, output.String(), `"selected":1`)
	require.Contains(t, output.String(), `"recovered":0`)
	require.NotContains(t, output.String(), o.OutTradeNo)
	require.NotContains(t, output.String(), o.UserEmail)
	count, err := s.entClient.PaymentOrder.Query().Where(paymentorder.IDEQ(o.ID)).Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestGelvLatencyCreationRecoverySpendsOneCallPerCycleOnSameIdentity(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
	ref, trade := o.OutTradeNo, o.PaymentTradeNo
	var creates, queries atomic.Int64
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.create = func(_ context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
		creates.Add(1)
		if req.OrderID != ref {
			return nil, errors.New("source identity changed")
		}
		return &payment.CreatePaymentResponse{TradeNo: trade}, nil
	}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		queries.Add(1)
		return &payment.QueryOrderResponse{TradeNo: trade, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: gelvServiceMetadata(o, "wechat_virtual")}, nil
	}
	o = gelvLatencyBind(t, s, o, p)
	var err error
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetPaymentTradeNo("").Save(context.Background())
	require.NoError(t, err)
	n, err := s.ReconcileGelvActiveOrders(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, int64(1), creates.Load())
	require.Zero(t, queries.Load())
	require.Zero(t, users.getByIDUser.Balance)
	o, err = s.entClient.PaymentOrder.Get(context.Background(), o.ID)
	require.NoError(t, err)
	require.Equal(t, trade, o.PaymentTradeNo)
	n, err = s.ReconcileGelvActiveOrders(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, int64(1), creates.Load())
	require.Equal(t, int64(1), queries.Load())
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Len(t, codes.useCalls, 1)
	count, err := s.entClient.PaymentOrder.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestGelvLatencySlowLaneStillClearsSignedRefundHoldForActiveStatus(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
	var queries atomic.Int64
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		queries.Add(1)
		metadata := gelvServiceMetadata(o, "wechat_virtual")
		metadata["refund_pending"] = "false"
		return &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: metadata}, nil
	}
	o = gelvLatencyBind(t, s, o, p)
	heldMetadata := gelvServiceMetadata(o, "wechat_virtual")
	heldMetadata["refund_pending"] = "true"
	require.NoError(t, s.holdGelvRefund(context.Background(), o, heldMetadata))
	n, err := s.ReconcileGelvActiveOrders(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	require.Zero(t, queries.Load())
	n, err = s.ReconcileGelvOrders(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, int64(1), queries.Load())
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Len(t, codes.useCalls, 1)
	saved, err := s.entClient.PaymentOrder.Get(context.Background(), o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	require.False(t, gelvRefundHeld(saved))
}

func TestGelvLatencyGeneralCycleNeverPollsActiveGelvOrders(t *testing.T) {
	s, o, _, _ := gelvServiceOrder(t, OrderStatusPending)
	var queries atomic.Int64
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		queries.Add(1)
		return nil, errors.New("test")
	}
	gelvLatencyBind(t, s, o, p)
	expiry := NewPaymentOrderExpiryService(s, time.Minute)
	expiry.runOnce()
	require.Zero(t, queries.Load(), "the original general cycle must not retain a second active sweep")
	expiry.runGelvActiveOnce()
	require.Equal(t, int64(1), queries.Load())
}
