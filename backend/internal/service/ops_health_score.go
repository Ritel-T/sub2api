package service

import (
	"math"
	"time"
)

// computeDashboardHealthScore retains the integer compatibility field.
func computeDashboardHealthScore(now time.Time, overview *OpsDashboardOverview) int {
	if overview == nil {
		return 0
	}
	applyDashboardHealth(now, overview)
	return overview.HealthScore
}

// computeBusinessHealth calculates business health score (0-100)
// Components: Error Rate (50%) + TTFT (50%)
func computeBusinessHealth(overview *OpsDashboardOverview) float64 {
	// Error rate score: 1% → 100, 10% → 0 (linear)
	// Combines request errors and upstream errors
	errorScore := 100.0
	errorPct := clampFloat64(overview.ErrorRate*100, 0, 100)
	upstreamPct := clampFloat64(overview.UpstreamErrorRate*100, 0, 100)
	combinedErrorPct := math.Max(errorPct, upstreamPct) // Use worst case
	if combinedErrorPct > 1.0 {
		if combinedErrorPct <= 10.0 {
			errorScore = (10.0 - combinedErrorPct) / 9.0 * 100
		} else {
			errorScore = 0
		}
	}

	// TTFT score: 1s → 100, 3s → 0 (linear)
	// Time to first token is critical for user experience
	ttftScore := 100.0
	if overview.TTFT.P99 != nil {
		p99 := float64(*overview.TTFT.P99)
		if p99 > 1000 {
			if p99 <= 3000 {
				ttftScore = (3000 - p99) / 2000 * 100
			} else {
				ttftScore = 0
			}
		}
	}

	// Weighted combination: 50% error rate + 50% TTFT
	return errorScore*0.5 + ttftScore*0.5
}

// computeInfraHealth calculates infrastructure health score (0-100)
// Components: Storage (40%) + Compute Resources (30%) + Background Jobs (30%)
func computeInfraHealth(now time.Time, overview *OpsDashboardOverview) float64 {
	// Storage score: DB critical, Redis less critical
	storageScore := 100.0
	if overview.SystemMetrics != nil {
		if overview.SystemMetrics.DBOK != nil && !*overview.SystemMetrics.DBOK {
			storageScore = 0 // DB failure is critical
		} else if overview.SystemMetrics.RedisOK != nil && !*overview.SystemMetrics.RedisOK {
			storageScore = 50 // Redis failure is degraded but not critical
		}
	}

	// Compute resources score: CPU + Memory
	computeScore := 100.0
	if overview.SystemMetrics != nil {
		cpuScore := 100.0
		if overview.SystemMetrics.CPUUsagePercent != nil {
			cpuPct := clampFloat64(*overview.SystemMetrics.CPUUsagePercent, 0, 100)
			if cpuPct > 80 {
				if cpuPct <= 100 {
					cpuScore = (100 - cpuPct) / 20 * 100
				} else {
					cpuScore = 0
				}
			}
		}

		memScore := 100.0
		if overview.SystemMetrics.MemoryUsagePercent != nil {
			memPct := clampFloat64(*overview.SystemMetrics.MemoryUsagePercent, 0, 100)
			if memPct > 85 {
				if memPct <= 100 {
					memScore = (100 - memPct) / 15 * 100
				} else {
					memScore = 0
				}
			}
		}

		computeScore = (cpuScore + memScore) / 2
	}

	// Background jobs score
	jobScore := 100.0
	failedJobs := 0
	totalJobs := 0
	for _, job := range overview.JobHealth {
		if job.State == "disabled" || job.State == "unknown" || job.State == "pending" {
			continue
		}
		totalJobs++
		if job.State == "failed" || job.State == "late" {
			failedJobs++
		}
	}

	if totalJobs > 0 && failedJobs > 0 {
		jobScore = (1 - float64(failedJobs)/float64(totalJobs)) * 100
	}

	// Weighted combination
	return storageScore*0.4 + computeScore*0.3 + jobScore*0.3
}

