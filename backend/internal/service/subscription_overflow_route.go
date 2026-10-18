package service

import (
	"context"
	"sync"
)

// SubscriptionBillingRouteState fences the only safe HTTP route-change window:
// user-slot admission before group policy and account selection. Every later
// queue/turn must retain the decided route or require a fresh request.
type SubscriptionBillingRouteState struct {
	mu        sync.RWMutex
	key       *APIKey
	resolving bool
}
type subscriptionBillingRouteStateKey struct{}

func WithSubscriptionBillingRouteState(ctx context.Context, key *APIKey) context.Context {
	return context.WithValue(ctx, subscriptionBillingRouteStateKey{}, &SubscriptionBillingRouteState{key: key})
}
func SubscriptionBillingRouteStateFromContext(ctx context.Context) *SubscriptionBillingRouteState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(subscriptionBillingRouteStateKey{}).(*SubscriptionBillingRouteState)
	return state
}
func (s *SubscriptionBillingRouteState) Begin() func() {
	if s == nil {
		return func() {}
	}
	s.mu.Lock()
	s.resolving = true
	s.mu.Unlock()
	return func() { s.mu.Lock(); s.resolving = false; s.mu.Unlock() }
}
func (s *SubscriptionBillingRouteState) Set(key *APIKey) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.key = key
	s.mu.Unlock()
}
func (s *SubscriptionBillingRouteState) Freeze(key *APIKey) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.key = key
	s.resolving = false
	s.mu.Unlock()
}
func (s *SubscriptionBillingRouteState) Snapshot() (*APIKey, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.key, s.resolving
}
