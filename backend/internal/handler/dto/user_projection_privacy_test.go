package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Preserve the internal-field boundary formerly buried in API response snapshots.
func TestUserProjectionsOmitInternalSubscriptionAndRedeemFields(t *testing.T) {
	assignedBy := int64(999)
	assignedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	sub := UserSubscriptionFromService(&service.UserSubscription{
		ID: 501, UserID: 1, GroupID: 10, AssignedBy: &assignedBy,
		AssignedAt: assignedAt, Notes: "admin-note", DailyUsageUSD: 1.23,
	})
	raw, err := json.Marshal(sub)
	require.NoError(t, err)
	var subscription map[string]any
	require.NoError(t, json.Unmarshal(raw, &subscription))
	for _, key := range []string{"assigned_by", "assigned_at", "notes"} {
		require.NotContains(t, subscription, key)
	}
	require.Equal(t, 1.23, subscription["daily_usage_usd"])

	redeem := RedeemCodeFromService(&service.RedeemCode{
		ID: 900, Code: "CODE-123", Type: service.RedeemTypeBalance,
		Value: 1.25, Notes: "internal-note",
	})
	raw, err = json.Marshal(redeem)
	require.NoError(t, err)
	var code map[string]any
	require.NoError(t, json.Unmarshal(raw, &code))
	require.NotContains(t, code, "notes")
	require.Equal(t, 1.25, code["value"])
}
