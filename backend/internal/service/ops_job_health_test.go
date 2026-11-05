//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpsJobHealthDeadlines(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	success := now.Add(-40 * time.Minute)
	hb := &OpsJobHeartbeat{LastRunAt: &success, LastSuccessAt: &success}
	rule := opsJobHealthRule{name: "hourly", enabled: true, interval: time.Hour, runtime: 2 * time.Minute, grace: time.Minute}
	out := evaluateOpsJobHealth(now, hb, rule)
	require.Equal(t, "healthy", out.State)
	require.Equal(t, success.Add(63*time.Minute), *out.DeadlineAt)
	require.Equal(t, "healthy", evaluateOpsJobHealth(*out.DeadlineAt, hb, rule).State)
	require.Equal(t, "late", evaluateOpsJobHealth(out.DeadlineAt.Add(time.Nanosecond), hb, rule).State)
	failure := now.Add(-time.Minute)
	hb.LastErrorAt = &failure
	require.Equal(t, "failed", evaluateOpsJobHealth(now, hb, rule).State)
	hb.LastSuccessAt = &now
	require.Equal(t, "healthy", evaluateOpsJobHealth(now, hb, rule).State)
	rule.enabled = false
	require.Equal(t, "disabled", evaluateOpsJobHealth(now, hb, rule).State)
	rule.enabled = true
	require.Equal(t, "pending", evaluateOpsJobHealth(now, nil, rule).State)
	rule.unknown = true
	require.Equal(t, "unknown", evaluateOpsJobHealth(now, hb, rule).State)
}

func TestOpsJobHealthCronTimezoneDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	for _, tt := range []struct {
		name       string
		base, next time.Time
	}{
		{"daily gap", time.Date(2026, 11, 5, 2, 0, 0, 0, loc), time.Date(2026, 11, 6, 2, 0, 0, 0, loc)},
		{"spring DST", time.Date(2026, 3, 7, 2, 0, 0, 0, loc), time.Date(2026, 3, 9, 2, 0, 0, 0, loc)},
		{"fall DST", time.Date(2026, 10, 31, 2, 0, 0, 0, loc), time.Date(2026, 11, 1, 2, 0, 0, 0, loc)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			completion := tt.base.Add(5 * time.Minute)
			hb := &OpsJobHeartbeat{LastRunAt: &tt.base, LastSuccessAt: &completion}
			rule := opsJobHealthRule{name: "cleanup", enabled: true, schedule: "0 2 * * *", location: loc, runtime: 30 * time.Minute, grace: time.Minute}
			out := evaluateOpsJobHealth(completion.Add(time.Hour), hb, rule)
			require.Equal(t, "healthy", out.State)
			require.Equal(t, tt.next.Add(31*time.Minute).UTC(), *out.DeadlineAt)
			require.Equal(t, "late", evaluateOpsJobHealth(tt.next.Add(32*time.Minute), hb, rule).State)
		})
	}
}

type jobHealthSettingsStub struct {
	SettingRepository
	values map[string]string
	err    error
}

func (s *jobHealthSettingsStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return s.values, s.err
}

// Embedded nil repository makes any unexpected write fail this test immediately.
func TestOpsJobHealthConfigurationEvidence(t *testing.T) {
	now := time.Now().UTC()
	success := now.Add(-20 * time.Minute)
	repo := &jobHealthSettingsStub{values: map[string]string{SettingKeyOpsMetricsIntervalSeconds: "1800"}}
	svc := &OpsService{settingRepo: repo}
	find := func(name string) OpsJobHealth {
		for _, h := range svc.evaluateJobHealth(context.Background(), now, []*OpsJobHeartbeat{{JobName: opsMetricsCollectorJobName, LastSuccessAt: &success}}) {
			if h.JobName == name {
				return h
			}
		}
		t.Fatal("missing job")
		return OpsJobHealth{}
	}
	require.Equal(t, "healthy", find(opsMetricsCollectorJobName).State)
	repo.values[SettingKeyOpsAlertRuntimeSettings] = "{bad"
	require.Equal(t, "unknown", find(opsAlertEvaluatorJobName).State)
	require.Equal(t, "healthy", find(opsMetricsCollectorJobName).State)
	repo.values[SettingKeyOpsEmailNotificationConfig] = "null"
	require.Equal(t, "unknown", find(opsScheduledReportJobName).State)
	svc.cfg = &config.Config{Timezone: "invalid/location", Ops: config.OpsConfig{Enabled: true}}
	svc.cleanupHealthConfig = func() (bool, string, *time.Location) { return true, "0 2 * * *", time.UTC }
	require.Equal(t, "unknown", find(opsCleanupJobName).State)
	svc.cfg.Timezone = "UTC"
	require.Equal(t, "pending", find(opsCleanupJobName).State)
	repo.values[SettingKeyOpsAdvancedSettings] = "{bad"
	require.Equal(t, "unknown", find(opsCleanupJobName).State)
	repo.err = errors.New("settings unavailable")
	require.Equal(t, "unknown", find(opsMetricsCollectorJobName).State)
	require.Equal(t, "unknown", find(opsAggDailyJobName).State)
}

