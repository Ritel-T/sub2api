package service

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Heartbeats are completion records, not proof that a job is currently running.
type OpsJobHealth struct {
	JobName     string     `json:"job_name"`
	State       string     `json:"state"`
	Reason      string     `json:"reason"`
	DeadlineAt  *time.Time `json:"deadline_at,omitempty"`
	EvaluatedAt time.Time  `json:"evaluated_at"`
}

type opsJobHealthRule struct {
	name     string
	enabled  bool
	interval time.Duration
	schedule string
	runtime  time.Duration
	grace    time.Duration
	location *time.Location
	unknown  bool
}

func evaluateOpsJobHealth(now time.Time, hb *OpsJobHeartbeat, rule opsJobHealthRule) OpsJobHealth {
	out := OpsJobHealth{JobName: rule.name, State: "unknown", Reason: "schedule_unavailable", EvaluatedAt: now}
	if rule.unknown {
		return out
	}
	if !rule.enabled {
		out.State = "disabled"
		out.Reason = "disabled_by_configuration"
		return out
	}
	if rule.schedule != "" {
		if _, err := opsCleanupCronParser.Parse(rule.schedule); err != nil {
			out.Reason = "invalid_schedule"
			return out
		}
	}
	if hb == nil || (hb.LastSuccessAt == nil && hb.LastErrorAt == nil) {
		out.State = "pending"
		out.Reason = "no_completion_observed"
		return out
	}
	if hb.LastSuccessAt != nil && hb.LastSuccessAt.After(now) || hb.LastErrorAt != nil && hb.LastErrorAt.After(now) {
		out.Reason = "completion_in_future"
		return out
	}
	// A failure wins ties; a later successful completion recovers it.
	if hb.LastErrorAt != nil && (hb.LastSuccessAt == nil || !hb.LastErrorAt.Before(*hb.LastSuccessAt)) {
		out.State = "failed"
		out.Reason = "last_execution_failed"
		return out
	}
	base := *hb.LastSuccessAt
	next := base.Add(rule.interval)
	if rule.schedule != "" {
		sched, err := opsCleanupCronParser.Parse(rule.schedule)
		if err != nil {
			out.Reason = "invalid_schedule"
			return out
		}
		if rule.location == nil {
			rule.location = time.Local
		}
		// Cron is anchored at run start so a long run cannot hide a missed firing.
		if hb.LastRunAt != nil {
			base = *hb.LastRunAt
		}
		next = sched.Next(base.In(rule.location))
	}
	if next.IsZero() || (rule.interval <= 0 && rule.schedule == "") {
		return out
	}
	deadline := next.Add(rule.runtime + rule.grace).UTC()
	out.DeadlineAt = &deadline
	out.State = "healthy"
	out.Reason = "within_completion_deadline"
	if now.After(deadline) {
		out.State = "late"
		out.Reason = "completion_deadline_exceeded"
	}
	return out
}

