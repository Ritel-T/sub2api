package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotCacheContextSingleflightAndWaiterCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newSnapshotCache(time.Second)
		release := make(chan struct{})
		var calls atomic.Int32
		load := func() (any, error) { calls.Add(1); <-release; return "exact", nil }
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				e, _, err := c.GetOrLoadContext(context.Background(), "same", load)
				if err != nil || e.Payload != "exact" {
					t.Errorf("result %v, %v", e, err)
				}
			})
		}
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, _, err := c.GetOrLoadContext(ctx, "same", load); done <- err }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Equal(t, int32(1), calls.Load())
		close(release)
		wg.Wait()
		_, hit, err := c.GetOrLoadContext(context.Background(), "same", load)
		require.NoError(t, err)
		require.True(t, hit)
		require.Equal(t, int32(1), calls.Load())
	})
}

func TestSnapshotCacheContextFailureAndLeaderCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newSnapshotCache(time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		release := make(chan struct{})
		done := make(chan error, 2)
		go func() {
			_, _, err := c.GetOrLoadContext(ctx, "same", func() (any, error) {
				<-release
				return "success despite cancellation", nil
			})
			done <- err
		}()
		synctest.Wait()
		go func() {
			_, _, err := c.GetOrLoadContext(context.Background(), "same", func() (any, error) { panic("must join") })
			done <- err
		}()
		synctest.Wait()
		cancel()
		close(release)
		require.ErrorIs(t, <-done, context.Canceled)
		require.ErrorIs(t, <-done, context.Canceled)
		_, ok := c.Get("same")
		require.False(t, ok)
		failure := errors.New("database failed")
		_, _, err := c.GetOrLoadContext(context.Background(), "same", func() (any, error) { return nil, failure })
		require.ErrorIs(t, err, failure)
		_, ok = c.Get("same")
		require.False(t, ok)
		e, hit, err := c.GetOrLoadContext(context.Background(), "same", func() (any, error) { return "recovered", nil })
		require.NoError(t, err)
		require.False(t, hit)
		require.Equal(t, "recovered", e.Payload)
		_, _, err = c.GetOrLoadContext(ctx, "same", func() (any, error) { panic("canceled hit") })
		require.ErrorIs(t, err, context.Canceled)
		_, _, err = c.GetOrLoadContext(ctx, "missing", func() (any, error) { panic("canceled miss") })
		require.ErrorIs(t, err, context.Canceled)
	})
}

type cancelOnMarshal struct{ cancel context.CancelFunc }

func (v cancelOnMarshal) MarshalJSON() ([]byte, error) { v.cancel(); return []byte(`"value"`), nil }

func TestSnapshotCacheContextCanceledDuringPublication(t *testing.T) {
	c := newSnapshotCache(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, err := c.GetOrLoadContext(ctx, "key", func() (any, error) { return cancelOnMarshal{cancel}, nil })
	require.ErrorIs(t, err, context.Canceled)
	_, ok := c.Get("key")
	require.False(t, ok)
}

func TestSnapshotCacheContextExpiryAndBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newSnapshotCache(30 * time.Second)
		calls := 0
		load := func() (any, error) { calls++; return "value", nil }
		for _, wantHit := range []bool{false, true, false} {
			_, hit, err := c.GetOrLoadContext(context.Background(), "key", load)
			require.NoError(t, err)
			require.Equal(t, wantHit, hit)
			if wantHit {
				time.Sleep(31 * time.Second)
			}
		}
		require.Equal(t, 2, calls)
		for i := range 300 {
			_, _, err := c.GetOrLoadContext(context.Background(), fmt.Sprint(i), load)
			require.NoError(t, err)
		}
		require.Len(t, c.items, 256)
		require.LessOrEqual(t, c.cost, 16<<20)
		c.maxCost = 32
		_, _, err := c.GetOrLoadContext(context.Background(), "too-large", func() (any, error) { return strings.Repeat("x", 33), nil })
		require.NoError(t, err)
		_, ok := c.Get("too-large")
		require.False(t, ok)
	})
}
