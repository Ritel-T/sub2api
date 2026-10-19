package service

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SubscriptionOverflowEvidence is an immutable, request-local charging decision.
// It is never stored in API-key auth caches. SourceGroup preserves the source
// model policy while routing and pricing use the actual target Group/GroupID.
type SubscriptionOverflowEvidence struct {
	UserID             int64
	BaseRateMultiplier *float64
	SubscriptionID     int64
	SourceGroupID      int64
	TargetGroupID      int64
	Dimension          string
	WindowStart        time.Time
	DailyUsageUSD      float64
	DailyLimitUSD      float64
	WeeklyUsageUSD     float64
	WeeklyLimitUSD     float64
	MonthlyUsageUSD    float64
	MonthlyLimitUSD    float64
	SourceGroup        *Group
	sourceAPIKey       *APIKey
}

type subscriptionBillingResolverContextKey struct{}
type SubscriptionBillingResolver func(context.Context, *APIKey) (*APIKey, *UserSubscription, error)

func WithSubscriptionBillingResolver(ctx context.Context, resolver SubscriptionBillingResolver) context.Context {
	return context.WithValue(ctx, subscriptionBillingResolverContextKey{}, resolver)
}

func SubscriptionBillingResolverFromContext(ctx context.Context) (SubscriptionBillingResolver, bool) {
	if ctx == nil {
		return nil, false
	}
	resolve, ok := ctx.Value(subscriptionBillingResolverContextKey{}).(SubscriptionBillingResolver)
	return resolve, ok && resolve != nil
}

type subscriptionOverflowContextKey struct{}

func WithSubscriptionOverflow(ctx context.Context, evidence *SubscriptionOverflowEvidence) context.Context {
	return context.WithValue(ctx, subscriptionOverflowContextKey{}, evidence)
}

func SubscriptionOverflowFromContext(ctx context.Context) (*SubscriptionOverflowEvidence, bool) {
	if ctx == nil {
		return nil, false
	}
	evidence, ok := ctx.Value(subscriptionOverflowContextKey{}).(*SubscriptionOverflowEvidence)
	return evidence, ok && evidence != nil
}

// SubscriptionOverflowSourceAPIKey returns the original authorization binding,
// not the temporary wallet route, for queue and WebSocket identity checks.
func SubscriptionOverflowSourceAPIKey(key *APIKey) *APIKey {
	if key != nil && key.SubscriptionOverflow != nil {
		if key.SubscriptionOverflow.sourceAPIKey != nil {
			return key.SubscriptionOverflow.sourceAPIKey
		}
		if sourceGroup := key.SubscriptionOverflow.SourceGroup; sourceGroup != nil {
			source := *key
			source.Group = sourceGroup
			sourceID := sourceGroup.ID
			source.GroupID = &sourceID
			source.SubscriptionOverflow = nil
			return &source
		}
	}
	return key
}

func IsSubscriptionQuotaError(err error) bool {
	return errors.Is(err, ErrDailyLimitExceeded) || errors.Is(err, ErrWeeklyLimitExceeded) || errors.Is(err, ErrMonthlyLimitExceeded)
}

