//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func subscriptionFallbackID(id int64) *int64 { return &id }

func TestAdminSubscriptionFallbackValidation(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   int64
		sourceType string
		requested  *int64
		target     *Group
		additional map[int64]*Group
		wantError  string
	}{
		{name: "omitted standard has no routing", sourceType: SubscriptionTypeStandard},
		{name: "zero clears without looking up target", sourceType: SubscriptionTypeStandard, requested: subscriptionFallbackID(0)},
		{name: "negative rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(-1), wantError: "non-negative"},
		{name: "standard source rejected", sourceType: SubscriptionTypeStandard, requested: subscriptionFallbackID(2), wantError: "only subscription"},
		{name: "self rejected", sourceID: 2, sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), wantError: "itself"},
		{name: "missing target rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), wantError: "not found"},
		{name: "different platform rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformGemini, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}, wantError: "same platform"},
		{name: "disabled target rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusDisabled, SubscriptionType: SubscriptionTypeStandard}, wantError: "active and standard"},
		{name: "subscription target rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}, wantError: "active and standard"},
		{name: "exclusive standard allowed", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true}},
		{name: "Claude fallbacks not reused", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, FallbackGroupID: subscriptionFallbackID(2), FallbackGroupIDOnInvalidRequest: subscriptionFallbackID(2)}},
		{name: "corrupt target self loop rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SubscriptionFallbackGroupID: subscriptionFallbackID(2)}, wantError: "cycle"},
		{name: "corrupt two group loop rejected", sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SubscriptionFallbackGroupID: subscriptionFallbackID(3)}, additional: map[int64]*Group{3: {ID: 3, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SubscriptionFallbackGroupID: subscriptionFallbackID(2)}}, wantError: "cycle"},
		{name: "cycle through source rejected", sourceID: 1, sourceType: SubscriptionTypeSubscription, requested: subscriptionFallbackID(2), target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, SubscriptionFallbackGroupID: subscriptionFallbackID(1)}, wantError: "cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := map[int64]*Group{}
			if tt.target != nil {
				groups[tt.target.ID] = tt.target
			}
			for id, group := range tt.additional {
				groups[id] = group
			}
			svc := &adminServiceImpl{groupRepo: &groupRepoStubForAdmin{getByIDByID: groups}}
			got, err := svc.resolveSubscriptionFallbackGroup(context.Background(), tt.sourceID, PlatformOpenAI, tt.sourceType, nil, tt.requested)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tt.requested == nil || *tt.requested == 0 {
				require.Nil(t, got)
			} else {
				require.Equal(t, tt.requested, got)
			}
		})
	}
}

func TestAdminSubscriptionFallbackCreateAndPartialUpdate(t *testing.T) {
	ctx := context.Background()
	target := &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true}
	repo := &groupRepoStubForAdmin{createID: 1, getByIDByID: map[int64]*Group{2: target}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{groupRepo: repo, authCacheInvalidator: invalidator}
	created, err := svc.CreateGroup(ctx, &CreateGroupInput{Name: "subscription", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1, SubscriptionFallbackGroupID: subscriptionFallbackID(2)})
	require.NoError(t, err)
	require.Equal(t, subscriptionFallbackID(2), created.SubscriptionFallbackGroupID)
	repo.getByIDByID[1] = created
	updated, err := svc.UpdateGroup(ctx, 1, &UpdateGroupInput{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, subscriptionFallbackID(2), updated.SubscriptionFallbackGroupID, "omission preserves destination")
	require.Equal(t, []int64{1}, invalidator.groupIDs)
	updated, err = svc.UpdateGroup(ctx, 1, &UpdateGroupInput{SubscriptionType: SubscriptionTypeStandard, SubscriptionFallbackGroupID: subscriptionFallbackID(0)})
	require.NoError(t, err)
	require.Nil(t, updated.SubscriptionFallbackGroupID, "zero clears in same edit as source type change")
}

func TestAdminSubscriptionFallbackRejectsChangedSourceAndUnavailableTarget(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  UpdateGroupInput
		target *Group
		want   string
	}{
		{name: "source type", input: UpdateGroupInput{SubscriptionType: SubscriptionTypeStandard}, want: "only subscription"},
		{name: "source platform", input: UpdateGroupInput{Platform: PlatformGemini}, want: "same platform"},
		{name: "target disabled", target: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusDisabled, SubscriptionType: SubscriptionTypeStandard}, want: "active and standard"},
		{name: "target deleted", want: "not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := &Group{ID: 1, Name: "subscription", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, SubscriptionFallbackGroupID: subscriptionFallbackID(2)}
			groups := map[int64]*Group{1: source}
			if tt.target != nil {
				groups[2] = tt.target
			} else if tt.name != "target deleted" {
				groups[2] = &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
			}
			repo := &groupRepoStubForAdmin{getByIDByID: groups}
			_, err := (&adminServiceImpl{groupRepo: repo}).UpdateGroup(context.Background(), 1, &tt.input)
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, repo.updated, "invalid partial edits must not persist")
		})
	}
}

func TestAdminSubscriptionFallbackDuplicateOwnsIndependentPointer(t *testing.T) {
	source := &Group{ID: 1, SubscriptionFallbackGroupID: subscriptionFallbackID(2)}
	cloned := cloneGroupForDuplicate(source, "subscription-fallback-test")
	require.Equal(t, source.SubscriptionFallbackGroupID, cloned.SubscriptionFallbackGroupID)
	*cloned.SubscriptionFallbackGroupID = 3
	require.Equal(t, int64(2), *source.SubscriptionFallbackGroupID)
}

func TestAdminSubscriptionFallbackRejectsCompositeButAllowsClearing(t *testing.T) {
	id := int64(2)
	svc := &adminServiceImpl{groupRepo: &groupRepoStubForAdmin{}}
	_, err := svc.resolveSubscriptionFallbackGroup(context.Background(), 1, PlatformComposite, SubscriptionTypeSubscription, nil, &id)
	require.ErrorContains(t, err, "composite")
	zero := int64(0)
	got, err := svc.resolveSubscriptionFallbackGroup(context.Background(), 1, PlatformComposite, SubscriptionTypeSubscription, &id, &zero)
	require.NoError(t, err)
	require.Nil(t, got, "a disabled unsupported configuration can always be cleared")
}
