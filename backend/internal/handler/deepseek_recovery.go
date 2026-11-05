package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	deepSeekRecoveryMaxAttempts = 3
	deepSeekRecoveryTimeout     = 10 * time.Minute
)

// Recovery is deliberately limited to the two stateless HTTP protocols used
// by DSH. Responses ownership/continuations and multi-choice streams keep
// their existing contracts.
func deepSeekRecoveryEnabled(platform, model string, body []byte) bool {
	return platform == "deepseek" &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-") &&
		gjson.GetBytes(body, "n").Int() <= 1 &&
		!gjson.GetBytes(body, "previous_response_id").Exists()
}

type deepSeekRecoveryRequest struct {
	apiKey                                     *service.APIKey
	subscription                               *service.UserSubscription
	model, forwardModel, protocol, sessionHash string
	stream                                     bool
	body                                       []byte
	mapping                                    service.ChannelMappingResult
	pricingAt                                  time.Time
	forward                                    func(context.Context, *gin.Context, *service.Account) (*service.OpenAIForwardResult, error)
}

// Each attempt gets its own Gin state and cancellation owner. Only the final
// selected attempt reaches customer billing; discarded attempts remain Ops
// evidence. No recursion through auth/RPM/quotas and no same-account resend.
func (h *OpenAIGatewayHandler) runDeepSeekRecovery(c *gin.Context, in deepSeekRecoveryRequest, log *zap.Logger) {
	parent, cancel := context.WithTimeout(c.Request.Context(), deepSeekRecoveryTimeout)
	defer cancel()
	sink := newDeepSeekRecoveryStream(c.Writer, in.protocol, in.stream)
	stopHeartbeat := startDeepSeekRecoveryHeartbeat(parent, sink, cancel)
	defer stopHeartbeat()
	excluded := make(map[int64]struct{})
	var lastErr error
	var lastResult *service.OpenAIForwardResult
	var lastAccount *service.Account
	var finalContext *gin.Context
	policyRecorded := false
	attempts := 0
	for attempts < deepSeekRecoveryMaxAttempts && parent.Err() == nil {
		var selection *service.AccountSelectionResult
		var err error
		if len(excluded) == 0 {
			selection, _, err = h.gatewayService.SelectAccountWithSchedulerForCapability(
				parent, in.apiKey.GroupID, "", in.sessionHash, in.forwardModel, excluded,
				service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions,
				false, false, true, "deepseek")
		} else {
			selection, err = h.gatewayService.SelectDeepSeekRecoveryAccount(parent, in.apiKey.GroupID, in.forwardModel, excluded)
		}
		if err != nil || selection == nil || selection.Account == nil {
			if lastErr == nil {
				lastErr = service.ErrNoAvailableAccounts
			}
			break
		}
		account := selection.Account
		// A bad/changed candidate must not turn a selector bug into an infinite
		// loop, nor permit retrying a previously submitted account.
		if _, seen := excluded[account.ID]; seen {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			lastErr = errors.New("recovery selector returned an attempted account")
			break
		}
		excluded[account.ID] = struct{}{}
		child := c.Copy()
		child.Request = c.Request.Clone(service.WithDeepSeekRecoverySelection(parent))
		child.Writer = sink
		if err := sink.BeginAttempt(); err != nil {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			lastErr = err
			break
		}
		setOpsSelectedAccount(child, account.ID, account.Platform)
		var streamStarted bool
		var admissionErr error
		release, acquired := h.acquireOpenAIAccountSlot(child, in.apiKey.GroupID, in.sessionHash, selection,
			false, &streamStarted, log, func(_ int, _ string, _ string, message string) { admissionErr = errors.New(message) })
		if acquired != openAISlotAcquireOK {
			if acquired == openAISlotAcquireProfitVetoed {
				continue
			}
			lastErr = admissionErr
			break
		}
		account = selection.Account
		fresh, admissionErr := h.gatewayService.RevalidateDeepSeekRecoveryAccount(child.Request.Context(), in.apiKey.GroupID, account, in.forwardModel)
		if admissionErr != nil {
			if release != nil {
				release()
			}
			lastErr = admissionErr
			continue
		}
		account = fresh
		attempts++
		attemptCtx, attempt := service.NewDeepSeekRecoveryAttempt(child.Request.Context())
		child.Request = child.Request.WithContext(attemptCtx)
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				attempt.Close()
				if release != nil {
					release()
				}
			}()
			res, cause := in.forward(attemptCtx, child, account)
			if sourceErr := attempt.Err(); sourceErr != nil {
				cause = sourceErr
			}
			if res != nil && attempt.Outcome().UsageUnknown {
				res.UsageUnavailable = true
			}
			return res, cause
		}()
		if mark := attempt.Outcome().CyberPolicy; mark != nil && !policyRecorded {
			policyRecorded = true
			service.MarkOpsCyberPolicy(child, *mark)
			// Keep the existing audit/notification and configured session policy,
			// but never let its independent failure-billing branch charge a loser.
			h.recordCyberPolicyIfMarked(child, in.apiKey, account, in.subscription, in.model, false,
				in.body, clientRequestedUsageFields(child, in.mapping, in.model, ""), service.HashUsageRequestPayload(in.body))
		}
		forwardErr = sink.EndAttempt(forwardErr)
		lastErr, lastResult, lastAccount, finalContext = forwardErr, result, account, child
		if forwardErr == nil {
			if result == nil {
				lastErr = errors.New("upstream returned no result")
				break
			}
			setOpsSelectedAccount(c, account.ID, account.Platform)
			service.SetOpsUpstreamModel(c, result.UpstreamModel)
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, in.forwardModel, true, result.FirstTokenMs)
			// Commit is a delivery operation. A write failure must never return to
			// the model loop, even when zero bytes were reported written.
			stopHeartbeat()
			_ = sink.CommitSuccess()
			h.submitDeepSeekRecoveryUsage(c, child, in, account, result, log)
			return
		}
		service.RecordDeepSeekRecoveryFailure(c, account, result, attempts, forwardErr, attempt.Outcome())
		outcome := attempt.Outcome()
		var upstreamFailure *service.UpstreamFailoverError
		budgetEnded := outcome.FinishReason == "length" || outcome.FinishReason == "max_tokens" || outcome.FinishReason == "max_output_tokens"
		if parent.Err() == nil && !sink.ClientGone() && !budgetEnded && outcome.CyberPolicy == nil &&
			errors.As(forwardErr, &upstreamFailure) && (upstreamFailure.StatusCode >= 500 || upstreamFailure.StatusCode == 429) {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, in.forwardModel, false, nil, forwardErr)
		}
		structured := gjson.GetBytes(in.body, "response_format.type").String()
		if parent.Err() != nil || !sink.CanRetry() || service.IsOpenAITurnAdmissionError(forwardErr) ||
			(structured != "" && structured != "text" && sink.BusinessStarted()) {
			break
		}
		if attempts < deepSeekRecoveryMaxAttempts {
			h.gatewayService.RecordOpenAIAccountSwitch()
		}
	}
	if lastErr == nil {
		lastErr = parent.Err()
	}
	if lastErr == nil {
		lastErr = errors.New("upstream recovery exhausted")
	}
	stopHeartbeat()
	if lastAccount != nil {
		setOpsSelectedAccount(c, lastAccount.ID, lastAccount.Platform)
		if lastResult != nil {
			service.SetOpsUpstreamModel(c, lastResult.UpstreamModel)
		}
	}
	if c.Request.Context().Err() == nil && !sink.ClientGone() {
		service.MarkOpsStreamFailure(c, "api_error", "deepseek_recovery_exhausted", "DeepSeek recovery exhausted", http.StatusBadGateway)
		_ = sink.CommitFailure("DeepSeek 暂时无法完成回复，请稍后重试。")
	}
	// Only an attempt that exposed a tool or whose downstream disconnected
	// retains the existing partial-usage contract. Recoverable failed text
	// attempts never occupy the final customer billing key.
	if finalContext != nil && lastResult != nil && (sink.ToolExposed() || sink.ClientGone()) {
		h.submitDeepSeekRecoveryUsage(c, finalContext, in, lastAccount, lastResult, log)
	}
	log.Warn("deepseek.recovery_exhausted", zap.Int("attempts", attempts), zap.Bool("tool_exposed", sink.ToolExposed()))
}