// ResolveSubscriptionBilling must run before model routing/account selection.
// Every request begins with its persisted subscription binding. A previous
// overflow decision is re-evaluated so a replenished quota automatically returns
// to the subscription; callers with a bound connection must reconnect on change.
func (s *APIKeyService) ResolveSubscriptionBilling(ctx context.Context, key *APIKey, subscriptions *SubscriptionService) (*APIKey, *UserSubscription, error) {
	if subscriptions == nil && ctx != nil {
		if resolve, ok := ctx.Value(subscriptionBillingResolverContextKey{}).(SubscriptionBillingResolver); ok && resolve != nil {
			return resolve(ctx, key)
		}
	}
	if key == nil || key.User == nil {
		return nil, nil, ErrAPIKeyNotFound
	}
	if s != nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return key, nil, nil
	}
	source := key
	refreshSource := key.SubscriptionOverflow != nil || key.Group != nil && key.Group.IsSubscriptionType() && key.Group.SubscriptionFallbackGroupID != nil
	if refreshSource {
		if s == nil || key.Key == "" || key.Group == nil {
			return nil, nil, ErrBillingServiceUnavailable
		}
		latest, err := s.GetByKeyUncached(ctx, key.Key)
		if err != nil {
			return nil, nil, ErrBillingServiceUnavailable.WithCause(err)
		}
		sourceGroup := key.Group
		if key.SubscriptionOverflow != nil {
			sourceGroup = key.SubscriptionOverflow.SourceGroup
		}
		// The persisted primary is not necessarily the source selected for a
		// multi-group request. Confirm that source is still bound and authorized
		// before refreshing its subscription or projecting onto a wallet group.
		if latest != nil && sourceGroup != nil && len(SubscriptionOverflowSourceAPIKey(key).ConfiguredGroupIDs()) > 1 {
			latest, err = s.PinAPIKeyGroup(ctx, latest, sourceGroup.ID)
			if err != nil {
				return nil, nil, err
			}
		}
		if latest == nil || latest.ID != key.ID || latest.UserID != key.UserID || latest.Group == nil || latest.GroupID == nil || sourceGroup == nil || latest.Group.ID != sourceGroup.ID || *latest.GroupID != sourceGroup.ID || latest.Group.Platform != sourceGroup.Platform || latest.Group.SubscriptionType != sourceGroup.SubscriptionType {
			return nil, nil, infraerrors.ServiceUnavailable("API_KEY_GROUP_CHANGED", "API key configuration changed; please retry")
		}
		source = latest
	}
	if source.Group == nil || !source.Group.IsSubscriptionType() {
		return source, nil, nil
	}
	if source.User == nil {
		return nil, nil, ErrAPIKeyNotFound
	}
	if !source.IsActive() || source.IsExpired() {
		if source.Status == StatusAPIKeyQuotaExhausted {
			return nil, nil, ErrAPIKeyQuotaExhausted
		}
		return nil, nil, ErrAPIKeyExpired
	}
	if source.IsQuotaExhausted() {
		return nil, nil, ErrAPIKeyQuotaExhausted
	}
	if !source.User.IsActive() || !source.Group.IsActive() {
		return nil, nil, infraerrors.Forbidden("SUBSCRIPTION_INVALID", "Subscription user or group is not active")
	}
	if subscriptions == nil {
		return nil, nil, ErrBillingServiceUnavailable
	}
	sub, err := subscriptions.getSubscriptionForBillingRoute(ctx, source.UserID, source.Group)
	if err != nil {
		return nil, nil, err
	}
	if sub == nil {
		return nil, nil, ErrSubscriptionNotFound
	}
	needsMaintenance, limitErr := subscriptions.ValidateAndCheckLimits(sub, source.Group)
	if needsMaintenance {
		sub, err = subscriptions.EnsureWindowMaintenance(ctx, sub)
		if err != nil {
			return nil, nil, ErrBillingServiceUnavailable.WithCause(err)
		}
		if sub == nil {
			return nil, nil, ErrBillingServiceUnavailable
		}
		_, limitErr = subscriptions.ValidateAndCheckLimits(sub, source.Group)
	}
	if limitErr == nil {
		return source, sub, nil
	}
	return s.ResolveSubscriptionFallback(ctx, source, sub, limitErr)
}

