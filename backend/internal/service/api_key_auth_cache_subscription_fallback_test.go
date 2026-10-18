package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotSubscriptionFallbackRoundtrip(t *testing.T) {
	for _, configured := range []bool{false, true} {
		targetID, groupID := int64(27), int64(9)
		group := &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, Hydrated: true}
		if configured {
			group.SubscriptionFallbackGroupID = &targetID
		}
		key := &APIKey{ID: 3, UserID: 4, Key: "sk-cache-fixture", GroupID: &groupID, Group: group, Status: StatusActive, User: &User{ID: 4, Status: StatusActive}}
		svc := &APIKeyService{}
		snapshot := svc.snapshotFromAPIKey(context.Background(), key)
		payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
		require.NoError(t, err)
		var entry APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(payload, &entry))
		restored, used, err := svc.applyAuthCacheEntry(key.Key, &entry)
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, group.SubscriptionFallbackGroupID, restored.Group.SubscriptionFallbackGroupID)
		entry.Snapshot.Version = 27
		_, used, err = svc.applyAuthCacheEntry(key.Key, &entry)
		require.NoError(t, err)
		require.False(t, used, "pre-overflow cache entries must reload the source group")
	}
}
