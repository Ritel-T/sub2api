package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type geminiQuotaRefreshRepo struct {
	SettingRepository
	value string
	err   error
}

func (r *geminiQuotaRefreshRepo) GetValue(context.Context, string) (string, error) {
	return r.value, r.err
}

func TestGeminiQuotaPolicyKeepsConfiguredLimitsAfterFailedRefresh(t *testing.T) {
	for _, readErr := range []error{context.Canceled, context.DeadlineExceeded, errors.New("temporary storage failure")} {
		t.Run(readErr.Error(), func(t *testing.T) {
			repo := &geminiQuotaRefreshRepo{value: `{"tiers":{"aistudio_free":{"pro_rpd":73}}}`}
			s := NewGeminiQuotaService(nil, repo)
			first, ok := s.Policy(context.Background()).QuotaForTier(GeminiTierAIStudioFree)
			require.True(t, ok)
			require.Equal(t, int64(73), first.ProRPD)
			expired := time.Now().Add(-2 * geminiQuotaCacheTTL)
			s.cachedAt = expired
			repo.err = readErr
			after, ok := s.Policy(context.Background()).QuotaForTier(GeminiTierAIStudioFree)
			require.True(t, ok)
			require.Equal(t, int64(73), after.ProRPD)
			require.Equal(t, expired, s.cachedAt)
			repo.err = nil
			repo.value = `{"tiers":{"aistudio_free":{"pro_rpd":91}}}`
			recovered, ok := s.Policy(context.Background()).QuotaForTier(GeminiTierAIStudioFree)
			require.True(t, ok)
			require.Equal(t, int64(91), recovered.ProRPD)
		})
	}
}

func TestGeminiQuotaPolicyDoesNotCacheFailedInitialRead(t *testing.T) {
	repo := &geminiQuotaRefreshRepo{err: context.Canceled}
	s := NewGeminiQuotaService(nil, repo)
	fallback, ok := s.Policy(context.Background()).QuotaForTier(GeminiTierAIStudioFree)
	require.True(t, ok)
	require.Equal(t, int64(50), fallback.ProRPD)
	repo.err = nil
	repo.value = `{"tiers":{"aistudio_free":{"pro_rpd":73}}}`
	current, ok := s.Policy(context.Background()).QuotaForTier(GeminiTierAIStudioFree)
	require.True(t, ok)
	require.Equal(t, int64(73), current.ProRPD)
}

func TestGeminiQuotaPolicyMissingSettingClearsOldOverrides(t *testing.T) {
	repo := &geminiQuotaRefreshRepo{value: `{"tiers":{"aistudio_free":{"pro_rpd":73}}}`}
	s := NewGeminiQuotaService(nil, repo)
	s.Policy(context.Background())
	s.cachedAt = time.Now().Add(-2 * geminiQuotaCacheTTL)
	repo.value = ""
	repo.err = ErrSettingNotFound
	current, ok := s.Policy(context.Background()).QuotaForTier(GeminiTierAIStudioFree)
	require.True(t, ok)
	require.Equal(t, int64(50), current.ProRPD)
	require.WithinDuration(t, time.Now(), s.cachedAt, time.Second)
}