// ResolveSubscriptionFallback only handles the three recurring quota errors.
// Missing/expired subscriptions, maintenance failures, key quota/ACL failures,
// and infrastructure errors never activate a wallet route.
func (s *APIKeyService) ResolveSubscriptionFallback(ctx context.Context, key *APIKey, sub *UserSubscription, limitErr error) (*APIKey, *UserSubscription, error) {
	if !IsSubscriptionQuotaError(limitErr) || key == nil || key.Group == nil || !key.Group.IsSubscriptionType() || sub == nil || key.Group.SubscriptionFallbackGroupID == nil {
		return key, sub, limitErr
	}
	if !key.IsActive() || key.IsExpired() {
		return nil, nil, ErrAPIKeyExpired
	}
	if key.IsQuotaExhausted() {
		return nil, nil, ErrAPIKeyQuotaExhausted
	}
	if sub.Status != SubscriptionStatusActive || !sub.ExpiresAt.After(time.Now()) || sub.UserID != key.UserID || sub.GroupID != key.Group.ID {
		return nil, nil, ErrSubscriptionExpired
	}
	targetID := *key.Group.SubscriptionFallbackGroupID
	if targetID <= 0 || targetID == key.Group.ID {
		return nil, nil, infraerrors.Forbidden("SUBSCRIPTION_FALLBACK_INVALID", "Subscription overflow group is not available")
	}
	if s == nil || s.groupRepo == nil || s.userRepo == nil || s.userGroupRateRepo == nil {
		return nil, nil, ErrBillingServiceUnavailable
	}
	target, err := s.groupRepo.GetByID(ctx, targetID)
	if err != nil {
		return nil, nil, ErrBillingServiceUnavailable.WithCause(err)
	}
	if target == nil || target.ID != targetID || !target.IsActive() || target.SubscriptionType != SubscriptionTypeStandard || target.Platform != key.Group.Platform || target.Platform == PlatformComposite {
		return nil, nil, infraerrors.Forbidden("SUBSCRIPTION_FALLBACK_INVALID", "Subscription overflow requires an active standard group on the same platform")
	}
	if key.User == nil {
		return nil, nil, ErrAPIKeyNotFound
	}
	user := *key.User
	if s.userRepo != nil {
		latest, userErr := s.userRepo.GetByID(ctx, key.UserID)
		if userErr != nil {
			return nil, nil, ErrBillingServiceUnavailable.WithCause(userErr)
		}
		if latest == nil || latest.ID != key.UserID || !latest.IsActive() {
			return nil, nil, infraerrors.Forbidden("USER_INACTIVE", "User account is not active")
		}
		user = *latest
	}
	if !user.CanBindGroup(target.ID, target.IsExclusive) {
		return nil, nil, infraerrors.Forbidden("SUBSCRIPTION_FALLBACK_NOT_ALLOWED", "Subscription overflow group is not allowed for this user")
	}
	if math.IsNaN(user.Balance) || math.IsInf(user.Balance, 0) {
		return nil, nil, ErrBillingServiceUnavailable
	}
	if user.Balance <= 0 {
		return nil, nil, ErrInsufficientBalance
	}
	// Freeze the base user/group price before any upstream work. A later
	// repository outage must never replace a negotiated user price with default.
	baseRate := target.RateMultiplier
	customRate, rateErr := s.userGroupRateRepo.GetByUserAndGroup(ctx, user.ID, target.ID)
	if rateErr != nil {
		return nil, nil, ErrBillingServiceUnavailable.WithCause(rateErr)
	}
	if customRate != nil {
		baseRate = *customRate
	}
	if math.IsNaN(baseRate) || math.IsInf(baseRate, 0) || baseRate < 0 {
		return nil, nil, ErrBillingServiceUnavailable
	}
	// Auth snapshots are group-specific. Clone both objects, load target policy,
	// and retain source explicit user denies so overflow cannot bypass them.
	user.UserGroupRPMOverride = nil
	user.UserGroupDeniedModels = append([]string(nil), key.DeniedModelsInGroup()...)
	if s.userGroupRateRepo != nil {
		sourceDenied, sourceErr := s.userGroupRateRepo.GetDeniedModelsByUserAndGroup(ctx, user.ID, key.Group.ID)
		if sourceErr != nil {
			return nil, nil, ErrBillingServiceUnavailable.WithCause(sourceErr)
		}
		user.UserGroupDeniedModels = append(user.UserGroupDeniedModels, sourceDenied...)
		denied, denyErr := s.userGroupRateRepo.GetDeniedModelsByUserAndGroup(ctx, user.ID, target.ID)
		if denyErr != nil {
			return nil, nil, ErrBillingServiceUnavailable.WithCause(denyErr)
		}
		user.UserGroupDeniedModels = append(user.UserGroupDeniedModels, denied...)
		seen := make(map[string]bool, len(user.UserGroupDeniedModels))
		unique := user.UserGroupDeniedModels[:0]
		for _, model := range user.UserGroupDeniedModels {
			if !seen[model] {
				unique = append(unique, model)
				seen[model] = true
			}
		}
		user.UserGroupDeniedModels = unique
		override, rpmErr := s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, user.ID, target.ID)
		if rpmErr != nil {
			return nil, nil, ErrBillingServiceUnavailable.WithCause(rpmErr)
		}
		if override != nil {
			n := *override
			user.UserGroupRPMOverride = &n
		}
	}
	source := *key
	source.SubscriptionOverflow = nil
	evidence := newSubscriptionOverflowEvidence(&source, sub, target.ID, limitErr)
	evidence.UserID = user.ID
	evidence.BaseRateMultiplier = &baseRate
	projected := *key
	projected.GroupID = &targetID
	projected.Group = target
	projected.User = &user
	projected.SubscriptionOverflow = evidence
	return &projected, nil, nil
}

