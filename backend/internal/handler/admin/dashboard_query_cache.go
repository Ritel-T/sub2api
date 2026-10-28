package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

var (
	dashboardTrendCache        = newSnapshotCache(30 * time.Second)
	dashboardModelStatsCache   = newSnapshotCache(30 * time.Second)
	dashboardGroupStatsCache   = newSnapshotCache(30 * time.Second)
	dashboardUsersTrendCache   = newSnapshotCache(30 * time.Second)
	dashboardAPIKeysTrendCache = newSnapshotCache(30 * time.Second)
)

type dashboardTrendCacheKey struct {
	StartTime             string `json:"start_time"`
	EndTime               string `json:"end_time"`
	Granularity           string `json:"granularity"`
	UserID                int64  `json:"user_id"`
	APIKeyID              int64  `json:"api_key_id"`
	AccountID             int64  `json:"account_id"`
	GroupID               int64  `json:"group_id"`
	Model                 string `json:"model"`
	RequestType           *int16 `json:"request_type"`
	Stream                *bool  `json:"stream"`
	NativeCompactionV2    *bool  `json:"native_compaction_v2"`
	BillingType           *int8  `json:"billing_type"`
	UpstreamModelMismatch *bool  `json:"upstream_model_mismatch"`
	RequestID             string `json:"request_id,omitempty"`
	BillingMode           string `json:"billing_mode,omitempty"`
	ModelFilterSource     string `json:"model_filter_source,omitempty"`
}

type dashboardModelGroupCacheKey struct {
	StartTime             string `json:"start_time"`
	EndTime               string `json:"end_time"`
	UserID                int64  `json:"user_id"`
	APIKeyID              int64  `json:"api_key_id"`
	AccountID             int64  `json:"account_id"`
	GroupID               int64  `json:"group_id"`
	ModelSource           string `json:"model_source,omitempty"`
	Model                 string `json:"model,omitempty"`
	RequestType           *int16 `json:"request_type"`
	Stream                *bool  `json:"stream"`
	NativeCompactionV2    *bool  `json:"native_compaction_v2"`
	BillingType           *int8  `json:"billing_type"`
	UpstreamModelMismatch *bool  `json:"upstream_model_mismatch"`
	RequestID             string `json:"request_id,omitempty"`
	BillingMode           string `json:"billing_mode,omitempty"`
	ModelFilterSource     string `json:"model_filter_source,omitempty"`
}

type dashboardEntityTrendCacheKey struct {
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	Granularity string `json:"granularity"`
	Limit       int    `json:"limit"`
	Metric      string `json:"metric,omitempty"`
	Filters     string `json:"filters,omitempty"`
}

func cacheStatusValue(hit bool) string {
	if hit {
		return "hit"
	}
	return "miss"
}

func mustMarshalDashboardCacheKey(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func snapshotPayloadAs[T any](payload any) (T, error) {
	typed, ok := payload.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("unexpected cache payload type %T", payload)
	}
	return typed, nil
}

