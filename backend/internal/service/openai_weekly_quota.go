package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	openAIWeeklyQuotaPageSize      = 200
	openAIWeeklyQuotaMaxPages      = 100 // At most 20,000 accounts and 100 list calls; no retries.
	openAIWeeklyQuotaWindowMinutes = 10080
	openAIWeeklyQuotaScale         = 10000
)

// OpenAIWeeklyQuotaAccountLister is satisfied by the existing AdminService.
// Its repository list excludes soft-deleted accounts via SoftDeleteMixin.
// Never replace this with a scheduler cache or an upstream usage probe.
type OpenAIWeeklyQuotaAccountLister interface {
	ListAccounts(context.Context, int, int, string, string, string, string, int64, string, string, string) ([]Account, int64, error)
}

type OpenAIWeeklyQuotaTier struct {
	Plan         string  `json:"plan"`
	Label        string  `json:"label"`
	WeeklyLimit  float64 `json:"weekly_limit"`
	AccountCount int     `json:"account_count"`
	Remaining    float64 `json:"remaining"`
}

type OpenAIWeeklyQuotaExcluded struct {
	Inactive        int `json:"inactive"`
	UnsupportedPlan int `json:"unsupported_plan"`
	MissingQuota    int `json:"missing_quota"`
	NonWeeklyWindow int `json:"non_weekly_window"`
	InvalidQuota    int `json:"invalid_quota"`
	ExpiredSnapshot int `json:"expired_snapshot"`
}

// OpenAIWeeklyQuotaSummary contains aggregate cached data only. Monetary values
// are rounded per included account to four decimals, then summed as fixed-point
// integers, so totals equal the displayed tier sums. No account identity escapes.
type OpenAIWeeklyQuotaSummary struct {
	GeneratedAt      time.Time                 `json:"generated_at"`
	TotalRemaining   float64                   `json:"total_remaining"`
	TotalWeeklyLimit float64                   `json:"total_weekly_limit"`
	IncludedAccounts int                       `json:"included_accounts"`
	Tiers            []OpenAIWeeklyQuotaTier   `json:"tiers"`
	Excluded         OpenAIWeeklyQuotaExcluded `json:"excluded"`
	OldestSnapshotAt *time.Time                `json:"oldest_snapshot_at"`
	LatestSnapshotAt *time.Time                `json:"latest_snapshot_at"`
}

func openAIWeeklyQuotaTiers() []OpenAIWeeklyQuotaTier {
	return []OpenAIWeeklyQuotaTier{
		{Plan: "plus", Label: "Plus", WeeklyLimit: 140},
		{Plan: "prolite", Label: "Pro100", WeeklyLimit: 700},
		{Plan: "pro", Label: "Pro200", WeeklyLimit: 1400},
		{Plan: "promax", Label: "Pro500", WeeklyLimit: 3500},
	}
}

type openAIWeeklyQuotaRow struct {
	tier      int
	reason    string
	remaining int64
	snapshot  time.Time
}

// CollectOpenAIWeeklyQuota reads the full OAuth pool, ignoring UI query filters.
// A single bounded scan checks ordered unique IDs, exact page lengths and totals.
// This is an estimate from the persisted snapshots read on each page, not a
// transactional point-in-time database snapshot. Normal quota/timestamp updates
// do not trigger a reread or reject the estimate. Observed pagination failures or
// structural inconsistencies fail closed without partial data.
func CollectOpenAIWeeklyQuota(ctx context.Context, lister OpenAIWeeklyQuotaAccountLister, now time.Time) (*OpenAIWeeklyQuotaSummary, error) {
	if lister == nil {
		return nil, errors.New("weekly quota account service unavailable")
	}
	now = now.UTC()
	rows, err := scanOpenAIWeeklyQuota(ctx, lister, now)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &OpenAIWeeklyQuotaSummary{GeneratedAt: now, Tiers: openAIWeeklyQuotaTiers()}
	tierUnits := make([]int64, len(result.Tiers))
	var totalUnits int64
	for _, row := range rows {
		switch row.reason {
		case "inactive":
			result.Excluded.Inactive++
		case "unsupported_plan":
			result.Excluded.UnsupportedPlan++
		case "missing_quota":
			result.Excluded.MissingQuota++
		case "non_weekly_window":
			result.Excluded.NonWeeklyWindow++
		case "invalid_quota":
			result.Excluded.InvalidQuota++
		case "expired_snapshot":
			result.Excluded.ExpiredSnapshot++
		default:
			tier := &result.Tiers[row.tier]
			tier.AccountCount++
			tierUnits[row.tier] += row.remaining
			totalUnits += row.remaining
			result.TotalWeeklyLimit += tier.WeeklyLimit
			result.IncludedAccounts++
			at := row.snapshot
			if result.OldestSnapshotAt == nil || at.Before(*result.OldestSnapshotAt) {
				result.OldestSnapshotAt = &at
			}
			if result.LatestSnapshotAt == nil || at.After(*result.LatestSnapshotAt) {
				result.LatestSnapshotAt = &at
			}
		}
	}
	for i := range result.Tiers {
		result.Tiers[i].Remaining = float64(tierUnits[i]) / openAIWeeklyQuotaScale
	}
	result.TotalRemaining = float64(totalUnits) / openAIWeeklyQuotaScale
	return result, nil
}

