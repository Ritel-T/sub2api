package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchResponseTransformControlsChargeBeforeWrite(t *testing.T) {
	raw := []byte(`{"output":"raw"}`)
	output, calls, err := transformAlphaSearchResponse(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, raw, output)
	require.Equal(t, 1, calls)
	for _, billable := range []bool{false, true} {
		ctx := WithAlphaSearchResponseTransform(context.Background(), func(b []byte) ([]byte, bool, error) {
			require.Equal(t, raw, b)
			return []byte(`{"results":[]}`), billable, nil
		})
		output, calls, err = transformAlphaSearchResponse(ctx, raw)
		require.NoError(t, err)
		require.JSONEq(t, `{"results":[]}`, string(output))
		if billable {
			require.Equal(t, 1, calls)
		} else {
			require.Zero(t, calls)
		}
	}
	ctx := WithAlphaSearchResponseTransform(context.Background(), func([]byte) ([]byte, bool, error) { return nil, true, errors.New("unrecognized upstream result") })
	output, calls, err = transformAlphaSearchResponse(ctx, raw)
	require.Error(t, err)
	require.Nil(t, output)
	require.Zero(t, calls)
}

func TestDeepSeekToolsSchedulingContextRejectsUnavailablePool(t *testing.T) {
	caller := &Group{ID: 17, Platform: PlatformDeepseek}
	ctx := context.WithValue(context.Background(), ctxkey.Group, caller)
	s := &OpenAIGatewayService{}
	selected, err := s.DeepSeekToolsSchedulingContext(ctx, 12)
	require.Error(t, err)
	require.Nil(t, selected)
	require.Same(t, caller, ctx.Value(ctxkey.Group))
}
