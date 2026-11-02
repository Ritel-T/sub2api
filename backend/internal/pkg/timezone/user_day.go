package timezone

import (
	"context"
	"time"
)

type userDayKey struct{}
type userDayRange struct{ start, end time.Time }

// WithUserDay fixes the caller's natural day for all queries and cache operations
// in this request. Empty or invalid names retain the existing server fallback.
func WithUserDay(ctx context.Context, userTZ string) context.Context {
	start, end := localDayRange(NowInUserLocation(userTZ))
	return context.WithValue(ctx, userDayKey{}, userDayRange{start: start, end: end})
}

// DayRange returns a half-open natural-day interval. Calendar addition retains
// 23/25-hour days at daylight-saving transitions.
func DayRange(ctx context.Context) (time.Time, time.Time) {
	if day, ok := ctx.Value(userDayKey{}).(userDayRange); ok {
		return day.start, day.end
	}
	return localDayRange(Now())
}

func localDayRange(now time.Time) (time.Time, time.Time) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return start, start.AddDate(0, 0, 1)
}
