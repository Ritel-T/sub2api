//go:build unit

package repository

import (
	"testing"
	"time"
)

func TestMaybeReset(t *testing.T) {
	start := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	other := start.AddDate(0, 0, -1)
	cases := []struct {
		name      string
		prevUsage float64
		prevStart *time.Time
		currStart time.Time
		cost      float64
		want      float64
	}{
		{"nil prev start resets", 10, nil, start, 1.5, 1.5},
		{"different start resets", 10, &other, start, 1.5, 1.5},
		{"same start accumulates", 10, &start, start, 1.5, 11.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maybeReset(c.prevUsage, c.prevStart, c.currStart, c.cost); got != c.want {
				t.Errorf("maybeReset = %v, want %v", got, c.want)
			}
		})
	}
}

// TestMonthlyMaybeReset_NilStart 验证 prevStart=nil 时重置。
func TestMonthlyMaybeReset_NilStart(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	usage, start := monthlyMaybeReset(10.0, nil, 1.5, now)
	if usage != 1.5 {
		t.Errorf("usage = %v, want 1.5", usage)
	}
	if !start.Equal(now) {
		t.Errorf("start = %v, want %v", start, now)
	}
}

// TestMonthlyMaybeReset_Expired 验证窗口满 30 天时重置（30 天恰好到期）。
func TestMonthlyMaybeReset_Expired(t *testing.T) {
	windowStart := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	// now = windowStart + 30d（刚好到期）
	now := windowStart.Add(30 * 24 * time.Hour)
	usage, start := monthlyMaybeReset(8.0, &windowStart, 2.0, now)
	if usage != 2.0 {
		t.Errorf("usage = %v, want 2.0 (reset)", usage)
	}
	if !start.Equal(now) {
		t.Errorf("start = %v, want %v (new window)", start, now)
	}
}

// TestMonthlyMaybeReset_CrossMonthBoundary 验证跨自然月时也使用 30 天滚动（不提前重置）。
// 旧行为：5 月 1 日跨月立即重置；新行为：窗口起始 4 月 20 日，5 月 1 日仅过了 11 天，应累加。
func TestMonthlyMaybeReset_CrossMonthBoundary(t *testing.T) {
	windowStart := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	// 5 月 1 日：距起始 11 天，不足 30 天，应累加
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usage, start := monthlyMaybeReset(5.0, &windowStart, 1.0, now)
	if usage != 6.0 {
		t.Errorf("usage = %v, want 6.0 (accumulate, not reset at month boundary)", usage)
	}
	if !start.Equal(windowStart) {
		t.Errorf("start = %v, want %v (preserved)", start, windowStart)
	}
}
