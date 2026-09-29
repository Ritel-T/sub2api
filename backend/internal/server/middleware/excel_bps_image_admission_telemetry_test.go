package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestBPSImageAdmissionRejectionCapturesFailedDecision(t *testing.T) {
	for _, tt := range []struct {
		name, reason string
		maxRequests  int
		firstWeight  int64
	}{
		{name: "request slots", reason: "request_slots", maxRequests: 1, firstWeight: 8 << 20},
		{name: "byte budget", reason: "byte_budget", maxRequests: 2, firstWeight: 512 << 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			budget := &bpsImageAdmissionBudget{limitBytes: 512 << 20, maxRequests: tt.maxRequests}
			first, rejection := budget.acquireWithRejection(tt.firstWeight)
			require.Nil(t, rejection)
			second, rejection := budget.acquireWithRejection(8 << 20)
			require.Nil(t, second)
			require.Equal(t, &bpsImageAdmissionRejection{
				requests: 1, bytes: tt.firstWeight, maxRequests: tt.maxRequests,
				limitBytes: 512 << 20, attemptedWeight: 8 << 20, reason: tt.reason,
			}, rejection)
			first.release()
			budget.configure(1024<<20, 128)
			require.Equal(t, 1, rejection.requests, "later releases must not alter the failed-decision snapshot")
			require.Equal(t, tt.firstWeight, rejection.bytes)
			require.Equal(t, tt.maxRequests, rejection.maxRequests)
		})
	}
}

func TestBPSImageAdmissionResizeRejectionCapturesFailedDecision(t *testing.T) {
	budget := &bpsImageAdmissionBudget{limitBytes: 512 << 20, maxRequests: 2}
	first, acquired := budget.acquire(256 << 20)
	require.True(t, acquired)
	second, acquired := budget.acquire(256 << 20)
	require.True(t, acquired)
	rejection := first.resizeWithRejection(264 << 20)
	require.Equal(t, &bpsImageAdmissionRejection{
		requests: 2, bytes: 512 << 20, maxRequests: 2,
		limitBytes: 512 << 20, attemptedWeight: 264 << 20, reason: "byte_budget",
	}, rejection)
	require.Equal(t, int64(256<<20), first.weight, "a failed resize must retain its original reservation")
	first.release()
	second.release()
	require.Equal(t, int64(512<<20), rejection.bytes)
	require.Zero(t, budget.bytes)
}

func TestBPSImageAdmissionRejectionLogsOnlyBoundedCapacityFields(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	log := zap.New(core)
	rejection := &bpsImageAdmissionRejection{
		requests: 64, bytes: 512 << 20, maxRequests: 128,
		limitBytes: 1024 << 20, attemptedWeight: 520 << 20, reason: "byte_budget",
	}
	for _, stage := range []string{"acquire", "read", "decode"} {
		bpsImageAdmissionLogRejection(log, stage, "unsupported-private-header-value", -1, rejection)
	}
	require.Equal(t, 3, logs.Len())
	for i, entry := range logs.All() {
		require.Equal(t, "image_relay_admission_rejected", entry.Message)
		require.Equal(t, map[string]interface{}{
			"component": "image_admission", "stage": []string{"acquire", "read", "decode"}[i],
			"reason": "byte_budget", "occupied_requests": int64(64), "occupied_bytes": int64(512 << 20),
			"max_requests": int64(128), "limit_bytes": int64(1024 << 20),
			"attempted_weight_bytes": int64(520 << 20), "content_encoding": "unsupported",
			"wire_content_length": int64(-1),
		}, entry.ContextMap())
	}
	for _, tt := range []struct{ wire, bounded string }{
		{"", "identity"}, {" Identity ", "identity"}, {"GZIP", "gzip"}, {"x-gzip", "gzip"},
		{" zstd ", "zstd"}, {"deflate", "deflate"}, {"gzip, private", "unsupported"},
	} {
		require.Equal(t, tt.bounded, bpsImageAdmissionEncoding(tt.wire))
	}
}
