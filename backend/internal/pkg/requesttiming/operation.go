package requesttiming

import (
	"context"
	"sort"
	"strings"
	"time"
)

// OperationEvidence contains classifications only, never generated content.
// Complete means the source parser observed a complete protocol envelope;
// it does not mean a complete answer or successful downstream delivery.
type OperationEvidence struct {
	Protocol, FinishReason, Terminal     string
	Answer, Reasoning, ToolCall, Refusal bool
	Complete                             bool
	Unsupported                          bool
	FailureReason                        string
}

type OperationSummary struct {
	Version                  int       `json:"version"`
	Protocol                 string    `json:"protocol"`
	Transport                string    `json:"transport"`
	Scope                    string    `json:"scope"`
	FinalOutcome             string    `json:"final_outcome"`
	Reason                   string    `json:"reason"`
	ContentKind              string    `json:"content_kind"`
	FinishClass              string    `json:"finish_class"`
	Delivery                 string    `json:"delivery"`
	EvidenceStatus           string    `json:"evidence_status"`
	PartialOutput            bool      `json:"partial_output"`
	Recovered                bool      `json:"recovered"`
	AttemptCount             int       `json:"attempt_count"`
	AttemptScope             string    `json:"attempt_scope"`
	FailedAttemptCount       int       `json:"failed_attempt_count"`
	EndedAt                  time.Time `json:"ended_at"`
	ServerElapsedMS          float64   `json:"server_elapsed_ms"`
	FirstUsableMS            *float64  `json:"first_usable_ms"`
	FinalFirstUsableMS       *float64  `json:"final_first_usable_ms"`
	UpstreamAttemptElapsedMS *float64  `json:"upstream_attempt_elapsed_ms"`
}

type operationState struct {
	protocol, transport, scope                       string
	source                                           OperationEvidence
	ordinal, attempts                                int
	failed                                           map[int]bool
	firstUsable, finalFirstUsable, terminalDelivered *float64
	deliveryError                                    bool
	pendingKind                                      string
	pendingTerminal                                  bool
	sourceAttempt                                    int
}

