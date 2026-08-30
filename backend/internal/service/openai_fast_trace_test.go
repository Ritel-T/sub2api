package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIFastTraceCapturesCanonicalRequestAndOutboundTier(t *testing.T) {
	group := &Group{
		ID:              77,
		Platform:        PlatformOpenAI,
		Status:          StatusActive,
		Hydrated:        true,
		ForceOpenAIFast: true,
		FreeOpenAIFast:  true,
	}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	account := &Account{Platform: PlatformOpenAI}

	trace := newOpenAIFastTrace(ctx, account, []byte(`{"model":"gpt-5.5","service_tier":"fast"}`))
	require.NotNil(t, trace)
	require.True(t, trace.Captured)
	require.Equal(t, int64(77), trace.GroupID)
	require.True(t, trace.GroupForceOpenAIFast)
	require.True(t, trace.GroupFreeOpenAIFast)
	require.Equal(t, "priority", trace.RequestedServiceTier)

	trace.SetOutbound([]byte(`{"model":"gpt-5.5","service_tier":"priority"}`))
	require.Equal(t, "priority", trace.OutboundServiceTier)

	// The trace is scalar-only; no request payload is retained on the object.
	require.NotContains(t, fmt.Sprintf("%+v", trace), "gpt-5.5")
}

func TestRecordUsageEmitsRedactedOpenAIFastTrace(t *testing.T) {
	sink, release := captureStructuredLog(t)
	defer release()

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)

	groupID := int64(77)
	requested := "priority"
	result := &OpenAIForwardResult{
		RequestID:                   "upstream-rid-1",
		Model:                       "gpt-5.5",
		Usage:                       OpenAIUsage{InputTokens: 1, OutputTokens: 1},
		Duration:                    1,
		ServiceTier:                 &requested,
		UpstreamResponseServiceTier: "default",
		OpenAIFastTrace: &OpenAIFastTrace{
			Captured:             true,
			GroupForceOpenAIFast: true,
			GroupFreeOpenAIFast:  true,
			RequestedServiceTier: "priority",
			OutboundServiceTier:  "priority",
		},
	}
	input := &OpenAIRecordUsageInput{
		Result: result,
		APIKey: &APIKey{
			ID:      1,
			GroupID: &groupID,
			Group:   &Group{ID: groupID, RateMultiplier: 1.25},
		},
		User:    &User{ID: 2},
		Account: &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}

	require.NoError(t, svc.RecordUsage(context.Background(), input))

	sink.mu.Lock()
	defer sink.mu.Unlock()
	var eventFields map[string]any
	for _, event := range sink.events {
		if event != nil && event.Message == "openai.fast_trace" {
			eventFields = event.Fields
			break
		}
	}
	require.NotNil(t, eventFields)
	require.Equal(t, "openai.fast_trace", eventFields["event"])
	require.Equal(t, "upstream-rid-1", eventFields["request_id"])
	require.Equal(t, int64(77), eventFields["group_id"])
	require.Equal(t, true, eventFields["group_force_openai_fast"])
	require.Equal(t, true, eventFields["group_free_openai_fast"])
	require.Equal(t, "priority", eventFields["requested_service_tier"])
	require.Equal(t, "priority", eventFields["outbound_service_tier"])
	require.Equal(t, "default", eventFields["observed_service_tier"])
	require.Equal(t, "default", eventFields["billed_service_tier"])
	require.Equal(t, 1.25, eventFields["rate_multiplier"])
	require.Equal(t, false, eventFields["openai_ws_mode"])
	require.NotContains(t, fmt.Sprint(eventFields), "Authorization")
	require.NotContains(t, fmt.Sprint(eventFields), "sk-")
}
