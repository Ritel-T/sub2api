package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var weeklyQuotaTestNow = time.Date(2026, 10, 25, 7, 0, 0, 0, time.UTC)

func weeklyQuotaFixture(id int64, plan string, used any) Account {
	return Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"plan_type": plan, "email": "private@example.test", "access_token": "private-token"},
		Extra: map[string]any{
			"codex_7d_used_percent": used, "codex_7d_window_minutes": 10080,
			"codex_7d_reset_at":      weeklyQuotaTestNow.Add(time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": weeklyQuotaTestNow.Add(-time.Minute).Format(time.RFC3339),
		},
	}
}

type weeklyQuotaTestLister struct {
	accounts []Account
	calls    int
	list     func(context.Context, int, int, int) ([]Account, int64, error)
}

func (s *weeklyQuotaTestLister) ListAccounts(ctx context.Context, page, size int, platform, typ, status, search string, group int64, privacy, sort, order string) ([]Account, int64, error) {
	s.calls++
	if platform != PlatformOpenAI || typ != AccountTypeOAuth || status != "" || search != "" || group != 0 || privacy != "" || sort != "id" || order != "asc" || size != openAIWeeklyQuotaPageSize {
		return nil, 0, errors.New("unexpected filters or unstable order")
	}
	if s.list != nil {
		return s.list(ctx, page, size, s.calls)
	}
	start := (page - 1) * size
	end := min(start+size, len(s.accounts))
	if start > len(s.accounts) {
		return nil, int64(len(s.accounts)), nil
	}
	return s.accounts[start:end], int64(len(s.accounts)), nil
}

func TestOpenAIWeeklyQuotaTiersAndRounding(t *testing.T) {
	accounts := []Account{
		weeklyQuotaFixture(1, "plus", 0),
		weeklyQuotaFixture(2, "prolite", 25),
		weeklyQuotaFixture(3, "pro", json.Number("12.3456789")),
		weeklyQuotaFixture(4, "promax", 100),
		weeklyQuotaFixture(5, " PLUS ", "99.123456"),
	}
	accounts[0].Extra["codex_usage_updated_at"] = weeklyQuotaTestNow.Add(-2 * time.Hour).Format(time.RFC3339)
	before := maps.Clone(accounts[0].Extra)
	lister := &weeklyQuotaTestLister{accounts: accounts}
	got, err := CollectOpenAIWeeklyQuota(context.Background(), lister, weeklyQuotaTestNow)
	require.NoError(t, err)
	require.Equal(t, 1, lister.calls)
	require.Equal(t, 5, got.IncludedAccounts)
	require.Equal(t, float64(5880), got.TotalWeeklyLimit)
	require.Equal(t, 1893.3877, got.TotalRemaining)
	require.Equal(t, []OpenAIWeeklyQuotaTier{
		{Plan: "plus", Label: "Plus", WeeklyLimit: 140, AccountCount: 2, Remaining: 141.2272},
		{Plan: "prolite", Label: "Pro100", WeeklyLimit: 700, AccountCount: 1, Remaining: 525},
		{Plan: "pro", Label: "Pro200", WeeklyLimit: 1400, AccountCount: 1, Remaining: 1227.1605},
		{Plan: "promax", Label: "Pro500", WeeklyLimit: 3500, AccountCount: 1, Remaining: 0},
	}, got.Tiers)
	require.Equal(t, weeklyQuotaTestNow, got.GeneratedAt)
	require.Equal(t, weeklyQuotaTestNow.Add(-2*time.Hour), *got.OldestSnapshotAt)
	require.Equal(t, weeklyQuotaTestNow.Add(-time.Minute), *got.LatestSnapshotAt)
	require.Equal(t, before, accounts[0].Extra, "aggregation must not mutate snapshots")
	body, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private")
	require.NotContains(t, string(body), "credentials")
}

