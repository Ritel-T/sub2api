package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const expiryCheckTimeout = 30 * time.Second

const (
	gelvActiveInterval           = 5 * time.Second
	gelvActiveCheckTimeout       = 4 * time.Second
	gelvActiveFulfillmentTimeout = 18 * time.Second
	gelvActiveLeaderLockKey      = "payment:gelv:active:leader"
	// A cycle drains queries (4s), committed fulfillment (18s) and touches (2s)
	// before releasing leadership. The provider's own HTTP timeout is 18s.
	gelvActiveLeaderLockTTL = 30 * time.Second
)

const (
	// paymentOrderExpiryLeaderLockKey gates the periodic reconcile + expiry sweep so
	// that only one instance issues the upstream payment-provider calls per cycle.
	paymentOrderExpiryLeaderLockKey = "payment:order:expiry:leader"
	// paymentOrderExpiryLeaderLockTTL must exceed the combined reconcile + expiry
	// timeouts (3 * expiryCheckTimeout) so the lock never expires mid-run.
	paymentOrderExpiryLeaderLockTTL = 3 * time.Minute
)

// PaymentOrderExpiryService periodically expires timed-out payment orders.
type PaymentOrderExpiryService struct {
	paymentSvc  *PaymentService
	interval    time.Duration
	stopCh      chan struct{}
	lifecycleMu sync.Mutex
	startOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

func NewPaymentOrderExpiryService(paymentSvc *PaymentService, interval time.Duration) *PaymentOrderExpiryService {
	return &PaymentOrderExpiryService{
		paymentSvc: paymentSvc,
		interval:   interval,
		stopCh:     make(chan struct{}),
		instanceID: uuid.NewString(),
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the periodic reconcile/expiry sweep. When both are nil the job
// runs ungated (single-instance / test behavior).
func (s *PaymentOrderExpiryService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *PaymentOrderExpiryService) Start() {
	if s == nil || s.paymentSvc == nil || s.interval <= 0 {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.startOnce.Do(func() {
		select {
		case <-s.stopCh:
			return
		default:
		}
		s.startLoop(s.interval, s.runOnce)
		s.startLoop(gelvActiveInterval, s.runGelvActiveOnce)
	})
}

// Each lane runs immediately and owns its timer and synchronous drain. A slow
// general reconcile cannot postpone the Gelv lane, and cycles never overlap.
func (s *PaymentOrderExpiryService) startLoop(interval time.Duration, run func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			default:
			}
			run()
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *PaymentOrderExpiryService) gelvContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, gelvReconcileLocksKey{}, gelvReconcileLocks{cache: s.lockCache, db: s.db})
}

func (s *PaymentOrderExpiryService) runGelvActiveOnce() {
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, gelvActiveLeaderLockKey, uuid.NewString(), gelvActiveLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(s.gelvContext(context.Background()), gelvActiveCheckTimeout)
	defer cancel()
	_, err := s.paymentSvc.ReconcileGelvActiveOrders(ctx)
	if err != nil && err != context.DeadlineExceeded {
		slog.Warn("[PaymentOrderExpiry] active mini-program reconciliation incomplete", "error", err)
	}
}

func (s *PaymentOrderExpiryService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.lifecycleMu.Unlock()
	s.wg.Wait()
}

func (s *PaymentOrderExpiryService) runOnce() {
	// Multi-instance guard: only the leader reconciles/expires orders per cycle,
	// avoiding N× upstream payment-provider API calls and update races.
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, paymentOrderExpiryLeaderLockKey, s.instanceID, paymentOrderExpiryLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	reconcileCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	recovered, err := s.paymentSvc.ReconcilePendingPaymentOrders(reconcileCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending payment orders", "error", err)
	} else if recovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid orders", "count", recovered)
	}

	expireCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	defer cancel()
	expired, err := s.paymentSvc.ExpireTimedOutOrders(expireCtx)
	if err != nil {
		slog.Error("[PaymentOrderExpiry] failed to expire orders", "error", err)
		return
	}
	if expired > 0 {
		slog.Info("[PaymentOrderExpiry] expired timed-out orders", "count", expired)
	}
	// A slow partner must not delay the existing channels' reconcile/expiry work.
	gelvCtx, gelvCancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	_, gelvErr := s.paymentSvc.ReconcileGelvOrders(s.gelvContext(gelvCtx))
	gelvCancel()
	if gelvErr != nil {
		slog.Warn("[PaymentOrderExpiry] mini-program reconciliation incomplete", "error", gelvErr)
	}
}
