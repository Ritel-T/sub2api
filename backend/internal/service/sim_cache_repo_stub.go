package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StubSimCacheRepository is an in-memory stub of SimCacheRepository for service tests.
// [OpusClaw Patch] simulated cache billing
type StubSimCacheRepository struct {
	mu    sync.Mutex
	store map[string]*SimCacheState
}

var _ SimCacheRepository = (*StubSimCacheRepository)(nil)

func NewStubSimCacheRepository() *StubSimCacheRepository {
	return &StubSimCacheRepository{store: make(map[string]*SimCacheState)}
}

func (s *StubSimCacheRepository) stubKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (s *StubSimCacheRepository) GetSessionCacheState(_ context.Context, groupID int64, sessionHash string) (*SimCacheState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.store[s.stubKey(groupID, sessionHash)]
	if !ok {
		return nil, nil
	}
	cp := *state
	return &cp, nil
}

func (s *StubSimCacheRepository) SetSessionCacheState(_ context.Context, groupID int64, sessionHash string, state *SimCacheState, _ time.Duration) error {
	if state == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *state
	s.store[s.stubKey(groupID, sessionHash)] = &cp
	return nil
}

// [OpusClaw Patch] simulated cache resilience
func (s *StubSimCacheRepository) AtomicUpdateSessionCacheState(_ context.Context, groupID int64, sessionHash string, cachedTokenCount int, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.stubKey(groupID, sessionHash)
	existing := s.store[key]
	turnCount := 1
	if existing != nil {
		turnCount = existing.TurnCount + 1
	}
	s.store[key] = &SimCacheState{
		CachedTokenCount: cachedTokenCount,
		TurnCount:        turnCount,
	}
	return nil
}