func TestOpenAIWeeklyQuotaSnapshotFixtures(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Account)
		reason    string
		remaining int64
	}{
		{"zero", func(a *Account) { a.Extra["codex_7d_used_percent"] = 0 }, "", 1400000},
		{"hundred", func(a *Account) { a.Extra["codex_7d_used_percent"] = 100 }, "", 0},
		{"error disabled", func(a *Account) { a.Status = "error"; a.ErrorMessage = "auth failure" }, "inactive", 0},
		{"unschedulable", func(a *Account) { a.Schedulable = false }, "inactive", 0},
		{"disabled", func(a *Account) { a.Status = "disabled" }, "inactive", 0},
		{"unknown plan", func(a *Account) { a.Credentials["plan_type"] = "business_prolite" }, "unsupported_plan", 0},
		{"free", func(a *Account) { a.Credentials["plan_type"] = "free" }, "unsupported_plan", 0},
		{"no snapshot", func(a *Account) { a.Extra = nil }, "missing_quota", 0},
		{"missing percent", func(a *Account) { delete(a.Extra, "codex_7d_used_percent") }, "missing_quota", 0},
		{"null percent", func(a *Account) { a.Extra["codex_7d_used_percent"] = nil }, "missing_quota", 0},
		{"missing timestamp", func(a *Account) { delete(a.Extra, "codex_usage_updated_at") }, "missing_quota", 0},
		{"bad timestamp", func(a *Account) { a.Extra["codex_usage_updated_at"] = "bad" }, "invalid_quota", 0},
		{"future timestamp", func(a *Account) {
			a.Extra["codex_usage_updated_at"] = weeklyQuotaTestNow.Add(time.Second).Format(time.RFC3339)
		}, "invalid_quota", 0},
		{"missing window", func(a *Account) { delete(a.Extra, "codex_7d_window_minutes") }, "non_weekly_window", 0},
		{"daily", func(a *Account) { a.Extra["codex_7d_window_minutes"] = 1440 }, "non_weekly_window", 0},
		{"monthly", func(a *Account) { a.Extra["codex_7d_window_minutes"] = 43200 }, "non_weekly_window", 0},
		{"almost weekly", func(a *Account) { a.Extra["codex_7d_window_minutes"] = 10080.5 }, "non_weekly_window", 0},
		{"negative", func(a *Account) { a.Extra["codex_7d_used_percent"] = -1 }, "invalid_quota", 0},
		{"over hundred", func(a *Account) { a.Extra["codex_7d_used_percent"] = 100.01 }, "invalid_quota", 0},
		{"nan", func(a *Account) { a.Extra["codex_7d_used_percent"] = math.NaN() }, "invalid_quota", 0},
		{"infinite", func(a *Account) { a.Extra["codex_7d_used_percent"] = math.Inf(1) }, "invalid_quota", 0},
		{"malformed", func(a *Account) { a.Extra["codex_7d_used_percent"] = "broken" }, "invalid_quota", 0},
		{"boolean", func(a *Account) { a.Extra["codex_7d_used_percent"] = true }, "invalid_quota", 0},
		{"missing reset", func(a *Account) { delete(a.Extra, "codex_7d_reset_at") }, "missing_quota", 0},
		{"bad reset", func(a *Account) { a.Extra["codex_7d_reset_at"] = "bad" }, "invalid_quota", 0},
		{"reset too far", func(a *Account) {
			a.Extra["codex_7d_reset_at"] = weeklyQuotaTestNow.Add(8 * 24 * time.Hour).Format(time.RFC3339)
		}, "invalid_quota", 0},
		{"expired", func(a *Account) {
			a.Extra["codex_7d_reset_at"] = weeklyQuotaTestNow.Add(-time.Second).Format(time.RFC3339)
		}, "expired_snapshot", 0},
		{"reset exact now", func(a *Account) { a.Extra["codex_7d_reset_at"] = weeklyQuotaTestNow.Format(time.RFC3339) }, "expired_snapshot", 0},
		{"anchored relative reset", func(a *Account) { delete(a.Extra, "codex_7d_reset_at"); a.Extra["codex_7d_reset_after_seconds"] = 3600 }, "", 700000},
		{"expired relative reset", func(a *Account) { delete(a.Extra, "codex_7d_reset_at"); a.Extra["codex_7d_reset_after_seconds"] = 30 }, "expired_snapshot", 0},
		{"invalid relative reset", func(a *Account) { delete(a.Extra, "codex_7d_reset_at"); a.Extra["codex_7d_reset_after_seconds"] = -1 }, "invalid_quota", 0},
		{"do not reset expired canonical from fresh raw", func(a *Account) {
			a.Extra["codex_7d_reset_at"] = weeklyQuotaTestNow.Add(-time.Second).Format(time.RFC3339)
			a.Extra["codex_primary_window_minutes"] = 10080
			a.Extra["codex_primary_used_percent"] = 0
			a.Extra["codex_primary_reset_after_seconds"] = 3600
		}, "expired_snapshot", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := weeklyQuotaFixture(1, "plus", 50)
			tc.mutate(&a)
			row := evaluateOpenAIWeeklyQuota(&a, weeklyQuotaTestNow)
			require.Equal(t, tc.reason, row.reason)
			require.Equal(t, tc.remaining, row.remaining)
		})
	}
}

