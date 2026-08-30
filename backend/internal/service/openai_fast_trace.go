package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// newOpenAIFastTrace snapshots the request-side Fast state before any policy
// or transport rewrite can change service_tier.  Group force is intentionally
// read from the authenticated request context: usage recording may run later
// with a detached context and must not try to infer the value again.
func newOpenAIFastTrace(ctx context.Context, account *Account, body []byte) *OpenAIFastTrace {
	if account == nil || account.Platform != PlatformOpenAI {
		return nil
	}
	groupID := int64(0)
	groupFreeOpenAIFast := false
	if ctx != nil {
		if group, _ := ctx.Value(ctxkey.Group).(*Group); IsGroupContextValid(group) {
			groupID = group.ID
			groupFreeOpenAIFast = group.FreeOpenAIFast
		}
	}
	return &OpenAIFastTrace{
		Captured:             true,
		GroupID:              groupID,
		GroupForceOpenAIFast: openAIGroupForcesFast(ctx, account),
		GroupFreeOpenAIFast:  groupFreeOpenAIFast,
		RequestedServiceTier: openAIFastTraceTierFromBody(body),
	}
}

func openAIFastTraceTierFromBody(body []byte) string {
	if tier := extractOpenAIServiceTierFromBody(body); tier != nil {
		return strings.TrimSpace(*tier)
	}
	return ""
}

func openAIFastTraceTierFromMap(body map[string]any) string {
	if tier := extractOpenAIServiceTier(body); tier != nil {
		return strings.TrimSpace(*tier)
	}
	return ""
}

// SetOutbound records the canonical tier in the body that is about to be sent
// upstream.  Only the scalar service_tier is inspected; the body is never
// retained by the trace.
func (t *OpenAIFastTrace) SetOutbound(body []byte) {
	if t == nil {
		return
	}
	t.OutboundServiceTier = openAIFastTraceTierFromBody(body)
}

// SetRequestedFromBody refreshes the request-side tier after a protocol
// adapter has materialized an equivalent field (for example Anthropic's
// beta fast-mode header becomes service_tier=priority). It is called before
// the Fast policy runs, so a policy filter cannot overwrite the client intent.
func (t *OpenAIFastTrace) SetRequestedFromBody(body []byte) {
	if t == nil {
		return
	}
	if tier := openAIFastTraceTierFromBody(body); tier != "" {
		t.RequestedServiceTier = tier
	}
}

// SetOutboundMap is the equivalent helper for the structured WebSocket
// payload used by the WSv2 forwarder.
func (t *OpenAIFastTrace) SetOutboundMap(body map[string]any) {
	if t == nil {
		return
	}
	t.OutboundServiceTier = openAIFastTraceTierFromMap(body)
}

// attachOpenAIFastTrace preserves a more precise trace produced by an inner
// transport (for example raw Chat Completions or passthrough) while filling in
// any request-side fields captured by the outer protocol adapter.
func attachOpenAIFastTrace(result *OpenAIForwardResult, trace *OpenAIFastTrace) {
	if result == nil || trace == nil || !trace.Captured {
		return
	}
	if result.OpenAIFastTrace == nil {
		result.OpenAIFastTrace = trace
		return
	}
	existing := result.OpenAIFastTrace
	if !existing.Captured {
		existing.Captured = true
		existing.GroupID = trace.GroupID
		existing.GroupForceOpenAIFast = trace.GroupForceOpenAIFast
		existing.GroupFreeOpenAIFast = trace.GroupFreeOpenAIFast
	}
	if existing.GroupID <= 0 {
		existing.GroupID = trace.GroupID
	}
	if existing.RequestedServiceTier == "" {
		existing.RequestedServiceTier = trace.RequestedServiceTier
	}
	if existing.OutboundServiceTier == "" {
		existing.OutboundServiceTier = trace.OutboundServiceTier
	}
}

// logOpenAIFastTrace emits exactly the scalar fields needed to diagnose Fast
// routing and billing.  It is intentionally independent of the request body
// and uses the global logger so a request-scoped logger cannot accidentally
// add sensitive fields to this diagnostic event.
func logOpenAIFastTrace(
	ctx context.Context,
	input *OpenAIRecordUsageInput,
	result *OpenAIForwardResult,
	requestID string,
	resolution ServiceTierBillingResolution,
	rateMultiplier float64,
) {
	if input == nil || input.CyberBlocked || result == nil || result.OpenAIFastTrace == nil {
		return
	}
	trace := result.OpenAIFastTrace
	if !trace.Captured {
		return
	}
	if strings.TrimSpace(requestID) == "" {
		requestID = resolveUsageBillingRequestID(ctx, result.RequestID)
	}
	groupID := trace.GroupID
	if groupID <= 0 && input.APIKey != nil && input.APIKey.GroupID != nil {
		groupID = *input.APIKey.GroupID
	}
	observedTier := strings.TrimSpace(resolution.Observed)
	billedTier := strings.TrimSpace(resolution.Billing)
	if billedTier == "" && result.ServiceTier != nil {
		billedTier = strings.TrimSpace(*result.ServiceTier)
	}

	logger.L().Info("openai.fast_trace",
		zap.String("event", "openai.fast_trace"),
		zap.String("request_id", strings.TrimSpace(requestID)),
		zap.Int64("group_id", groupID),
		zap.Bool("group_force_openai_fast", trace.GroupForceOpenAIFast),
		zap.Bool("group_free_openai_fast", trace.GroupFreeOpenAIFast),
		zap.String("requested_service_tier", strings.TrimSpace(trace.RequestedServiceTier)),
		zap.String("outbound_service_tier", strings.TrimSpace(trace.OutboundServiceTier)),
		zap.String("observed_service_tier", observedTier),
		zap.String("billed_service_tier", billedTier),
		zap.Float64("rate_multiplier", rateMultiplier),
		zap.Bool("openai_ws_mode", result.OpenAIWSMode),
	)
}
