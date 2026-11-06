package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
	"github.com/tidwall/gjson"
	"sync"
	"time"
)

type openAIWSOperationSourceKey struct{}

// OpenAIWSOperationObserver owns observations only. A connection may carry
// many operations; account failover keeps the accepted operation's clock.
type OpenAIWSOperationObserver struct {
	mu                sync.Mutex
	parent            context.Context
	record            ConnectionTrace
	recorder          interface{ RecordConnectionTrace(ConnectionTrace) }
	collector         *requesttiming.Collector
	ctx               context.Context
	ordinal           int
	retry             bool
	pendingEnd        bool
	deliveredTerminal bool
}

func (s *OpenAIGatewayService) NewWSOperationObserver(ctx context.Context, started time.Time, record ConnectionTrace) *OpenAIWSOperationObserver {
	o := &OpenAIWSOperationObserver{parent: ctx, record: record}
	o.recorder, _ = s.usageLogRepo.(interface{ RecordConnectionTrace(ConnectionTrace) })
	o.Start(1, started)
	return o
}

func (o *OpenAIWSOperationObserver) Start(_ int, started time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.collector != nil && o.pendingEnd {
		o.finishLocked()
	}
	if o.collector != nil {
		if o.retry {
			o.ordinal++
			requesttiming.BeginOperationAttempt(o.ctx, o.ordinal)
			o.retry = false
		}
		return
	}
	if started.IsZero() {
		started = time.Now()
	}
	o.collector = requesttiming.New(started, 0)
	o.ctx = requesttiming.With(o.parent, o.collector)
	requesttiming.BeginOperation(o.ctx, "responses", "ws", "accepted_model_operation")
	o.ordinal = 1
	requesttiming.BeginOperationAttempt(o.ctx, 1)
}
func (o *OpenAIWSOperationObserver) Source(payload []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.collector == nil {
		return
	}
	ObserveOperationProtocol(o.ctx, "responses", payload, "")
}
func (o *OpenAIWSOperationObserver) Delivery(payload []byte, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.collector == nil {
		return
	}
	typ := gjson.GetBytes(payload, "type").String()
	terminal := typ == "response.completed" || typ == "response.done" || typ == "response.incomplete" || typ == "response.failed" || typ == "error" || typ == "response.cancelled"
	requesttiming.OperationDelivery(o.ctx, OperationProtocolContentKind("responses", payload, ""), terminal, err)
	if terminal || err != nil {
		o.deliveredTerminal = true
	}
	if o.pendingEnd && o.deliveredTerminal {
		o.finishLocked()
	}
}
func (o *OpenAIWSOperationObserver) End(accountID int64, result *OpenAIForwardResult, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.collector == nil {
		return
	}
	o.record.AccountID = accountID
	if result != nil && result.Model != "" {
		o.record.Model = result.Model
	}
	if err != nil {
		requesttiming.OperationAttemptFailed(o.ctx, "ws_turn_error")
		var failover *UpstreamFailoverError
		if errors.As(err, &failover) {
			o.retry = true
			return
		}
		if isOpenAIWSClientDisconnectError(err) || IsOpenAIWSClientGoneError(err) || errors.Is(err, context.Canceled) {
			requesttiming.OperationDelivery(o.ctx, "", false, err)
		} else {
			requesttiming.ObserveOperationSource(o.ctx, requesttiming.OperationEvidence{Protocol: "responses", Terminal: "failed", FailureReason: "ws_turn_error", Complete: true})
		}
	}
	// Passthrough announces its usage result before the terminal socket write.
	// Wait for that actual write without delaying or changing its billing callback.
	if err == nil && !o.deliveredTerminal {
		o.pendingEnd = true
		return
	}
	o.finishLocked()
}
func (o *OpenAIWSOperationObserver) Finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finishLocked()
}
func (o *OpenAIWSOperationObserver) finishLocked() {
	if o.collector == nil {
		return
	}
	record := o.record
	record.Source = "application"
	record.Stream = true
	record.Method = "GET"
	o.collector.WhenFinished(func(detail requesttiming.Snapshot) {
		record.TraceID, record.StartedAt, record.Detail = detail.TraceID, detail.StartedAt, detail
		if o.recorder != nil {
			o.recorder.RecordConnectionTrace(record)
		}
	})
	o.collector.Finish(101, o.parent.Err() != nil)
	o.collector = nil
	o.ctx = nil
	o.retry = false
	o.pendingEnd = false
	o.deliveredTerminal = false
}