func TestOpenAIWeeklyQuotaRawWindowFallback(t *testing.T) {
	for _, weekly := range []string{"primary", "secondary"} {
		t.Run(weekly, func(t *testing.T) {
			a := weeklyQuotaFixture(1, "prolite", 99)
			// Stale daily canonical metadata must never be counted as weekly.
			a.Extra["codex_7d_window_minutes"] = 1440
			other := "primary"
			if weekly == other {
				other = "secondary"
			}
			a.Extra["codex_"+weekly+"_window_minutes"] = json.Number("10080")
			a.Extra["codex_"+weekly+"_used_percent"] = 25
			a.Extra["codex_"+weekly+"_reset_after_seconds"] = 7200
			a.Extra["codex_"+other+"_window_minutes"] = 300
			a.Extra["codex_"+other+"_used_percent"] = 100
			a.Extra["codex_"+other+"_reset_after_seconds"] = 0
			row := evaluateOpenAIWeeklyQuota(&a, weeklyQuotaTestNow)
			require.Empty(t, row.reason)
			require.Equal(t, int64(5250000), row.remaining)
			a.Extra["codex_"+weekly+"_reset_after_seconds"] = 0
			require.Equal(t, "expired_snapshot", evaluateOpenAIWeeklyQuota(&a, weeklyQuotaTestNow).reason)
		})
	}
	a := weeklyQuotaFixture(1, "plus", 50)
	delete(a.Extra, "codex_7d_window_minutes")
	for _, raw := range []string{"primary", "secondary"} {
		a.Extra["codex_"+raw+"_window_minutes"] = 10080
		a.Extra["codex_"+raw+"_used_percent"] = 50
	}
	require.Equal(t, "invalid_quota", evaluateOpenAIWeeklyQuota(&a, weeklyQuotaTestNow).reason)
	// Raw percentages without lengths cannot prove a weekly window.
	a.Extra = map[string]any{"codex_primary_used_percent": 20, "codex_secondary_used_percent": 30}
	require.Equal(t, "non_weekly_window", evaluateOpenAIWeeklyQuota(&a, weeklyQuotaTestNow).reason)
}

func TestOpenAIWeeklyQuotaExcludedCountsAndEmptyContract(t *testing.T) {
	accounts := make([]Account, 6)
	for i := range accounts {
		accounts[i] = weeklyQuotaFixture(int64(i+1), "plus", 50)
	}
	accounts[0].Status = "error"
	accounts[1].Credentials["plan_type"] = "unknown"
	accounts[2].Extra = nil
	accounts[3].Extra["codex_7d_window_minutes"] = 43200
	accounts[4].Extra["codex_7d_used_percent"] = math.NaN()
	accounts[5].Extra["codex_7d_reset_at"] = weeklyQuotaTestNow.Format(time.RFC3339)
	got, err := CollectOpenAIWeeklyQuota(context.Background(), &weeklyQuotaTestLister{accounts: accounts}, weeklyQuotaTestNow)
	require.NoError(t, err)
	require.Equal(t, OpenAIWeeklyQuotaExcluded{1, 1, 1, 1, 1, 1}, got.Excluded)
	require.Zero(t, got.TotalRemaining)
	require.Zero(t, got.TotalWeeklyLimit)
	require.Zero(t, got.IncludedAccounts)
	require.Len(t, got.Tiers, 4)
	require.Nil(t, got.OldestSnapshotAt)
	require.Nil(t, got.LatestSnapshotAt)
	empty, err := CollectOpenAIWeeklyQuota(context.Background(), &weeklyQuotaTestLister{}, weeklyQuotaTestNow)
	require.NoError(t, err)
	require.Len(t, empty.Tiers, 4)
	require.Equal(t, OpenAIWeeklyQuotaExcluded{}, empty.Excluded)
}