func startDeepSeekRecoveryHeartbeat(ctx context.Context, sink *deepSeekRecoveryStream, cancel context.CancelFunc) func() {
	if !sink.stream {
		return func() {}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if sink.Heartbeat() != nil {
					cancel()
					return
				}
			}
		}
	}()
	return sync.OnceFunc(func() { close(stop); <-done })
}

func (h *OpenAIGatewayHandler) submitDeepSeekRecoveryUsage(c, final *gin.Context, in deepSeekRecoveryRequest, account *service.Account, result *service.OpenAIForwardResult, log *zap.Logger) {
	stampOpenAIRequestedReasoningEffort(result, c)
	input := &service.OpenAIRecordUsageInput{
		Result: result, APIKey: in.apiKey, User: in.apiKey.User, Account: account, Subscription: in.subscription,
		InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: resolveOpenAIUpstreamEndpoint(final, account, result),
		UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c),
		SessionID: service.ExtractClientSessionID(c), RequestPayloadHash: service.HashUsageRequestPayload(in.body),
		APIKeyService: h.apiKeyService, QuotaPlatform: service.QuotaPlatform(c.Request.Context(), in.apiKey),
		PricingAt: in.pricingAt, ChannelUsageFields: clientRequestedUsageFields(c, in.mapping, in.model, result.UpstreamModel),
	}
	h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, input); err != nil {
			log.Error("deepseek.recovery_billing_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		}
	})
}