func scanOpenAIWeeklyQuota(ctx context.Context, lister OpenAIWeeklyQuotaAccountLister, now time.Time) ([]openAIWeeklyQuotaRow, error) {
	var rows []openAIWeeklyQuotaRow
	var expectedTotal int64 = -1
	var lastID int64
	for page := 1; page <= openAIWeeklyQuotaMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		accounts, total, err := lister.ListAccounts(ctx, page, openAIWeeklyQuotaPageSize, PlatformOpenAI, AccountTypeOAuth, "", "", 0, "", "id", "asc")
		if err != nil {
			return nil, fmt.Errorf("weekly quota list page %d failed: %w", page, err)
		}
		if total < 0 || total > openAIWeeklyQuotaPageSize*openAIWeeklyQuotaMaxPages {
			return nil, errors.New("weekly quota list exceeds scan bounds")
		}
		if expectedTotal < 0 {
			expectedTotal = total
		}
		if total != expectedTotal {
			return nil, errors.New("weekly quota account count changed during scan")
		}
		want := min(int(total)-len(rows), openAIWeeklyQuotaPageSize)
		if len(accounts) != want {
			return nil, errors.New("weekly quota incomplete or shifted account page")
		}
		for i := range accounts {
			a := &accounts[i]
			if a.ID <= lastID || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth {
				return nil, errors.New("weekly quota unordered or out-of-scope account page")
			}
			lastID = a.ID
			rows = append(rows, evaluateOpenAIWeeklyQuota(a, now))
		}
		if int64(len(rows)) == total {
			return rows, nil
		}
	}
	return nil, errors.New("weekly quota page limit reached")
}

func evaluateOpenAIWeeklyQuota(a *Account, now time.Time) openAIWeeklyQuotaRow {
	row := openAIWeeklyQuotaRow{tier: -1}
	if a.Status != StatusActive || !a.Schedulable {
		row.reason = "inactive"
		return row
	}
	// Exact allowlist: never infer an allowance from a substring such as business_prolite.
	plan := strings.ToLower(strings.TrimSpace(a.GetCredential("plan_type")))
	tiers := openAIWeeklyQuotaTiers()
	for i := range tiers {
		if tiers[i].Plan == plan {
			row.tier = i
			break
		}
	}
	if row.tier < 0 {
		row.reason = "unsupported_plan"
		return row
	}
	used, at, reason := openAIWeeklyQuotaSnapshot(a.Extra, now)
	row.reason = reason
	if reason != "" {
		return row
	}
	row.snapshot = at
	row.remaining = int64(math.Round(tiers[row.tier].WeeklyLimit * (1 - used/100) * openAIWeeklyQuotaScale))
	return row
}

func openAIWeeklyQuotaSnapshot(extra map[string]any, now time.Time) (float64, time.Time, string) {
	fail := func(reason string) (float64, time.Time, string) { return 0, time.Time{}, reason }
	// A canonical 7d name alone is insufficient: Normalize historically also put
	// daily/monthly/unknown long windows under it. Require exactly 10080 minutes.
	window := ""
	if n, ok := openAIWeeklyQuotaNumber(extra["codex_7d_window_minutes"]); ok && n == openAIWeeklyQuotaWindowMinutes {
		if _, exists := extra["codex_7d_used_percent"]; exists {
			window = "7d"
		}
	}
	if window == "" {
		for _, raw := range []string{"primary", "secondary"} {
			n, ok := openAIWeeklyQuotaNumber(extra["codex_"+raw+"_window_minutes"])
			if !ok || n != openAIWeeklyQuotaWindowMinutes {
				continue
			}
			if window != "" {
				return fail("invalid_quota")
			} // Ambiguous duplicate weekly windows.
			window = raw
		}
	}
	if window == "" {
		for _, w := range []string{"7d", "primary", "secondary"} {
			if _, exists := extra["codex_"+w+"_used_percent"]; exists {
				return fail("non_weekly_window")
			}
		}
		return fail("missing_quota")
	}
	prefix := "codex_" + window
	raw, exists := extra[prefix+"_used_percent"]
	if !exists || raw == nil {
		return fail("missing_quota")
	}
	used, ok := openAIWeeklyQuotaNumber(raw)
	if !ok || used < 0 || used > 100 {
		return fail("invalid_quota")
	}
	rawAt, exists := extra["codex_usage_updated_at"]
	if !exists || rawAt == nil {
		return fail("missing_quota")
	}
	at, ok := openAIWeeklyQuotaTime(rawAt)
	if !ok || at.After(now) {
		return fail("invalid_quota")
	}
	var reset time.Time
	if rawReset, exists := extra[prefix+"_reset_at"]; exists && rawReset != nil {
		reset, ok = openAIWeeklyQuotaTime(rawReset)
		if !ok {
			return fail("invalid_quota")
		}
	} else {
		rawAfter, exists := extra[prefix+"_reset_after_seconds"]
		if !exists || rawAfter == nil {
			return fail("missing_quota")
		}
		after, valid := openAIWeeklyQuotaNumber(rawAfter)
		if !valid || after < 0 || after > openAIWeeklyQuotaWindowMinutes*60 {
			return fail("invalid_quota")
		}
		reset = at.Add(time.Duration(after * float64(time.Second)))
	}
	// Once reset has passed, a cached percentage cannot tell us whether a new
	// window started or how much was consumed since. Exclude, never reset to zero
	// used, advance reset, subtract spend, query upstream, or write a new snapshot.
	if !reset.After(now) {
		return fail("expired_snapshot")
	}
	if reset.After(at.Add(openAIWeeklyQuotaWindowMinutes * time.Minute)) {
		return fail("invalid_quota")
	}
	return used, at, ""
}

func openAIWeeklyQuotaTime(raw any) (time.Time, bool) {
	s, ok := raw.(string)
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	return at.UTC(), err == nil && !at.IsZero()
}

func openAIWeeklyQuotaNumber(raw any) (float64, bool) {
	var n float64
	switch v := raw.(type) {
	case float64:
		n = v
	case float32:
		n = float64(v)
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	case int32:
		n = float64(v)
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		n = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		n = parsed
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}
