package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type todayStatsWindowRepoStub struct {
	usageBatchLogRepoStub
	today    map[int64]*usagestats.AccountStats
	lifetime map[int64]*usagestats.AccountStats
}

func (r *todayStatsWindowRepoStub) GetAccountTodayStats(_ context.Context, accountID int64) (*usagestats.AccountStats, error) {
	if stats, ok := r.today[accountID]; ok {
		return stats, nil
	}
	return &usagestats.AccountStats{}, nil
}

func (r *todayStatsWindowRepoStub) GetAccountWindowStats(_ context.Context, accountID int64, startTime time.Time) (*usagestats.AccountStats, error) {
	source := r.today
	if startTime.IsZero() {
		source = r.lifetime
	}
	if stats, ok := source[accountID]; ok {
		return stats, nil
	}
	return &usagestats.AccountStats{}, nil
}

func (r *todayStatsWindowRepoStub) GetAccountWindowStatsBatch(_ context.Context, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error) {
	out := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, id := range accountIDs {
		stats, err := r.GetAccountWindowStats(context.Background(), id, startTime)
		if err != nil {
			return nil, err
		}
		out[id] = stats
	}
	return out, nil
}

func TestGetTodayStatsIncludesLifetimeTotals(t *testing.T) {
	todayEquivalent, lifetimeEquivalent := 2.5, 2345.67
	t.Parallel()
	repo := &todayStatsWindowRepoStub{
		today: map[int64]*usagestats.AccountStats{
			3: {Requests: 10, Tokens: 1000, Cost: 1.25, StandardCost: 1.25, UserCost: 1.25, APIEquivalentCost: &todayEquivalent, APIEquivalentUnpricedRequests: 1},
		},
		lifetime: map[int64]*usagestats.AccountStats{
			3: {Requests: 50, Tokens: 800000000, Cost: 1904.56, StandardCost: 1904.56, UserCost: 1904.56, APIEquivalentCost: &lifetimeEquivalent, APIEquivalentUnpricedRequests: 2},
		},
	}
	svc := &AccountUsageService{usageLogRepo: repo}

	got, err := svc.GetTodayStats(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetTodayStats: %v", err)
	}
	if got.Tokens != 1000 || got.Cost != 1.25 {
		t.Fatalf("today stats = tokens %d cost %v, want 1000 / 1.25", got.Tokens, got.Cost)
	}
	if got.APIEquivalentCost == nil || *got.APIEquivalentCost != todayEquivalent || got.APIEquivalentUnpricedRequests != 1 || got.LifetimeAPIEquivalentCost == nil || *got.LifetimeAPIEquivalentCost != lifetimeEquivalent || got.LifetimeAPIEquivalentUnpricedRequests != 2 {
		t.Fatalf("API equivalent fields not propagated: %+v", got)
	}
	if got.LifetimeTokens != 800000000 || got.LifetimeCost != 1904.56 {
		t.Fatalf("lifetime stats = tokens %d cost %v, want 800000000 / 1904.56", got.LifetimeTokens, got.LifetimeCost)
	}
}

func TestGetTodayStatsBatchIncludesLifetimeTotals(t *testing.T) {
	todayEquivalent, lifetimeEquivalent := 2.5, 11.5
	t.Parallel()
	repo := &todayStatsWindowRepoStub{
		today: map[int64]*usagestats.AccountStats{
			3: {Tokens: 200, Cost: 2, APIEquivalentCost: &todayEquivalent, APIEquivalentUnpricedRequests: 1},
			8: {Tokens: 0, Cost: 0},
		},
		lifetime: map[int64]*usagestats.AccountStats{
			3: {Tokens: 900, Cost: 9.5, APIEquivalentCost: &lifetimeEquivalent, APIEquivalentUnpricedRequests: 3},
			8: {Tokens: 12, Cost: 0.4},
		},
	}
	svc := &AccountUsageService{usageLogRepo: repo}

	got, err := svc.GetTodayStatsBatch(context.Background(), []int64{3, 8, 3})
	if err != nil {
		t.Fatalf("GetTodayStatsBatch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2", len(got))
	}
	if got[3].APIEquivalentCost == nil || *got[3].APIEquivalentCost != todayEquivalent || got[3].APIEquivalentUnpricedRequests != 1 || got[3].LifetimeAPIEquivalentCost == nil || *got[3].LifetimeAPIEquivalentCost != lifetimeEquivalent || got[3].LifetimeAPIEquivalentUnpricedRequests != 3 {
		t.Fatalf("API equivalent fields not propagated: %+v", got[3])
	}
	if got[3].Tokens != 200 || got[3].LifetimeTokens != 900 || got[3].LifetimeCost != 9.5 {
		t.Fatalf("account 3 = %+v", got[3])
	}
	if got[8].LifetimeTokens != 12 || got[8].LifetimeCost != 0.4 {
		t.Fatalf("account 8 = %+v", got[8])
	}
}