func newSubscriptionOverflowEvidence(key *APIKey, sub *UserSubscription, targetID int64, err error) *SubscriptionOverflowEvidence {
	evidence := &SubscriptionOverflowEvidence{
		SubscriptionID: sub.ID, SourceGroupID: key.Group.ID, TargetGroupID: targetID,
		DailyUsageUSD: sub.DailyUsageUSD, WeeklyUsageUSD: sub.WeeklyUsageUSD, MonthlyUsageUSD: sub.MonthlyUsageUSD,
		SourceGroup: key.Group, sourceAPIKey: key,
	}
	value := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	evidence.DailyLimitUSD = value(key.Group.DailyLimitUSD)
	evidence.WeeklyLimitUSD = value(key.Group.WeeklyLimitUSD)
	evidence.MonthlyLimitUSD = value(key.Group.MonthlyLimitUSD)
	var start *time.Time
	switch {
	case errors.Is(err, ErrDailyLimitExceeded):
		evidence.Dimension, start = "daily", sub.DailyWindowStart
	case errors.Is(err, ErrWeeklyLimitExceeded):
		evidence.Dimension, start = "weekly", sub.WeeklyWindowStart
	case errors.Is(err, ErrMonthlyLimitExceeded):
		evidence.Dimension, start = "monthly", sub.MonthlyWindowStart
	default:
		return nil
	}
	evidence.WindowStart = sub.StartsAt
	if start != nil {
		evidence.WindowStart = *start
	}
	return evidence
}

// Subscription overflow is a charging decision, so configured routes bypass the
// auth L1 subscription cache and begin with the repository source of truth.
func (s *SubscriptionService) getSubscriptionForBillingRoute(ctx context.Context, userID int64, group *Group) (*UserSubscription, error) {
	if group != nil && group.SubscriptionFallbackGroupID != nil {
		if s == nil || s.userSubRepo == nil {
			return nil, ErrBillingServiceUnavailable
		}
		sub, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, userID, group.ID)
		if err != nil {
			return nil, err
		}
		if sub == nil {
			return nil, ErrSubscriptionNotFound
		}
		copy := *sub
		return &copy, nil
	}
	return s.GetActiveSubscription(ctx, userID, group.ID)
}

func (s *SubscriptionService) GetSubscriptionForBillingRoute(ctx context.Context, userID int64, group *Group) (*UserSubscription, error) {
	return s.getSubscriptionForBillingRoute(ctx, userID, group)
}

// subscriptionOverflowBaseRateFromContext is used by request-side estimates
// and profit checks, which carry only user/group IDs rather than the API key.
func subscriptionOverflowBaseRateFromContext(ctx context.Context, userID, groupID int64) (float64, bool) {
	evidence, ok := SubscriptionOverflowFromContext(ctx)
	if !ok || evidence.UserID != userID || evidence.TargetGroupID != groupID || evidence.BaseRateMultiplier == nil {
		return 0, false
	}
	return *evidence.BaseRateMultiplier, true
}

// Workers carry the immutable key snapshot even when detached from request ctx.
func subscriptionOverflowBaseRateFromKey(key *APIKey) (float64, bool) {
	if key == nil || key.GroupID == nil || key.SubscriptionOverflow == nil {
		return 0, false
	}
	evidence := key.SubscriptionOverflow
	if evidence.UserID != key.UserID || evidence.TargetGroupID != *key.GroupID || evidence.BaseRateMultiplier == nil {
		return 0, false
	}
	return *evidence.BaseRateMultiplier, true
}
