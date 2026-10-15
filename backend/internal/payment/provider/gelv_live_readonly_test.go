//go:build integration

package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// Explicitly opt in with a private file path. This test only reads the source's
// events and existing orders; it never creates, pays, refunds or grants credit.
func TestGelvFormalLiveReadOnlyContract(t *testing.T) {
	path := os.Getenv("GELV_PRIVATE_CONFIG_FILE")
	if path == "" {
		t.Skip("private partner config not supplied")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var private struct {
		Origin    string `json:"origin"`
		PartnerID string `json:"partnerId"`
		Secret    string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(raw, &private))
	require.True(t, private.Origin == GelvAPIBase && private.PartnerID == "ritelsi", "unexpected partner origin or ID")
	g, err := NewGelv("", map[string]string{"partnerId": private.PartnerID, "partnerSecret": private.Secret, "productIds": "3,4,5,6,7,8"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	var events struct {
		Events []struct {
			Type    string `json:"type"`
			OrderID string `json:"order_id"`
		} `json:"events"`
		Next int64 `json:"next"`
	}
	require.NoError(t, g.post(ctx, "/api/partner/v1/events", struct {
		After int64 `json:"after"`
	}{0}, &events))
	ids := map[string]bool{}
	queried, refunded := 0, 0
	for _, event := range events.Events {
		if event.Type != "paid" && event.Type != "refund" && event.Type != "channel_verified" {
			continue
		}
		id := strings.TrimPrefix(event.OrderID, "P")
		if ids[id] {
			continue
		}
		ids[id] = true
		require.True(t, gelvOrderIDPattern.MatchString(id), "invalid source order ID")
		order, queryErr := g.QueryOrder(ctx, id)
		require.NoError(t, queryErr)
		queried++
		if order.Status == payment.ProviderStatusRefunded {
			refunded++
		}
		if queried >= 3 {
			break
		}
	}
	require.Greater(t, len(events.Events), 0)
	t.Logf("Signed read-only contract passed: source_events=%d existing_orders=%d refunded=%d; writes=0", len(events.Events), queried, refunded)
}
