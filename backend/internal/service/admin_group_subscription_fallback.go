package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// resolveSubscriptionFallbackGroup merges a partial admin edit and validates only
// this feature's independent routing graph. It never follows either Claude fallback.
func (s *adminServiceImpl) resolveSubscriptionFallbackGroup(ctx context.Context, sourceID int64, platform, subscriptionType string, current, requested *int64) (*int64, error) {
	targetID := current
	if requested != nil {
		if *requested < 0 {
			return nil, infraerrors.BadRequest("INVALID_SUBSCRIPTION_FALLBACK_GROUP", "subscription_fallback_group_id must be non-negative")
		}
		if *requested == 0 {
			return nil, nil
		}
		targetID = requested
	}
	if targetID == nil {
		return nil, nil
	}
	invalid := func(message string) (*int64, error) {
		return nil, infraerrors.BadRequest("INVALID_SUBSCRIPTION_FALLBACK_GROUP", message)
	}
	if subscriptionType != SubscriptionTypeSubscription {
		return invalid("only subscription groups can configure a subscription fallback")
	}
	if platform == PlatformComposite {
		return invalid("composite groups cannot configure a subscription fallback")
	}
	if *targetID <= 0 {
		return invalid("subscription fallback group must have a positive ID")
	}
	visited := make(map[int64]struct{})
	next := *targetID
	for {
		if sourceID > 0 && next == sourceID {
			return invalid("subscription fallback cannot refer to itself or create a cycle")
		}
		if _, exists := visited[next]; exists {
			return invalid("subscription fallback group cycle detected")
		}
		visited[next] = struct{}{}
		target, err := s.groupRepo.GetByIDLite(ctx, next)
		if err != nil || target == nil {
			return invalid("subscription fallback group not found")
		}
		if target.Platform != platform {
			return invalid("subscription fallback group must use the same platform")
		}
		if target.Status != StatusActive || target.SubscriptionType != SubscriptionTypeStandard {
			return invalid("subscription fallback group must be active and standard")
		}
		// A standard target cannot normally own this field. Walk any stale data
		// defensively so an imported/corrupt graph cannot introduce a cycle.
		if target.SubscriptionFallbackGroupID == nil {
			id := *targetID
			return &id, nil
		}
		next = *target.SubscriptionFallbackGroupID
		if next <= 0 {
			return invalid("subscription fallback chain has an invalid group ID")
		}
	}
}