func (h *DashboardHandler) getUsageTrendCached(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	userID, apiKeyID, accountID, groupID int64,
	model string,
	requestType *int16,
	stream *bool,
	nativeCompactionV2 *bool,
	billingType *int8,
	upstreamModelMismatch *bool,
	extraFilters ...usagestats.UsageLogFilters,
) ([]usagestats.TrendDataPoint, bool, error) {
	filters := usagestats.UsageLogFilters{UserID: userID, APIKeyID: apiKeyID, AccountID: accountID, GroupID: groupID,
		Model: model, RequestType: requestType, Stream: stream, NativeCompactionV2: nativeCompactionV2, BillingType: billingType, UpstreamModelMismatch: upstreamModelMismatch}
	if len(extraFilters) > 0 {
		filters = extraFilters[0]
	}
	if filters.Model == "" {
		filters.ModelFilterSource = ""
	}

	key := mustMarshalDashboardCacheKey(dashboardTrendCacheKey{
		RequestID: filters.RequestID, BillingMode: filters.BillingMode, ModelFilterSource: filters.ModelFilterSource,
		StartTime:             startTime.UTC().Format(time.RFC3339Nano),
		EndTime:               endTime.UTC().Format(time.RFC3339Nano),
		Granularity:           granularity,
		UserID:                filters.UserID,
		APIKeyID:              filters.APIKeyID,
		AccountID:             filters.AccountID,
		GroupID:               filters.GroupID,
		Model:                 filters.Model,
		RequestType:           filters.RequestType,
		Stream:                filters.Stream,
		NativeCompactionV2:    filters.NativeCompactionV2,
		BillingType:           filters.BillingType,
		UpstreamModelMismatch: filters.UpstreamModelMismatch,
	})
	entry, hit, err := dashboardTrendCache.GetOrLoad(key, func() (any, error) {
		return h.dashboardService.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, filters)
	})
	if err != nil {
		return nil, hit, err
	}
	trend, err := snapshotPayloadAs[[]usagestats.TrendDataPoint](entry.Payload)
	return trend, hit, err
}

func (h *DashboardHandler) getModelStatsCached(
	ctx context.Context,
	startTime, endTime time.Time,
	userID, apiKeyID, accountID, groupID int64,
	modelSource string,
	requestType *int16,
	stream *bool,
	nativeCompactionV2 *bool,
	billingType *int8,
	upstreamModelMismatch *bool,
	extraFilters ...usagestats.UsageLogFilters,
) ([]usagestats.ModelStat, bool, error) {
	filters := usagestats.UsageLogFilters{UserID: userID, APIKeyID: apiKeyID, AccountID: accountID, GroupID: groupID,
		RequestType: requestType, Stream: stream, NativeCompactionV2: nativeCompactionV2, BillingType: billingType, UpstreamModelMismatch: upstreamModelMismatch}
	if len(extraFilters) > 0 {
		filters = extraFilters[0]
	}
	if filters.Model == "" {
		filters.ModelFilterSource = ""
	}

	key := mustMarshalDashboardCacheKey(dashboardModelGroupCacheKey{
		RequestID: filters.RequestID, BillingMode: filters.BillingMode, ModelFilterSource: filters.ModelFilterSource,
		Model:                 filters.Model,
		StartTime:             startTime.UTC().Format(time.RFC3339Nano),
		EndTime:               endTime.UTC().Format(time.RFC3339Nano),
		UserID:                filters.UserID,
		APIKeyID:              filters.APIKeyID,
		AccountID:             filters.AccountID,
		GroupID:               filters.GroupID,
		ModelSource:           usagestats.NormalizeModelSource(modelSource),
		RequestType:           filters.RequestType,
		Stream:                filters.Stream,
		NativeCompactionV2:    filters.NativeCompactionV2,
		BillingType:           filters.BillingType,
		UpstreamModelMismatch: filters.UpstreamModelMismatch,
	})
	entry, hit, err := dashboardModelStatsCache.GetOrLoad(key, func() (any, error) {
		return h.dashboardService.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, filters, modelSource)
	})
	if err != nil {
		return nil, hit, err
	}
	stats, err := snapshotPayloadAs[[]usagestats.ModelStat](entry.Payload)
	return stats, hit, err
}