func BeginOperation(ctx context.Context, protocol, transport, scope string) {
	c := From(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished || c.operation != nil {
		return
	}
	c.operation = &operationState{protocol: observationCode(protocol), transport: observationCode(transport), scope: observationCode(scope), failed: make(map[int]bool)}
}

func BeginOperationAttempt(ctx context.Context, ordinal int) {
	c := From(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o := c.operation
	if c.finished || o == nil {
		return
	}
	if ordinal == 0 {
		ordinal = o.ordinal + 1
	}
	if ordinal <= o.ordinal {
		return
	}
	o.ordinal, o.attempts = ordinal, ordinal
	o.source = OperationEvidence{}
	o.sourceAttempt = 0
	o.pendingKind, o.pendingTerminal = "", false
	o.finalFirstUsable, o.terminalDelivered = nil, nil
}

func ObserveOperationSource(ctx context.Context, e OperationEvidence) {
	c := From(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o := c.operation
	if c.finished || o == nil {
		return
	}
	if tr, ok := ctx.Value(attemptKey{}).(*Trace); ok && tr.c == c {
		number := tr.index + 1
		// Parser contexts may refer to an egress parent or its physical HTTP
		// child. Switching between those views is not a new logical attempt.
		for c.data.Attempts[number-1].Parent > 0 {
			number = c.data.Attempts[number-1].Parent
		}
		if o.sourceAttempt != 0 && o.sourceAttempt != number {
			o.source = OperationEvidence{}
			o.finalFirstUsable, o.terminalDelivered = nil, nil
			o.pendingKind, o.pendingTerminal = "", false
		}
		o.sourceAttempt = number
	}
	if e.Answer {
		o.pendingKind = "text"
	} else if e.ToolCall {
		o.pendingKind = "tool"
	} else if e.Reasoning {
		o.pendingKind = "reasoning"
	}
	if e.Terminal != "" && e.Complete {
		o.pendingTerminal = true
	}
	if e.Protocol != "" {
		o.protocol = observationCode(e.Protocol)
	}
	o.source.Answer = o.source.Answer || e.Answer
	o.source.Reasoning = o.source.Reasoning || e.Reasoning
	o.source.ToolCall = o.source.ToolCall || e.ToolCall
	o.source.Refusal = o.source.Refusal || e.Refusal
	o.source.Complete = o.source.Complete || e.Complete
	o.source.Unsupported = o.source.Unsupported || e.Unsupported
	if e.Terminal != "" {
		o.source.Terminal = observationCode(e.Terminal)
	}
	if e.FinishReason != "" {
		o.source.FinishReason = observationCode(e.FinishReason)
	}
	if e.FailureReason != "" {
		o.source.FailureReason = observationCode(e.FailureReason)
	}
}

// OperationDelivery is called after an actual typed output write/flush. A
// parser observing an upstream delta must not use this as delivery evidence.
func OperationDelivery(ctx context.Context, contentKind string, terminal bool, err error) {
	c := From(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o := c.operation
	if c.finished || o == nil {
		return
	}
	if err != nil {
		o.deliveryError = true
		return
	}
	v := c.offset(c.now())
	if contentKind == "text" || contentKind == "tool" {
		if o.firstUsable == nil {
			o.firstUsable = &v
		}
		if o.finalFirstUsable == nil {
			o.finalFirstUsable = &v
		}
	}
	if terminal && o.terminalDelivered == nil {
		o.terminalDelivered = &v
	}
}

func OperationAttemptFailed(ctx context.Context, reason string) {
	c := From(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o := c.operation
	if c.finished || o == nil {
		return
	}
	ordinal := o.ordinal
	if ordinal < 1 {
		ordinal = 1
	}
	o.failed[ordinal] = true
}

func observationCode(s string) string {
	if len(s) > 96 {
		return "unknown"
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' && r != '/' {
			return "unknown"
		}
	}
	return s
}

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

// operationSummary runs under the collector lock. The end-to-end measurement
// is one monotonic interval, never the sum of overlapping diagnostic spans.
func (c *Collector) operationSummary() *OperationSummary {
	o := c.operation
	if o == nil {
		return nil
	}
	e := o.source
	r := &OperationSummary{Version: 2, Protocol: o.protocol, Transport: o.transport, Scope: o.scope,
		FinalOutcome: "unknown", Reason: "source_evidence_missing", ContentKind: "unknown", FinishClass: e.FinishReason,
		Delivery: "unknown", EvidenceStatus: "partial", AttemptCount: o.attempts, FailedAttemptCount: len(o.failed),
		AttemptScope:    "logical_attempt",
		ServerElapsedMS: c.data.TotalMS, FirstUsableMS: cloneFloat(o.firstUsable), FinalFirstUsableMS: cloneFloat(o.finalFirstUsable)}
	if o.terminalDelivered != nil {
		r.ServerElapsedMS = *o.terminalDelivered
		r.Delivery = "terminal_written"
	}
	r.EndedAt = c.start.Add(time.Duration(r.ServerElapsedMS * float64(time.Millisecond)))
	physicalCount, physicalFailed, elapsed, timingComplete := operationAttempts(c.data.Attempts)
	if r.AttemptCount == 0 {
		r.AttemptScope = "physical_leaf"
		r.AttemptCount = physicalCount
		r.FailedAttemptCount = physicalFailed
	}
	if timingComplete && physicalCount > 0 {
		r.UpstreamAttemptElapsedMS = &elapsed
	}
	if e.Answer {
		r.ContentKind = "text"
	} else if e.ToolCall {
		r.ContentKind = "tool"
	} else if e.Refusal {
		r.ContentKind = "refusal"
	} else if e.Reasoning {
		r.ContentKind = "reasoning"
	}
	r.PartialOutput = o.firstUsable != nil || e.Reasoning
	switch {
	case e.FailureReason != "" || e.Terminal == "failed":
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "failed", e.FailureReason, "observed"
		if r.Reason == "" {
			r.Reason = "source_failed"
		}
		if e.FailureReason == "no_answer" {
			r.FinalOutcome, r.Reason = "incomplete", "no_answer"
			if e.Reasoning {
				r.Reason = "reasoning_only"
			}
		}
	case o.deliveryError || c.data.DownstreamError:
		r.FinalOutcome, r.Reason, r.Delivery, r.EvidenceStatus = "client_disconnected", "downstream_write_failed", "write_failed", "observed"
	case (c.data.ClientDisconnect || c.data.Canceled) && o.terminalDelivered == nil:
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "client_disconnected", "connection_ended", "observed"
	case e.Refusal && e.Complete:
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "limited", "refusal_or_filter", "observed"
	case e.Complete && isLengthFinish(e.FinishReason):
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "incomplete", "output_budget", "observed"
	case e.Complete && e.Terminal == "incomplete":
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "incomplete", "source_incomplete", "observed"
	case e.Complete && e.Terminal == "completed" && !e.Answer && !e.ToolCall:
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "incomplete", "no_answer", "observed"
		if e.Reasoning {
			r.Reason = "reasoning_only"
		}
	case e.Complete && e.Terminal == "completed" && (e.Answer || e.ToolCall) && o.terminalDelivered != nil:
		r.FinalOutcome, r.Reason, r.EvidenceStatus, r.PartialOutput = "completed", "protocol_completed", "observed", false
	}
	if c.data.Truncated {
		r.EvidenceStatus = "partial"
		if r.FinalOutcome == "completed" || r.Reason == "no_answer" || r.Reason == "reasoning_only" {
			r.FinalOutcome, r.Reason = "unknown", "observation_truncated"
		}
	}
	if e.Unsupported && (r.FinalOutcome == "completed" || r.FinalOutcome == "incomplete" || r.FinalOutcome == "limited") {
		r.FinalOutcome, r.Reason, r.EvidenceStatus = "unknown", "unsupported_semantics", "partial"
	}
	r.Recovered = r.FinalOutcome == "completed" && r.FailedAttemptCount > 0
	return r
}

func isLengthFinish(reason string) bool {
	switch strings.ToLower(reason) {
	case "length", "max_tokens", "max_output_tokens":
		return true
	}
	return false
}

// Physical leaf intervals exclude egress parents and use a union for overlap.
func operationAttempts(attempts []Attempt) (count, failed int, elapsed float64, complete bool) {
	parents := make(map[int]bool)
	for _, a := range attempts {
		if a.Parent > 0 {
			parents[a.Parent] = true
		}
	}
	intervals := make([][2]float64, 0, len(attempts))
	complete = true
	for _, a := range attempts {
		if parents[a.Number] {
			continue
		}
		count++
		if (a.Error != "" && !a.CleanupCanceled) || a.Status >= 400 || a.Terminal == "failed" {
			failed++
		}
		if a.EndMS == nil || !finite(a.StartMS) || !finite(*a.EndMS) || a.StartMS < 0 || *a.EndMS < a.StartMS {
			complete = false
			continue
		}
		intervals = append(intervals, [2]float64{a.StartMS, *a.EndMS})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i][0] < intervals[j][0] })
	var end float64
	for _, v := range intervals {
		if v[1] <= end {
			continue
		}
		start := v[0]
		if start < end {
			start = end
		}
		elapsed += v[1] - start
		end = v[1]
	}
	return
}

// OperationOutputFlushed connects typed source observations with a successful
// local parser flush. Heartbeat writers must never call this function.
func OperationOutputFlushed(ctx context.Context) {
	c := From(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.finished || c.operation == nil || c.data.DownstreamError {
		c.mu.Unlock()
		return
	}
	o := c.operation
	kind, terminal := o.pendingKind, o.pendingTerminal
	o.pendingKind, o.pendingTerminal = "", false
	v := c.offset(c.now())
	if kind == "text" || kind == "tool" {
		if o.firstUsable == nil {
			o.firstUsable = &v
		}
		if o.finalFirstUsable == nil {
			o.finalFirstUsable = &v
		}
	}
	if terminal && o.terminalDelivered == nil {
		o.terminalDelivered = &v
	}
	c.mu.Unlock()
}
