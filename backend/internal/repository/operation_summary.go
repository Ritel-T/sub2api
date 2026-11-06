package repository

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"time"
)

// The physical application trace table has one server-generated UUID per
// operation. Legacy unions, edge observations and billed usage are not samples.
func operationSummaryQuery(f service.ConnectionTraceFilter) (string, []any) {
	args := []any{f.Start, f.End}
	q := `SELECT COALESCE(detail->'operation'->>'final_outcome','unknown'),COUNT(*),
 COUNT(*) FILTER (WHERE detail->'operation'->>'recovered'='true')
 FROM request_connection_traces WHERE started_at >= $1 AND started_at < $2
 AND source='application' AND detail->'operation'->>'version'='2'`
	add := func(column string, v any) { args = append(args, v); q += fmt.Sprintf(" AND %s=$%d", column, len(args)) }
	for _, p := range []struct {
		column string
		value  int64
	}{{"user_id", f.UserID}, {"api_key_id", f.APIKeyID}, {"account_id", f.AccountID}, {"group_id", f.GroupID}} {
		if p.value > 0 {
			add(p.column, p.value)
		}
	}
	for _, p := range []struct{ column, value string }{{"host", f.Host}, {"model", f.Model}, {"client_ip", f.ClientIP}} {
		if p.value != "" {
			add(p.column, p.value)
		}
	}
	if f.RequestID != "" {
		args = append(args, f.RequestID)
		n := len(args)
		q += fmt.Sprintf(" AND (request_id=$%d OR client_request_id=$%d OR trace_id::text=$%d)", n, n, n)
	}
	if f.State == "abnormal" {
		q += ` AND COALESCE(detail->'operation'->>'final_outcome','unknown') NOT IN ('completed','unknown')`
	} else if f.State != "" && f.State != "all" {
		add("detail->'operation'->>'final_outcome'", f.State)
	}
	return q + " GROUP BY 1", args
}

func (r *usageLogRepository) ObservedOperationSummary(ctx context.Context, f service.ConnectionTraceFilter) *service.OperationSummary {
	out := &service.OperationSummary{Status: "observed", Coverage: "unknown", WindowBasis: "started_at", Start: f.Start, End: f.End, Outcomes: map[string]int64{}, Writer: service.OperationWriterStats{Enqueued: connectionObservationsEnqueued.Load(), Dropped: connectionObservationsDropped.Load(), WriteFailed: connectionObservationsWriteFailed.Load(), Scope: "all_connection_diagnostics_process_lifetime_not_window"}}
	if !f.End.After(f.Start) || f.End.Sub(f.Start) > time.Hour {
		out.Status = "unsupported_range"
		return out
	}
	if f.Source != "" && f.Source != "all" && f.Source != "application" {
		out.Status = "unsupported_source"
		return out
	}
	q, args := operationSummaryQuery(f)
	rows, err := r.sql.QueryContext(ctx, q, args...)
	if err != nil {
		out.Status = "unavailable"
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var count, recovered int64
		if err := rows.Scan(&outcome, &count, &recovered); err != nil {
			out.Status = "unavailable"
			out.Outcomes = map[string]int64{}
			out.SampleCount = 0
			out.RecoveredCount = 0
			return out
		}
		out.Outcomes[outcome] += count
		out.SampleCount += count
		out.RecoveredCount += recovered
	}
	if rows.Err() != nil {
		out.Status = "unavailable"
		out.Outcomes = map[string]int64{}
		out.SampleCount = 0
		out.RecoveredCount = 0
	}
	return out
}