func clampFloat64(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// OpsHealthScoreDetail weights are the legacy global contributions and sum to 1.
// A zero with an insufficient-evidence reason is unavailable, not a measured failure.
type OpsHealthScoreDetail struct {
	Component string  `json:"component"`
	Score     float64 `json:"score"`
	Weight    float64 `json:"weight"`
	Reason    string  `json:"reason"`
}

func applyDashboardHealth(now time.Time, ov *OpsDashboardOverview) {
	ov.HealthScoreVersion = "legacy-v2-job-health"
	details := make([]OpsHealthScoreDetail, 0, 5)
	missing, degraded := false, false
	add := func(component string, score, weight float64, reason string, known bool) {
		if !known {
			missing = true
			score = 0
		} else if score < 100 {
			degraded = true
		}
		details = append(details, OpsHealthScoreDetail{component, score, weight, reason})
	}
	traffic := ov.RequestCountSLA > 0 || ov.RequestCountTotal > 0 || ov.ErrorCountTotal > 0
	errorPct := math.Max(clampFloat64(ov.ErrorRate*100, 0, 100), clampFloat64(ov.UpstreamErrorRate*100, 0, 100))
	errorScore := 100.0
	if errorPct > 1 {
		errorScore = clampFloat64((10-errorPct)/9*100, 0, 100)
	}
	errorReason := "error_rate_within_legacy_threshold"
	if errorScore < 100 {
		errorReason = "request_or_upstream_error_rate_above_1_percent"
	}
	if !traffic {
		errorReason = "insufficient_evidence_no_requests"
	}
	add("error_rate", errorScore, .35, errorReason, traffic)
	ttftKnown := traffic && ov.TTFT.P99 != nil
	if ov.TTFT.SampleCount != nil && *ov.TTFT.SampleCount <= 0 {
		ttftKnown = false
	}
	ttftScore := 100.0
	if ov.TTFT.P99 != nil && *ov.TTFT.P99 > 1000 {
		ttftScore = clampFloat64((3000-float64(*ov.TTFT.P99))/2000*100, 0, 100)
	}
	ttftReason := "ttft_within_legacy_threshold"
	if ttftScore < 100 {
		ttftReason = "ttft_p99_above_1000ms_legacy_threshold"
	}
	if !ttftKnown {
		ttftReason = "insufficient_evidence_ttft"
	}
	if ttftKnown && (ov.TTFT.SampleCount == nil || ov.TTFT.PercentileMethod != "raw_exact") {
		missing = true
		ttftReason += ";provisional_percentile_evidence:" + ov.TTFT.PercentileMethod
	}
	add("ttft", ttftScore, .35, ttftReason, ttftKnown)

	maxAge := opsMetricsCollectorMinInterval + opsMetricsCollectorTimeout + time.Minute
	for _, job := range ov.JobHealth {
		if job.JobName == opsMetricsCollectorJobName && job.DeadlineAt != nil {
			for _, hb := range ov.JobHeartbeats {
				if hb != nil && hb.JobName == job.JobName && hb.LastSuccessAt != nil {
					maxAge = job.DeadlineAt.Sub(*hb.LastSuccessAt)
				}
			}
		}
	}
	m := ov.SystemMetrics
	fresh := m != nil && !m.CreatedAt.IsZero() && !m.CreatedAt.After(now) && !now.After(m.CreatedAt.Add(maxAge))
	storageKnown := fresh && m.DBOK != nil && m.RedisOK != nil
	storage := 100.0
	storageReason := "storage_healthy"
	if fresh && m.DBOK != nil && !*m.DBOK {
		storage = 0
		storageReason = "database_unhealthy"
		storageKnown = true
	} else if fresh && m.RedisOK != nil && !*m.RedisOK {
		storage = 50
		storageReason = "redis_unhealthy"
		storageKnown = true
	}
	if !storageKnown {
		storageReason = "insufficient_evidence_storage_missing_or_stale"
	}
	add("storage", storage, .12, storageReason, storageKnown)
	computeKnown := fresh && m.CPUUsagePercent != nil && m.MemoryUsagePercent != nil
	compute := 100.0
	computeReason := "compute_within_legacy_threshold"
	if computeKnown {
		cpu, mem := 100.0, 100.0
		if *m.CPUUsagePercent > 80 {
			cpu = clampFloat64((100-*m.CPUUsagePercent)/20*100, 0, 100)
		}
		if *m.MemoryUsagePercent > 85 {
			mem = clampFloat64((100-*m.MemoryUsagePercent)/15*100, 0, 100)
		}
		compute = (cpu + mem) / 2
		if compute < 100 {
			computeReason = "cpu_above_80_or_memory_above_85_percent"
		}
	} else {
		computeReason = "insufficient_evidence_compute_missing_or_stale"
	}
	add("compute", compute, .09, computeReason, computeKnown)
	total, failed, unknown := 0, 0, 0
	for _, job := range ov.JobHealth {
		switch job.State {
		case "disabled":
		case "healthy":
			total++
		case "late", "failed":
			total++
			failed++
		default:
			unknown++
		}
	}
	jobs := 100.0
	jobsReason := "enabled_jobs_healthy"
	jobsKnown := unknown == 0 && total > 0
	if total > 0 {
		jobs = (1 - float64(failed)/float64(total)) * 100
	}
	if failed > 0 {
		jobsReason = "background_jobs_failed_or_late"
		jobsKnown = true
	} else if !jobsKnown {
		jobsReason = "insufficient_evidence_job_health"
	}
	if unknown > 0 {
		missing = true
	}
	add("jobs", jobs, .09, jobsReason, jobsKnown)
	score := 0.0
	for _, detail := range details {
		score += detail.Score * detail.Weight
	}
	ov.HealthScore = int(math.Round(clampFloat64(score, 0, 100)))
	ov.HealthScoreDetails = details
	ov.HealthScoreAvailable = !missing
	ov.HealthStatus = "healthy"
	if missing {
		ov.HealthStatus = "unknown"
	}
	if degraded {
		ov.HealthStatus = "degraded"
	}
}
