package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestChannelResponseKeepsTimestampInstant(t *testing.T) {
	loc := time.FixedZone("server", 8*60*60)
	at := time.Date(2026, 11, 6, 12, 34, 56, 0, loc)
	got := channelToResponse(&service.Channel{CreatedAt: at, UpdatedAt: at})
	parsed, err := time.Parse(time.RFC3339, got.CreatedAt)
	require.NoError(t, err)
	require.True(t, at.Equal(parsed))
	require.Equal(t, "2026-11-06T04:34:56Z", got.CreatedAt)
}

func TestModerationCalendarFilterUsesUserDSTDay(t *testing.T) {
	start, err := parseSafetyPromptDate("2026-11-01", false, "America/New_York")
	require.NoError(t, err)
	end, err := parseSafetyPromptDate("2026-11-01", true, "America/New_York")
	require.NoError(t, err)
	require.Equal(t, 25*time.Hour, end.Add(time.Nanosecond).Sub(start))
	moderation, dateOnly, err := parseContentModerationDate("2026-11-01", "America/New_York")
	require.NoError(t, err)
	require.True(t, dateOnly)
	require.True(t, start.Equal(moderation))
}
