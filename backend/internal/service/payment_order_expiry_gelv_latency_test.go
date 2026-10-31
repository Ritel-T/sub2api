//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type gelvLatencyLockEvent struct {
	key, owner string
	ttl        time.Duration
	at         time.Time
}
type gelvLatencySchedulerCache struct {
	fakeLeaderLockCache
	generalEntered chan struct{}
	generalRelease chan struct{}
	events         chan gelvLatencyLockEvent
	once           sync.Once
}

func (c *gelvLatencySchedulerCache) TryAcquireLeaderLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if key == paymentOrderExpiryLeaderLockKey && c.generalRelease != nil {
		c.once.Do(func() { close(c.generalEntered) })
		<-c.generalRelease
	}
	ok, err := c.fakeLeaderLockCache.TryAcquireLeaderLock(ctx, key, owner, ttl)
	if ok && c.events != nil {
		c.events <- gelvLatencyLockEvent{key, owner, ttl, time.Now()}
	}
	return ok, err
}

func TestGelvLatencySchedulerFreshFiveSecondLaneIndependentOfGeneralAndStopDrains(t *testing.T) {
	s, _, _, _ := gelvServiceOrder(t, OrderStatusCompleted)
	cache := &gelvLatencySchedulerCache{generalEntered: make(chan struct{}), generalRelease: make(chan struct{}), events: make(chan gelvLatencyLockEvent, 20)}
	expiry := NewPaymentOrderExpiryService(s, time.Minute)
	expiry.SetLeaderLock(cache, nil)
	require.Equal(t, 5*time.Second, gelvActiveInterval)
	require.Greater(t, gelvActiveLeaderLockTTL, gelvActiveCheckTimeout+gelvActiveFulfillmentTimeout+6*time.Second)
	start := time.Now()
	expiry.Start()
	expiry.Start() // Repeated Start must never install a second worker.
	<-cache.generalEntered
	var fast []gelvLatencyLockEvent
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	for len(fast) < 2 {
		select {
		case event := <-cache.events:
			if event.key == gelvActiveLeaderLockKey {
				fast = append(fast, event)
			}
		case <-deadline.C:
			close(cache.generalRelease)
			expiry.Stop()
			t.Fatal("slow general lane blocked the fresh/timed fast lane")
		}
	}
	require.Less(t, fast[0].at.Sub(start), time.Second)
	require.InDelta(t, 5.0, fast[1].at.Sub(fast[0].at).Seconds(), 0.5)
	require.NotEqual(t, fast[0].owner, fast[1].owner, "each cycle must have an independent lock token")
	require.Equal(t, gelvActiveLeaderLockTTL, fast[0].ttl)
	stopped := make(chan struct{})
	go func() { expiry.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned without draining general lane")
	case <-time.After(25 * time.Millisecond):
	}
	close(cache.generalRelease)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not drain")
	}
	require.Empty(t, cache.heldBy(gelvActiveLeaderLockKey))
	require.Empty(t, cache.heldBy(paymentOrderExpiryLeaderLockKey))
	expiry.Stop()
	expiry.Start() // Stop is idempotent; a stopped service cannot restart.
}

func TestGelvLatencySchedulerLeaderCollisionReleaseAndNoSelfLockout(t *testing.T) {
	cache := &fakeLeaderLockCache{}
	_, _ = cache.TryAcquireLeaderLock(context.Background(), gelvActiveLeaderLockKey, "peer", time.Minute)
	// A losing leader must not even access a database.
	loser := NewPaymentOrderExpiryService(&PaymentService{}, time.Minute)
	loser.SetLeaderLock(cache, nil)
	require.NotPanics(t, loser.runGelvActiveOnce)
	require.Equal(t, "peer", cache.heldBy(gelvActiveLeaderLockKey))
	require.NoError(t, cache.ReleaseLeaderLock(context.Background(), gelvActiveLeaderLockKey, "peer"))
	s, _, _, _ := gelvServiceOrder(t, OrderStatusCompleted)
	winner := NewPaymentOrderExpiryService(s, time.Minute)
	winner.SetLeaderLock(cache, nil)
	for i := 0; i < 3; i++ {
		winner.runGelvActiveOnce()
		require.Empty(t, cache.heldBy(gelvActiveLeaderLockKey))
	}
	// The general lane's lock does not prevent an active cycle.
	_, _ = cache.TryAcquireLeaderLock(context.Background(), paymentOrderExpiryLeaderLockKey, "general", time.Minute)
	winner.runGelvActiveOnce()
	require.Empty(t, cache.heldBy(gelvActiveLeaderLockKey))
	require.Equal(t, "general", cache.heldBy(paymentOrderExpiryLeaderLockKey))
}

func TestGelvLatencySchedulerStopDrainsActiveFulfillmentBeforeLeaderRelease(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	p := &gelvLatencyProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv}}
	p.query = func(context.Context, string) (*payment.QueryOrderResponse, error) {
		calls.Add(1)
		close(started)
		<-release
		return &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: gelvServiceMetadata(o, "wechat_virtual")}, nil
	}
	o = gelvLatencyBind(t, s, o, p)
	cache := &fakeLeaderLockCache{}
	expiry := NewPaymentOrderExpiryService(s, time.Minute)
	expiry.SetLeaderLock(cache, nil)
	expiry.Start()
	<-started
	require.NotEmpty(t, cache.heldBy(gelvActiveLeaderLockKey))
	stopped := make(chan struct{})
	go func() { expiry.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned with active payment in flight")
	case <-time.After(25 * time.Millisecond):
	}
	require.NotEmpty(t, cache.heldBy(gelvActiveLeaderLockKey))
	// Another instance collides while Stop drains the first instance's cycle.
	other := NewPaymentOrderExpiryService(s, time.Minute)
	other.SetLeaderLock(cache, nil)
	other.runGelvActiveOnce()
	require.Equal(t, int64(1), calls.Load())
	close(release)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop failed to drain active cycle")
	}
	require.Empty(t, cache.heldBy(gelvActiveLeaderLockKey))
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Len(t, codes.useCalls, 1)
}

func TestGelvLatencySchedulerConcurrentStartStop(t *testing.T) {
	s, _, _, _ := gelvServiceOrder(t, OrderStatusCompleted)
	for range 10 {
		expiry := NewPaymentOrderExpiryService(s, time.Minute)
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(expiry.Start)
			wg.Go(expiry.Stop)
		}
		wg.Wait()
		expiry.Stop()
	}
}
