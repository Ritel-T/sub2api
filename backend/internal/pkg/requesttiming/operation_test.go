package requesttiming

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationSourceAndDeliveryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		evidence   OperationEvidence
		delivered  bool
		writeError bool
		want       string
		reason     string
	}{
		{"reasoning_only", OperationEvidence{Reasoning: true, Complete: true, Terminal: "completed"}, true, false, "incomplete", "reasoning_only"},
		{"tool_complete", OperationEvidence{ToolCall: true, Complete: true, Terminal: "completed"}, true, false, "completed", "protocol_completed"},
		{"budget_with_text", OperationEvidence{Answer: true, Complete: true, Terminal: "completed", FinishReason: "length"}, true, false, "incomplete", "output_budget"},
		{"source_not_delivery", OperationEvidence{Answer: true, Complete: true, Terminal: "completed"}, false, false, "unknown", "source_evidence_missing"},
		{"write_failed", OperationEvidence{Answer: true, Complete: true, Terminal: "completed"}, true, true, "client_disconnected", "downstream_write_failed"},
		{"usage_done_not_proof", OperationEvidence{}, true, false, "unknown", "source_evidence_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(time.Now(), 0)
			ctx := With(context.Background(), c)
			BeginOperation(ctx, "responses", "http", "application_request")
			ObserveOperationSource(ctx, tc.evidence)
			if tc.delivered {
				var err error
				if tc.writeError {
					err = errors.New("closed")
				}
				OperationDelivery(ctx, "", true, err)
			}
			c.Finish(200, false)
			c.WhenFinished(func(s Snapshot) {
				if s.Operation == nil || s.Operation.FinalOutcome != tc.want || s.Operation.Reason != tc.reason {
					t.Fatalf("unexpected operation: %+v", s.Operation)
				}
			})
		})
	}
}

func TestOperationRecoveryTimeAndPhysicalUnion(t *testing.T) {
	start := time.Now()
	now := start
	c := newCollector(start, 0, func() time.Time { return now })
	ctx := With(context.Background(), c)
	BeginOperation(ctx, "chat", "http", "application_request")
	BeginOperationAttempt(ctx, 1)
	now = start.Add(10 * time.Millisecond)
	ObserveOperationSource(ctx, OperationEvidence{Answer: true})
	OperationDelivery(ctx, "text", false, nil)
	OperationAttemptFailed(ctx, "stream_interrupted")
	BeginOperationAttempt(ctx, 2)
	now = start.Add(90 * time.Millisecond)
	ObserveOperationSource(ctx, OperationEvidence{Answer: true, Complete: true, Terminal: "completed"})
	OperationOutputFlushed(ctx)
	end := 80.0
	c.data.Attempts = []Attempt{{Number: 1, Kind: "egress", StartMS: 0, EndMS: &end}, {Number: 2, Parent: 1, Kind: "http", StartMS: 10, EndMS: floatValue(60)}, {Number: 3, Kind: "http", StartMS: 50, EndMS: &end}}
	now = start.Add(100 * time.Millisecond)
	c.Finish(200, false)
	c.WhenFinished(func(s Snapshot) {
		o := s.Operation
		if !o.Recovered || o.AttemptCount != 2 || o.FailedAttemptCount != 1 || o.ServerElapsedMS != 90 || o.FirstUsableMS == nil || *o.FirstUsableMS != 10 || o.FinalFirstUsableMS == nil || *o.FinalFirstUsableMS != 90 || o.UpstreamAttemptElapsedMS == nil || *o.UpstreamAttemptElapsedMS != 70 {
			t.Fatalf("bad timing or recovery: %+v", o)
		}
	})
}

func TestOperationTurnIsolation(t *testing.T) {
	for i := 0; i < 2; i++ {
		c := New(time.Now(), 0)
		ctx := With(context.Background(), c)
		BeginOperation(ctx, "responses", "ws", "accepted_model_operation")
		if i == 0 {
			OperationDelivery(ctx, "text", false, errors.New("closed"))
		}
		c.Finish(200, false)
		c.WhenFinished(func(s Snapshot) {
			if i == 1 && (s.Operation.Delivery != "unknown" || s.Operation.FirstUsableMS != nil || s.Operation.FinalOutcome != "unknown") {
				t.Fatalf("turn leak: %+v", s.Operation)
			}
		})
	}
}

func floatValue(n float64) *float64 { return &n }
