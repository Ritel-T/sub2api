package basispoints

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func parallelAttachmentPlan(t *testing.T) *NativeImages {
	t.Helper()
	var urls []string
	for i := 0; i < 8; i++ {
		picture := image.NewRGBA(image.Rect(0, 0, 2, 3))
		picture.SetRGBA(0, 0, color.RGBA{R: uint8(i), A: 255})
		var data bytes.Buffer
		require.NoError(t, png.Encode(&data, picture))
		urls = append(urls, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(data.Bytes()))
	}
	urls = append(urls, urls[0])
	plan, err := PrepareNativeImages(nativeTestRequest(t, urls...))
	require.NoError(t, err)
	return plan
}

func TestNativeUploadParallelBoundOrderAndDeduplication(t *testing.T) {
	plan := parallelAttachmentPlan(t)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var active, peak, calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := plan.Upload(context.Background(), &AttachmentCache{}, "parallel", func(ctx context.Context, img InlineAttachment) (string, error) {
			calls.Add(1)
			now := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); now > old && !peak.CompareAndSwap(old, now); old = peak.Load() {
			}
			started <- struct{}{}
			select {
			case <-release:
				return fmt.Sprintf("file-%x", img.digest[:8]), nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		})
		done <- err
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("four uploads must start before any upload completes")
		}
	}
	require.Equal(t, int32(4), active.Load())
	unblock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("uploads did not finish")
	}
	require.Equal(t, int32(4), peak.Load())
	require.Equal(t, int32(8), calls.Load(), "duplicate occurrence uploads only once")
	require.Zero(t, active.Load())
	for _, part := range plan.parts {
		require.Equal(t, fmt.Sprintf("file-%x", part.image.digest[:8]), part.part["file_id"], "preserve image order")
	}
}

func TestNativeUploadCancellationWaitsAndLeavesHistoryIntact(t *testing.T) {
	plan := parallelAttachmentPlan(t)
	original, err := plan.Body()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 8)
	done := make(chan error, 1)
	var active atomic.Int32
	go func() {
		result, err := plan.Upload(ctx, &AttachmentCache{}, "cancel", func(ctx context.Context, _ InlineAttachment) (string, error) {
			active.Add(1)
			defer active.Add(-1)
			started <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		})
		if result != nil {
			err = fmt.Errorf("canceled upload returned a partial body")
		}
		done <- err
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("uploads did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop uploads")
	}
	require.Zero(t, active.Load())
	after, err := plan.Body()
	require.NoError(t, err)
	require.Equal(t, original, after)
}
