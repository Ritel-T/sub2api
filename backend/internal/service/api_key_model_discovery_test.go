//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type discoveryGroupRepo struct {
	multiGroupRepoStub
	err error
}

func (r discoveryGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	if id == 2 && r.err != nil {
		return nil, r.err
	}
	return r.multiGroupRepoStub.GetByIDLite(ctx, id)
}

func TestModelDiscoveryGroupsPermissionsOrderAndErrors(t *testing.T) {
	failure := errors.New("repository unavailable")
	for _, tc := range []struct {
		name    string
		revoked bool
		err     error
		want    []int64
	}{
		{name: "binding_order_and_denied_scope", want: []int64{3, 2, 1}},
		{name: "current_permission_revoked_and_group_inactive", revoked: true, want: []int64{1}},
		{name: "repository_error_after_candidate", err: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups := map[int64]*Group{1: {ID: 1, Status: StatusActive}, 2: {ID: 2, Status: StatusActive, IsExclusive: true}, 3: {ID: 3, Status: StatusActive}}
			owner := &User{ID: 7, Status: StatusActive, AllowedGroups: []int64{2}, Balance: -10}
			if tc.revoked {
				owner.AllowedGroups = nil
				groups[3].Status = StatusDisabled
			}
			stale := &User{ID: 7, Status: StatusActive, AllowedGroups: []int64{2}, UserGroupDeniedModels: []string{"stale"}}
			key := &APIKey{UserID: 7, User: stale, GroupID: &groups[3].ID, Group: groups[3], GroupIDs: []int64{3, 2, 1}}
			svc := &APIKeyService{
				userRepo:          &overflowUserRepo{user: owner},
				groupRepo:         discoveryGroupRepo{multiGroupRepoStub: multiGroupRepoStub{groups: groups}, err: tc.err},
				userGroupRateRepo: multiGroupRateStub{denied: map[int64][]string{1: {"one"}, 2: {"two"}, 3: {"three"}}},
			}
			got, err := svc.ModelDiscoveryGroups(context.Background(), key, nil)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			ids := make([]int64, 0, len(got))
			for _, candidate := range got {
				ids = append(ids, *candidate.GroupID)
				require.NotSame(t, key, candidate)
				require.NotSame(t, owner, candidate.User)
				require.Equal(t, svc.userGroupRateRepo.(multiGroupRateStub).denied[candidate.Group.ID], candidate.DeniedModelsInGroup())
			}
			require.Equal(t, tc.want, ids)
			require.Same(t, stale, key.User)
			require.Equal(t, []string{"stale"}, key.User.UserGroupDeniedModels)
			require.Equal(t, int64(3), *key.GroupID)
		})
	}
}
