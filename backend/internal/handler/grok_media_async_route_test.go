//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAsyncVideoCreationRejectsVariableBillingRoutesBeforeForwarding(t *testing.T) {
	endpoints := []service.GrokMediaEndpoint{
		service.GrokMediaEndpointVideosGenerations,
		service.GrokMediaEndpointVideosEdits,
		service.GrokMediaEndpointVideosExtensions,
		service.SeedanceEndpointCreate,
		service.MiniMaxH3EndpointCreate,
	}
	for _, endpoint := range endpoints {
		for _, route := range []string{"multiple groups", "subscription overflow"} {
			t.Run(string(endpoint)+"/"+route, func(t *testing.T) {
				platform := service.PlatformGrok
				body := `{"model":"grok-imagine-video","prompt":"waves","duration":6}`
				if endpoint.IsSeedance() {
					platform = service.PlatformOpenAI
					body = `{"model":"doubao-seedance","content":[{"type":"text","text":"waves"}]}`
				}
				if endpoint.IsMiniMaxH3() {
					platform = service.PlatformMiniMax
					body = `{"model":"MiniMax-H3","prompt":"waves","seconds":"1"}`
				}
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, platform)
				c, w := grokMediaSlotContext(context.Background(), true)
				key, _ := middleware.GetAPIKeyFromContext(c)
				key.Group.Platform = platform
				key.User.Balance, key.QuotaUsed = 7, 2
				if route == "multiple groups" {
					key.GroupIDs = []int64{24, 25}
					fallbackID := int64(25)
					key.Group.SubscriptionType = service.SubscriptionTypeSubscription
					key.Group.SubscriptionFallbackGroupID = &fallbackID
				}
				if route == "subscription overflow" {
					sourceGroup := *key.Group
					sourceGroup.ID, sourceGroup.SubscriptionType = 25, service.SubscriptionTypeSubscription
					key.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroupID: sourceGroup.ID, TargetGroupID: key.Group.ID, SourceGroup: &sourceGroup}
				}
				resolves := 0
				ctx := service.WithSubscriptionBillingResolver(context.Background(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
					resolves++
					return key, nil, nil
				})
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				h.handleGrokMedia(c, endpoint, "")
				require.Equal(t, 1, resolves, "guard must use the freshly resolved billing route")
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), `"code":"unsupported_async_media_billing_route"`)
				require.Zero(t, upstream.calls)
				require.Zero(t, slots.acquired, "reject before selecting or reserving an upstream account")
				require.Zero(t, bindings.writes)
				require.Empty(t, bindings.pending)
				require.Empty(t, bindings.billed)
				require.Equal(t, 7.0, key.User.Balance)
				require.Equal(t, 2.0, key.QuotaUsed)
				slots.assertReleased(t)
			})
		}
	}
}

func TestAsyncVideoRouteGuardPreservesSynchronousImages(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.GroupIDs = []int64{24, 25}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"grok-imagine-image","prompt":"waves"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	upstream.call = func(*http.Request, int64) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"url":"https://example.test/image.png"}]}`))}, nil
	}
	h.GrokImages(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.NotContains(t, w.Body.String(), "unsupported_async_media_billing_route")
	require.Empty(t, bindings.pending)
	slots.assertReleased(t)
}

func TestAsyncVideoRouteGuardPreservesSingleGroupCreation(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
	c, w := grokMediaSlotContext(context.Background(), true)
	h.GrokVideoGeneration(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Len(t, bindings.pending, 1)
	slots.assertReleased(t)
}

func TestAsyncVideoRouteGuardPreservesExistingTaskLookups(t *testing.T) {
	for _, route := range []string{"multiple groups", "subscription overflow"} {
		t.Run(route, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
			c, w := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			if route == "multiple groups" {
				key.GroupIDs = []int64{24, 25}
			}
			if route == "subscription overflow" {
				sourceGroup := *key.Group
				sourceGroup.ID, sourceGroup.SubscriptionType = 25, service.SubscriptionTypeSubscription
				key.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroupID: sourceGroup.ID, TargetGroupID: key.Group.ID, SourceGroup: &sourceGroup}
			}
			h.GrokVideoStatus(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.Zero(t, bindings.writes, "lookup must retain existing task ownership and expiry")
			require.Empty(t, bindings.pending)
			require.Empty(t, bindings.billed)
			slots.assertReleased(t)
		})
	}
}