func (s *OpsService) evaluateJobHealth(ctx context.Context, now time.Time, heartbeats []*OpsJobHeartbeat) []OpsJobHealth {
	enabled := s.IsMonitoringEnabled(ctx)
	loc := time.Local
	interval := opsMetricsCollectorMinInterval
	alertInterval := time.Minute
	reportsEnabled := false
	configUnknown := false
	alertUnknown, reportUnknown, timezoneUnknown, collectorUnknown, cleanupConfigUnknown := false, false, false, false, false
	if s.cfg != nil && strings.TrimSpace(s.cfg.Timezone) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err != nil {
			timezoneUnknown = true
		}
	}
	if s.settingRepo != nil {
		values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyOpsAdvancedSettings, SettingKeyOpsMonitoringEnabled, SettingKeyOpsMetricsIntervalSeconds, SettingKeyOpsAlertRuntimeSettings, SettingKeyOpsEmailNotificationConfig})
		if err != nil {
			configUnknown = true
		} else {
			enabled = (s.cfg == nil || s.cfg.Ops.Enabled) && parseOpsMonitoringEnabled(values[SettingKeyOpsMonitoringEnabled])
			if raw := strings.TrimSpace(values[SettingKeyOpsMetricsIntervalSeconds]); raw != "" {
				if seconds, err := strconv.Atoi(raw); err == nil {
					if seconds < int(opsMetricsCollectorMinInterval.Seconds()) {
						seconds = int(opsMetricsCollectorMinInterval.Seconds())
					}
					if seconds > int(opsMetricsCollectorMaxInterval.Seconds()) {
						seconds = int(opsMetricsCollectorMaxInterval.Seconds())
					}
					interval = time.Duration(seconds) * time.Second
				} else {
					collectorUnknown = true
				}
			}
			if raw := values[SettingKeyOpsAdvancedSettings]; raw != "" {
				var cfg OpsAdvancedSettings
				cleanupConfigUnknown = json.Unmarshal([]byte(raw), &cfg) != nil || strings.TrimSpace(raw) == "null"
			}
			if raw := values[SettingKeyOpsAlertRuntimeSettings]; raw != "" {
				cfg := defaultOpsAlertRuntimeSettings()
				if json.Unmarshal([]byte(raw), cfg) != nil || strings.TrimSpace(raw) == "null" {
					alertUnknown = true
				} else if cfg.EvaluationIntervalSeconds > 0 && cfg.EvaluationIntervalSeconds <= 86400 {
					alertInterval = time.Duration(cfg.EvaluationIntervalSeconds) * time.Second
				} else {
					alertUnknown = true
				}
			}
			if raw := values[SettingKeyOpsEmailNotificationConfig]; raw != "" {
				cfg := defaultOpsEmailNotificationConfig()
				if json.Unmarshal([]byte(raw), cfg) != nil || strings.TrimSpace(raw) == "null" {
					reportUnknown = true
				} else {
					normalizeOpsEmailNotificationConfig(cfg)
					reportsEnabled = cfg.Report.Enabled && (cfg.Report.DailySummaryEnabled || cfg.Report.WeeklySummaryEnabled || cfg.Report.ErrorDigestEnabled || cfg.Report.AccountHealthEnabled)
					if cfg.Report.Enabled {
						for _, report := range []struct {
							enabled bool
							spec    string
						}{
							{cfg.Report.DailySummaryEnabled, cfg.Report.DailySummarySchedule},
							{cfg.Report.WeeklySummaryEnabled, cfg.Report.WeeklySummarySchedule},
							{cfg.Report.ErrorDigestEnabled, cfg.Report.ErrorDigestSchedule},
							{cfg.Report.AccountHealthEnabled, cfg.Report.AccountHealthSchedule},
						} {
							if report.enabled {
								if _, err := opsScheduledReportCronParser.Parse(report.spec); err != nil {
									reportUnknown = true
								}
							}
						}
					}
				}
			}
		}
	}
	aggregationEnabled := enabled && (s.cfg == nil || s.cfg.Ops.Aggregation.Enabled)
	cleanupEnabled := false
	cleanupSchedule := opsCleanupDefaultSchedule
	cleanupUnknown := true
	// Use the schedule installed in the scheduler, including config-only overrides.
	if s.cleanupHealthConfig != nil {
		var installedLoc *time.Location
		cleanupEnabled, cleanupSchedule, installedLoc = s.cleanupHealthConfig()
		if installedLoc != nil {
			loc = installedLoc
		}
		cleanupUnknown = timezoneUnknown || cleanupConfigUnknown || (cleanupEnabled && installedLoc == nil)
	}
	rules := []opsJobHealthRule{
		{name: opsMetricsCollectorJobName, enabled: enabled, interval: interval, runtime: opsMetricsCollectorTimeout, grace: time.Minute, unknown: configUnknown || collectorUnknown},
		{name: opsAggHourlyJobName, enabled: aggregationEnabled, interval: opsAggHourlyInterval, runtime: opsAggHourlyTimeout, grace: time.Minute, unknown: configUnknown},
		{name: opsAggDailyJobName, enabled: aggregationEnabled, interval: opsAggDailyInterval, runtime: opsAggDailyTimeout, grace: time.Minute, unknown: configUnknown},
		{name: opsAlertEvaluatorJobName, enabled: enabled, interval: alertInterval, runtime: opsAlertEvaluatorTimeout, grace: time.Minute, unknown: configUnknown || alertUnknown},
		{name: opsCleanupJobName, enabled: enabled && cleanupEnabled, schedule: cleanupSchedule, location: loc, runtime: opsCleanupRunTimeout, grace: time.Minute, unknown: cleanupUnknown || configUnknown},
		// This heartbeat describes the poller (which succeeds even when no report is due).
		{name: opsScheduledReportJobName, enabled: enabled && reportsEnabled, interval: opsScheduledReportTickInterval, runtime: time.Minute, grace: time.Minute, unknown: configUnknown || reportUnknown},
	}
	byName := map[string]*OpsJobHeartbeat{}
	for _, hb := range heartbeats {
		if hb != nil {
			byName[hb.JobName] = hb
		}
	}
	out := make([]OpsJobHealth, 0, len(rules))
	for _, rule := range rules {
		out = append(out, evaluateOpsJobHealth(now, byName[rule.name], rule))
		delete(byName, rule.name)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, evaluateOpsJobHealth(now, byName[name], opsJobHealthRule{name: name, unknown: true}))
	}
	return out
}