func TestOpenAIWeeklyQuotaPaginationAndFailures(t *testing.T) {
	accounts := make([]Account, openAIWeeklyQuotaPageSize+1)
	for i := range accounts {
		accounts[i] = weeklyQuotaFixture(int64(i+1), "plus", 50)
	}
	base := func(page, size int) ([]Account, int64, error) {
		start := (page - 1) * size
		return accounts[start:min(start+size, len(accounts))], int64(len(accounts)), nil
	}
	t.Run("all pages once", func(t *testing.T) {
		l := &weeklyQuotaTestLister{accounts: accounts}
		got, err := CollectOpenAIWeeklyQuota(context.Background(), l, weeklyQuotaTestNow)
		require.NoError(t, err)
		require.Equal(t, 2, l.calls)
		require.Equal(t, len(accounts), got.IncludedAccounts)
		require.Equal(t, float64(len(accounts)*70), got.TotalRemaining)
	})
	t.Run("28 accounts need one list call", func(t *testing.T) {
		l := &weeklyQuotaTestLister{accounts: accounts[:28]}
		got, err := CollectOpenAIWeeklyQuota(context.Background(), l, weeklyQuotaTestNow)
		require.NoError(t, err)
		require.Equal(t, 1, l.calls)
		require.Equal(t, 28, got.IncludedAccounts)
		require.Equal(t, float64(1960), got.TotalRemaining)
	})
	t.Run("natural quota updates preserve snapshots already read", func(t *testing.T) {
		live := make([]Account, len(accounts))
		for i := range live {
			live[i] = weeklyQuotaFixture(int64(i+1), "plus", 50)
		}
		l := &weeklyQuotaTestLister{}
		l.list = func(_ context.Context, page, size, call int) ([]Account, int64, error) {
			if call > 2 {
				return nil, 0, errors.New("unexpected reread")
			}
			if page == 2 {
				// A previously read account changes naturally while the next page loads.
				live[0].Extra["codex_7d_used_percent"] = 100
				live[0].Extra["codex_usage_updated_at"] = weeklyQuotaTestNow.Add(-30 * time.Second).Format(time.RFC3339)
				live[size].Extra["codex_7d_used_percent"] = 25
				live[size].Extra["codex_usage_updated_at"] = weeklyQuotaTestNow.Add(-10 * time.Second).Format(time.RFC3339)
			}
			start := (page - 1) * size
			return live[start:min(start+size, len(live))], int64(len(live)), nil
		}
		got, err := CollectOpenAIWeeklyQuota(context.Background(), l, weeklyQuotaTestNow)
		require.NoError(t, err)
		require.Equal(t, 2, l.calls)
		require.Equal(t, 201, got.IncludedAccounts)
		require.Equal(t, float64(14105), got.TotalRemaining)
		require.Equal(t, weeklyQuotaTestNow.Add(-time.Minute), *got.OldestSnapshotAt)
		require.Equal(t, weeklyQuotaTestNow.Add(-10*time.Second), *got.LatestSnapshotAt)
	})
	for _, mode := range []string{"page failure", "count changed", "duplicate boundary", "short page", "wrong platform", "wrong type", "negative total", "too large"} {
		t.Run(mode, func(t *testing.T) {
			l := &weeklyQuotaTestLister{}
			l.list = func(_ context.Context, page, size, call int) ([]Account, int64, error) {
				items, total, err := base(page, size)
				items = append([]Account(nil), items...)
				switch mode {
				case "page failure":
					if call == 2 {
						return nil, 0, errors.New("failed")
					}
				case "count changed":
					if call == 2 {
						total++
					}
				case "duplicate boundary":
					if page == 2 {
						items[0].ID = accounts[size-1].ID
					}
				case "short page":
					if page == 1 {
						items = items[:len(items)-1]
					}
				case "wrong platform":
					items[0].Platform = PlatformAnthropic
				case "wrong type":
					items[0].Type = AccountTypeAPIKey
				case "negative total":
					total = -1
				case "too large":
					total = openAIWeeklyQuotaPageSize*openAIWeeklyQuotaMaxPages + 1
				}
				return items, total, err
			}
			got, err := CollectOpenAIWeeklyQuota(context.Background(), l, weeklyQuotaTestNow)
			require.Error(t, err, fmt.Sprint(mode))
			require.Nil(t, got, "never expose partial totals")
			require.LessOrEqual(t, l.calls, 2)
		})
	}
	t.Run("canceled before read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		l := &weeklyQuotaTestLister{}
		got, err := CollectOpenAIWeeklyQuota(ctx, l, weeklyQuotaTestNow)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, got)
		require.Zero(t, l.calls)
	})
	t.Run("nil service", func(t *testing.T) {
		got, err := CollectOpenAIWeeklyQuota(context.Background(), nil, weeklyQuotaTestNow)
		require.Error(t, err)
		require.Nil(t, got)
	})
}
