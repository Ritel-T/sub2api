package repository

import (
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"strings"
)

func appendUsageLogReadFilters(query string, args []any, f usagestats.UsageLogFilters, alias string) (string, []any) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	for _, id := range []struct {
		column string
		value  int64
	}{{"user_id", f.UserID}, {"api_key_id", f.APIKeyID}, {"account_id", f.AccountID}, {"group_id", f.GroupID}} {
		if id.value > 0 {
			query += fmt.Sprintf(" AND %s%s = $%d", prefix, id.column, len(args)+1)
			args = append(args, id.value)
		}
	}
	if requestID := strings.TrimSpace(f.RequestID); requestID != "" {
		query += fmt.Sprintf(" AND %srequest_id = $%d", prefix, len(args)+1)
		args = append(args, requestID)
	}
	if strings.TrimSpace(f.Model) != "" {
		expr := prefix + "model"
		if f.ModelFilterSource != "" {
			expr = resolveModelDimensionExpressionWithAlias(f.ModelFilterSource, alias)
		}
		query += fmt.Sprintf(" AND %s = $%d", expr, len(args)+1)
		args = append(args, f.Model)
	}
	if f.RequestType != nil {
		condition, values := buildRequestTypeFilterConditionWithAlias(len(args)+1, *f.RequestType, alias)
		query += " AND " + condition
		args = append(args, values...)
	} else if f.Stream != nil {
		query += fmt.Sprintf(" AND %sstream = $%d", prefix, len(args)+1)
		args = append(args, *f.Stream)
	}
	query, args = appendNativeCompactionV2QueryFilter(query, args, f.NativeCompactionV2, alias)
	if f.BillingType != nil {
		query += fmt.Sprintf(" AND %sbilling_type = $%d", prefix, len(args)+1)
		args = append(args, int16(*f.BillingType))
	}
	query, args = appendUsageLogBillingModeQueryFilter(query, args, f.BillingMode, alias)
	if f.UpstreamModelMismatch != nil {
		query += " AND " + upstreamModelMismatchCondition(prefix+"upstream_model_mismatch", *f.UpstreamModelMismatch)
	}
	return query, args
}

// usageLogReadConditions is shared by record pagination and aggregate statistics.
// Keep their identity, time, legacy request-type and model matching semantics identical.
func usageLogReadConditions(filters UsageLogFilters) ([]string, []any) {
	conditions := make([]string, 0, 9)
	args := make([]any, 0, 9)

	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	if requestID := strings.TrimSpace(filters.RequestID); requestID != "" {
		conditions = append(conditions, fmt.Sprintf("request_id = $%d", len(args)+1))
		args = append(args, requestID)
	}
	conditions, args = appendUsageLogModelWhereCondition(conditions, args, filters.Model, filters.ModelFilterSource)
	conditions, args = appendRequestTypeOrStreamWhereCondition(conditions, args, filters.RequestType, filters.Stream)
	conditions, args = appendNativeCompactionV2WhereCondition(conditions, args, filters.NativeCompactionV2, "")
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("billing_type = $%d", len(args)+1))
		args = append(args, int16(*filters.BillingType))
	}
	conditions, args = appendUsageLogBillingModeWhereCondition(conditions, args, filters.BillingMode)
	if filters.UpstreamModelMismatch != nil {
		conditions = append(conditions, upstreamModelMismatchCondition("upstream_model_mismatch", *filters.UpstreamModelMismatch))
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}

	return conditions, args
}
