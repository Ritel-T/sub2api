package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperSubscriptionFallback(t *testing.T) {
	id := int64(19)
	group := &service.Group{ID: 1, SubscriptionFallbackGroupID: &id}
	got := GroupFromServiceAdmin(group)
	require.Equal(t, &id, got.SubscriptionFallbackGroupID)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"subscription_fallback_group_id":19`)
	group.SubscriptionFallbackGroupID = nil
	require.Nil(t, GroupFromServiceAdmin(group).SubscriptionFallbackGroupID)
}