func TestDashboardHealthEvidenceAndLegacyWeights(t *testing.T) {
	now := time.Now().UTC()
	good := func() *OpsDashboardOverview {
		return &OpsDashboardOverview{
			RequestCountTotal: 100, RequestCountSLA: 100,
			TTFT:          OpsPercentiles{P99: intPtr(1000), SampleCount: int64Ptr(100), PercentileMethod: "raw_exact"},
			SystemMetrics: &OpsSystemMetricsSnapshot{CreatedAt: now, DBOK: boolPtr(true), RedisOK: boolPtr(true), CPUUsagePercent: float64Ptr(30), MemoryUsagePercent: float64Ptr(40)},
			JobHealth:     []OpsJobHealth{{JobName: "job", State: "healthy", EvaluatedAt: now}},
		}
	}
	for _, tt := range []struct {
		name   string
		mutate func(*OpsDashboardOverview)
		score  int
		status string
	}{
		{"healthy", func(*OpsDashboardOverview) {}, 100, "healthy"},
		{"legacy ttft threshold", func(o *OpsDashboardOverview) { o.TTFT.P99 = intPtr(2000) }, 83, "degraded"},
		{"legacy error threshold", func(o *OpsDashboardOverview) { o.ErrorRate = .1 }, 65, "degraded"},
		{"idle infrastructure assessed", func(o *OpsDashboardOverview) {
			o.RequestCountTotal = 0
			o.RequestCountSLA = 0
			o.SystemMetrics.DBOK = boolPtr(false)
		}, 18, "degraded"},
		{"idle healthy infrastructure", func(o *OpsDashboardOverview) { o.RequestCountTotal = 0; o.RequestCountSLA = 0 }, 30, "unknown"},
		{"stale evidence", func(o *OpsDashboardOverview) { o.SystemMetrics.CreatedAt = now.Add(-time.Hour) }, 79, "unknown"},
		{"metadata missing provisional", func(o *OpsDashboardOverview) { o.TTFT.SampleCount = nil }, 100, "unknown"},
		{"raw zero samples", func(o *OpsDashboardOverview) { o.TTFT.SampleCount = int64Ptr(0) }, 65, "unknown"},
		{"degraded plus missing", func(o *OpsDashboardOverview) { o.TTFT.P99 = nil; o.SystemMetrics.DBOK = boolPtr(false) }, 53, "degraded"},
		{"missing ttft", func(o *OpsDashboardOverview) { o.TTFT.P99 = nil }, 65, "unknown"},
		{"late job same UI state", func(o *OpsDashboardOverview) { o.JobHealth[0].State = "late" }, 91, "degraded"},
		{"disabled job ignored", func(o *OpsDashboardOverview) { o.JobHealth[0].State = "disabled" }, 91, "unknown"},
		{"pending job", func(o *OpsDashboardOverview) { o.JobHealth[0].State = "pending" }, 91, "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ov := good()
			tt.mutate(ov)
			applyDashboardHealth(now, ov)
			require.Equal(t, tt.score, ov.HealthScore)
			require.Equal(t, tt.status, ov.HealthStatus)
			if tt.status == "unknown" || tt.name == "degraded plus missing" {
				require.False(t, ov.HealthScoreAvailable)
			}
			if tt.name == "healthy" {
				require.True(t, ov.HealthScoreAvailable)
			}
			sum := 0.0
			for _, d := range ov.HealthScoreDetails {
				sum += d.Weight
				require.NotEmpty(t, d.Reason)
			}
			require.InDelta(t, 1, sum, .00001)
			raw, err := json.Marshal(ov)
			require.NoError(t, err)
			for _, key := range []string{"health_score_version", "health_score_details", "health_status", "job_health"} {
				require.Contains(t, string(raw), key)
			}
		})
	}
	empty := &OpsDashboardOverview{}
	require.Equal(t, 0, computeDashboardHealthScore(now, empty))
	require.Equal(t, "unknown", empty.HealthStatus)
}