func (h *DashboardHandler) getGroupStatsCached(
	ctx context.Context,
	startTime, endTime time.Time,
	userID, apiKeyID, accountID, groupID int64,
	requestType *int16,
	stream *bool,
	nativeCompactionV2 *bool,
	billingType *int8,
	upstreamModelMismatch *bool,
	extraFilters ...usagestats.UsageLogFilters,
) ([]usagestats.GroupStat, bool, error) {
	filters := usagestats.UsageLogFilters{UserID: userID, APIKeyID: apiKeyID, AccountID: accountID, GroupID: groupID,
		RequestType: requestType, Stream: stream, NativeCompactionV2: nativeCompactionV2, BillingType: billingType, UpstreamModelMismatch: upstreamModelMismatch}
	if len(extraFilters) > 0 {
		filters = extraFilters[0]
	}
	if filters.Model == "" {
		filters.ModelFilterSource = ""
	}

	key := mustMarshalDashboardCacheKey(dashboardModelGroupCacheKey{
		RequestID: filters.RequestID, BillingMode: filters.BillingMode, ModelFilterSource: filters.ModelFilterSource,
		Model:                 filters.Model,
		StartTime:             startTime.UTC().Format(time.RFC3339Nano),
		EndTime:               endTime.UTC().Format(time.RFC3339Nano),
		UserID:                filters.UserID,
		APIKeyID:              filters.APIKeyID,
		AccountID:             filters.AccountID,
		GroupID:               filters.GroupID,
		RequestType:           filters.RequestType,
		Stream:                filters.Stream,
		NativeCompactionV2:    filters.NativeCompactionV2,
		BillingType:           filters.BillingType,
		UpstreamModelMismatch: filters.UpstreamModelMismatch,
	})
	entry, hit, err := dashboardGroupStatsCache.GetOrLoad(key, func() (any, error) {
		return h.dashboardService.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, filters)
	})
	if err != nil {
		return nil, hit, err
	}
	stats, err := snapshotPayloadAs[[]usagestats.GroupStat](entry.Payload)
	return stats, hit, err
}

func (h *DashboardHandler) getAPIKeyUsageTrendCached(ctx context.Context, startTime, endTime time.Time, granularity string, limit int, extraFilters ...usagestats.UsageLogFilters) ([]usagestats.APIKeyUsageTrendPoint, bool, error) {
	filters := usagestats.UsageLogFilters{}
	filterKey := ""
	if len(extraFilters) > 0 {
		filters = extraFilters[0]
		if filters.HasRowFilters() {
			filterKey = usageStatsCacheKey(filters)
		}
	}

	key := mustMarshalDashboardCacheKey(dashboardEntityTrendCacheKey{
		Filters:     filterKey,
		StartTime:   startTime.UTC().Format(time.RFC3339Nano),
		EndTime:     endTime.UTC().Format(time.RFC3339Nano),
		Granularity: granularity,
		Limit:       limit,
	})
	entry, hit, err := dashboardAPIKeysTrendCache.GetOrLoad(key, func() (any, error) {
		return h.dashboardService.GetAPIKeyUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, limit, filters)
	})
	if err != nil {
		return nil, hit, err
	}
	trend, err := snapshotPayloadAs[[]usagestats.APIKeyUsageTrendPoint](entry.Payload)
	return trend, hit, err
}

func (h *DashboardHandler) getUserUsageTrendCached(ctx context.Context, startTime, endTime time.Time, granularity string, limit int, metric string, extraFilters ...usagestats.UsageLogFilters) ([]usagestats.UserUsageTrendPoint, bool, error) {
	filters := usagestats.UsageLogFilters{}
	filterKey := ""
	if len(extraFilters) > 0 {
		filters = extraFilters[0]
		if filters.HasRowFilters() {
			filterKey = usageStatsCacheKey(filters)
		}
	}

	key := mustMarshalDashboardCacheKey(dashboardEntityTrendCacheKey{
		Filters:     filterKey,
		StartTime:   startTime.UTC().Format(time.RFC3339Nano),
		EndTime:     endTime.UTC().Format(time.RFC3339Nano),
		Granularity: granularity,
		Limit:       limit,
		Metric:      metric,
	})
	entry, hit, err := dashboardUsersTrendCache.GetOrLoad(key, func() (any, error) {
		return h.dashboardService.GetUserUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, limit, metric, filters)
	})
	if err != nil {
		return nil, hit, err
	}
	trend, err := snapshotPayloadAs[[]usagestats.UserUsageTrendPoint](entry.Payload)
	return trend, hit, err
}
