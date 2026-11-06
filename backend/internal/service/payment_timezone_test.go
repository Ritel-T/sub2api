package service

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPaymentDailySeriesUsesCallerCalendar(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// UTC Nov 2 00:30 still belongs to Nov 1 in New York, including DST fall back.
	paid := time.Date(2026, 11, 2, 0, 30, 0, 0, time.UTC)
	since := time.Date(2026, 10, 31, 0, 0, 0, 0, loc)
	rows := buildDailySeries([]*dbent.PaymentOrder{{PaidAt: &paid, PayAmount: 10}}, since, 2)
	require.Equal(t, "2026-11-01", rows[0].Date)
	require.Equal(t, 1, rows[0].Count)
	require.Equal(t, 0, rows[1].Count)
}
