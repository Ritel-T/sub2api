package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionFallbackEntityProjection(t *testing.T) {
	targetID := int64(19)
	require.Equal(t, &targetID, groupEntityToService(&dbent.Group{SubscriptionFallbackGroupID: &targetID}).SubscriptionFallbackGroupID)
	require.Nil(t, groupEntityToService(&dbent.Group{}).SubscriptionFallbackGroupID)
}

func TestSubscriptionFallbackAPIKeyAuthProjectionSQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "subscription-overflow@test.local")
	target, err := client.Group.Create().SetName("payg-target").SetPlatform(service.PlatformOpenAI).SetSubscriptionType(service.SubscriptionTypeStandard).Save(ctx)
	require.NoError(t, err)
	source, err := client.Group.Create().SetName("sub-source").SetPlatform(service.PlatformOpenAI).SetSubscriptionType(service.SubscriptionTypeSubscription).SetSubscriptionFallbackGroupID(target.ID).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Key: "sk-subscription-fallback-fixture", Name: "test", GroupID: &source.ID, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, &target.ID, got.Group.SubscriptionFallbackGroupID, "restricted auth SELECT must include the fallback ID")
	_, err = client.Group.UpdateOneID(source.ID).ClearSubscriptionFallbackGroupID().Save(ctx)
	require.NoError(t, err)
	got, err = repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Nil(t, got.Group.SubscriptionFallbackGroupID)
}

func TestSubscriptionFallbackGroupRepositoryCreateUpdateAndClearSQLite(t *testing.T) {
	keyRepo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	target, err := client.Group.Create().SetName("group-repo-payg").SetPlatform(service.PlatformOpenAI).SetSubscriptionType(service.SubscriptionTypeStandard).Save(ctx)
	require.NoError(t, err)
	source := &service.Group{Name: "group-repo-subscription", Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, Status: service.StatusActive, RateMultiplier: 1, SubscriptionFallbackGroupID: &target.ID}
	require.NoError(t, createGroupRecord(ctx, client, source))
	groupRepo := newGroupRepositoryWithSQL(client, keyRepo.sql)
	restored, err := groupRepo.GetByIDLite(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, &target.ID, restored.SubscriptionFallbackGroupID)
	restored.Name = "preserved-on-update"
	require.NoError(t, groupRepo.Update(ctx, restored))
	restored, err = groupRepo.GetByIDLite(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, &target.ID, restored.SubscriptionFallbackGroupID)
	restored.SubscriptionFallbackGroupID = nil
	require.NoError(t, groupRepo.Update(ctx, restored))
	restored, err = groupRepo.GetByIDLite(ctx, source.ID)
	require.NoError(t, err)
	require.Nil(t, restored.SubscriptionFallbackGroupID)
}
