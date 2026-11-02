package timezone

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestUserNaturalDayDST(t *testing.T) {
	for _, tc := range []struct {
		date  string
		hours time.Duration
	}{{"2026-03-08", 23}, {"2026-11-01", 25}, {"2026-11-02", 24}} {
		t.Run(tc.date, func(t *testing.T) {
			loc, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)
			now, err := time.ParseInLocation("2006-01-02 15:04", tc.date+" 12:00", loc)
			require.NoError(t, err)
			start, end := localDayRange(now)
			require.Equal(t, tc.hours*time.Hour, end.Sub(start))
			require.Equal(t, 0, start.Hour())
			require.Equal(t, 0, end.Hour())
			require.True(t, start.Before(now) && now.Before(end))
		})
	}
}
func TestUserDayContextSurvivesDetachedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(WithUserDay(context.Background(), "Asia/Shanghai"))
	start, end := DayRange(ctx)
	cancel()
	detached := context.WithoutCancel(ctx)
	gotStart, gotEnd := DayRange(detached)
	require.NoError(t, detached.Err())
	require.True(t, start.Equal(gotStart))
	require.True(t, end.Equal(gotEnd))
	require.Equal(t, "Asia/Shanghai", gotStart.Location().String())
}
func TestUserDayFallbackMatchesServerDay(t *testing.T) {
	wantStart, wantEnd := DayRange(context.Background())
	for _, name := range []string{"", "invalid/timezone"} {
		start, end := DayRange(WithUserDay(context.Background(), name))
		require.True(t, start.Equal(wantStart))
		require.True(t, end.Equal(wantEnd))
	}
}
