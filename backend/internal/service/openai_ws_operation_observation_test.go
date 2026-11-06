package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type wsOperationRecorder struct{ records []ConnectionTrace }

func (r *wsOperationRecorder) RecordConnectionTrace(v ConnectionTrace) {
	r.records = append(r.records, v)
}
func TestWSOperationTurnIsolationAndDelivery(t *testing.T) {
	recorder := &wsOperationRecorder{}
	o := &OpenAIWSOperationObserver{parent: context.Background(), recorder: recorder}
	first := []byte(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}}`)
	o.Start(1, time.Now().Add(-time.Second))
	o.Source(first)
	o.Delivery(first, nil)
	o.End(10, nil, nil)
	second := []byte(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning"}]}}`)
	o.Start(2, time.Now())
	o.Source(second)
	o.Delivery(second, nil)
	o.End(10, nil, nil)
	require.Len(t, recorder.records, 2)
	require.NotEqual(t, recorder.records[0].TraceID, recorder.records[1].TraceID)
	require.Equal(t, "completed", recorder.records[0].Detail.Operation.FinalOutcome)
	require.Equal(t, "incomplete", recorder.records[1].Detail.Operation.FinalOutcome)
	require.Nil(t, recorder.records[1].Detail.Operation.FirstUsableMS)
	require.GreaterOrEqual(t, recorder.records[0].Detail.Operation.ServerElapsedMS, float64(1000))
	o.Start(3, time.Now())
	o.Source(first)
	o.Delivery(first, errors.New("closed"))
	o.End(10, nil, nil)
	require.Equal(t, "client_disconnected", recorder.records[2].Detail.Operation.FinalOutcome)
}
func TestWSOperationRetryRetainsOneClock(t *testing.T) {
	recorder := &wsOperationRecorder{}
	o := &OpenAIWSOperationObserver{parent: context.Background(), recorder: recorder}
	o.Start(1, time.Now().Add(-time.Second))
	o.End(10, nil, &UpstreamFailoverError{StatusCode: 502})
	require.Empty(t, recorder.records)
	o.Start(1, time.Now())
	output := []byte(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","id":"a","name":"read","arguments":"{}"}]}}`)
	o.Source(output)
	o.Delivery(output, nil)
	o.End(11, nil, nil)
	require.Len(t, recorder.records, 1)
	op := recorder.records[0].Detail.Operation
	require.Equal(t, "completed", op.FinalOutcome)
	require.True(t, op.Recovered)
	require.Equal(t, 2, op.AttemptCount)
	require.Equal(t, 1, op.FailedAttemptCount)
	require.GreaterOrEqual(t, op.ServerElapsedMS, float64(1000))
}
