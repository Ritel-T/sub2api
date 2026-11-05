package service

import (
	"context"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ModelDiscoveryGroups returns permitted, active bindings in declaration order.
// Discovery uses current owner permissions and active subscriptions, without
// scheduler probes, billing limits, rates, fallbacks or window maintenance.
func (s *APIKeyService) ModelDiscoveryGroups(ctx context.Context, key *APIKey, accepts func(*APIKey) bool) ([]*APIKey, error) {
	if s.localAuth != nil || s.userRepo == nil || s.groupRepo == nil {
		return nil, infraerrors.ServiceUnavailable("MULTI_GROUP_DISCOVERY_UNAVAILABLE", "Multi-group model discovery is unavailable on this gateway")
	}
	owner, err := s.userRepo.GetByID(ctx, key.UserID)
	if err != nil {
		return nil, err
	}
	if !owner.IsActive() {
		return nil, ErrUserNotActive
	}
	snapshot := *key
	snapshot.User = owner
	out := make([]*APIKey, 0, len(key.ConfiguredGroupIDs()))
	for _, id := range key.ConfiguredGroupIDs() {
		group, err := s.groupRepo.GetByIDLite(ctx, id)
		if errors.Is(err, ErrGroupNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !group.IsActive() {
			continue
		}
		if group.IsSubscriptionType() {
			if s.userSubRepo == nil {
				return nil, infraerrors.ServiceUnavailable("MULTI_GROUP_DISCOVERY_UNAVAILABLE", "Subscription permissions are unavailable")
			}
			sub, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, owner.ID, id)
			if errors.Is(err, ErrSubscriptionNotFound) || errors.Is(err, ErrSubscriptionExpired) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !sub.IsActive() {
				continue
			}
		} else if !owner.CanBindGroup(id, group.IsExclusive) {
			continue
		}
		candidate, err := s.apiKeyWithGroup(ctx, &snapshot, group)
		if err != nil {
			return nil, err
		}
		if accepts == nil || accepts(candidate) {
			out = append(out, candidate)
		}
	}
	return out, nil
}
