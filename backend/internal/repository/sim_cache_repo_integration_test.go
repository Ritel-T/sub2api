//go:build integration

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type SimCacheSuite struct {
	IntegrationRedisSuite
	repo service.SimCacheRepository
}

func (s *SimCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.repo = NewSimCacheRepo(s.rdb)
}

func (s *SimCacheSuite) TestGetSessionCacheState_Missing() {
	state, err := s.repo.GetSessionCacheState(s.ctx, 1, "nonexistent")
	require.NoError(s.T(), err)
	require.Nil(s.T(), state, "missing key should return nil state")
}

func (s *SimCacheSuite) TestSetAndGetSessionCacheState() {
	groupID := int64(1)
	sessionHash := "sess-abc"
	want := &service.SimCacheState{CachedTokenCount: 1500, TurnCount: 3}

	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, groupID, sessionHash, want, 5*time.Minute))

	got, err := s.repo.GetSessionCacheState(s.ctx, groupID, sessionHash)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), got)
	require.Equal(s.T(), want.CachedTokenCount, got.CachedTokenCount)
	require.Equal(s.T(), want.TurnCount, got.TurnCount)
}

func (s *SimCacheSuite) TestSetSessionCacheState_NilIsNoop() {
	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, 1, "x", nil, time.Minute))

	got, err := s.repo.GetSessionCacheState(s.ctx, 1, "x")
	require.NoError(s.T(), err)
	require.Nil(s.T(), got)
}

func (s *SimCacheSuite) TestSessionCacheState_TTL() {
	groupID := int64(2)
	sessionHash := "sess-ttl"
	state := &service.SimCacheState{CachedTokenCount: 100, TurnCount: 1}
	ttl := 3 * time.Minute

	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, groupID, sessionHash, state, ttl))

	key := buildSimCacheKey(groupID, sessionHash)
	actual, err := s.rdb.TTL(s.ctx, key).Result()
	require.NoError(s.T(), err)
	s.AssertTTLWithin(actual, 2*time.Second, ttl)
}

func (s *SimCacheSuite) TestSessionCacheState_GroupIsolation() {
	sessionHash := "shared-hash"
	state1 := &service.SimCacheState{CachedTokenCount: 100, TurnCount: 1}
	state2 := &service.SimCacheState{CachedTokenCount: 200, TurnCount: 2}

	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, 1, sessionHash, state1, time.Minute))
	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, 2, sessionHash, state2, time.Minute))

	got1, err := s.repo.GetSessionCacheState(s.ctx, 1, sessionHash)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 100, got1.CachedTokenCount)

	got2, err := s.repo.GetSessionCacheState(s.ctx, 2, sessionHash)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 200, got2.CachedTokenCount)
}

func (s *SimCacheSuite) TestGetSessionCacheState_CorruptedJSON() {
	groupID := int64(3)
	sessionHash := "corrupted"
	key := buildSimCacheKey(groupID, sessionHash)

	require.NoError(s.T(), s.rdb.Set(s.ctx, key, "not-json{", time.Minute).Err())

	state, err := s.repo.GetSessionCacheState(s.ctx, groupID, sessionHash)
	require.Error(s.T(), err, "corrupted JSON should return error")
	require.Nil(s.T(), state)
}

func (s *SimCacheSuite) TestSetSessionCacheState_Overwrite() {
	groupID := int64(4)
	sessionHash := "overwrite"
	state1 := &service.SimCacheState{CachedTokenCount: 100, TurnCount: 1}
	state2 := &service.SimCacheState{CachedTokenCount: 500, TurnCount: 2}

	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, groupID, sessionHash, state1, time.Minute))
	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, groupID, sessionHash, state2, time.Minute))

	got, err := s.repo.GetSessionCacheState(s.ctx, groupID, sessionHash)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 500, got.CachedTokenCount)
	require.Equal(s.T(), 2, got.TurnCount)
}

func (s *SimCacheSuite) TestSessionCacheState_JSONFormat() {
	groupID := int64(5)
	sessionHash := "json-check"
	state := &service.SimCacheState{CachedTokenCount: 999, TurnCount: 7}

	require.NoError(s.T(), s.repo.SetSessionCacheState(s.ctx, groupID, sessionHash, state, time.Minute))

	key := buildSimCacheKey(groupID, sessionHash)
	raw, err := s.rdb.Get(s.ctx, key).Bytes()
	require.NoError(s.T(), err)

	var parsed service.SimCacheState
	require.NoError(s.T(), json.Unmarshal(raw, &parsed))
	require.Equal(s.T(), 999, parsed.CachedTokenCount)
	require.Equal(s.T(), 7, parsed.TurnCount)
}

func TestSimCacheSuite(t *testing.T) {
	suite.Run(t, new(SimCacheSuite))
}

// Verify interface compliance at compile time
var _ service.SimCacheRepository = (*simCacheRepo)(nil)
