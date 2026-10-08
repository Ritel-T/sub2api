package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func compressedBudgetTestBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	var writer io.WriteCloser
	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&compressed)
	case "deflate":
		writer = zlib.NewWriter(&compressed)
	case "zstd":
		encoder, err := zstd.NewWriter(&compressed, zstd.WithEncoderConcurrency(1))
		require.NoError(t, err)
		writer = encoder
	default:
		t.Fatalf("unsupported test encoding %s", encoding)
	}
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func TestReadRequestBodyBudgetNilPreservesExistingReader(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip", "zstd", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			wire := []byte(samplePayload)
			if encoding != "identity" {
				wire = compressedBudgetTestBody(t, encoding, wire)
			}
			existing := newRequestWithBody(t, wire, encoding)
			budgeted := newRequestWithBody(t, wire, encoding)
			want, wantErr := ReadRequestBodyWithPreallocLimit(existing, 64<<20)
			got, gotErr := ReadRequestBodyWithPreallocLimitAndBudget(budgeted, 64<<20, nil)
			require.Equal(t, wantErr, gotErr)
			require.Equal(t, want, got)
			require.Equal(t, existing.Header, budgeted.Header)
			require.Equal(t, existing.ContentLength, budgeted.ContentLength)
		})
	}
}

type budgetFailingBody struct {
	reads int
	err   error
}

func (body *budgetFailingBody) Read([]byte) (int, error) { body.reads++; return 0, body.err }
func (body *budgetFailingBody) Close() error             { return nil }

func TestReadRequestBodyBudgetStopsBeforeWireRead(t *testing.T) {
	denied := errors.New("budget denied")
	body := &budgetFailingBody{err: errors.New("must not read")}
	request := newRequestWithBody(t, []byte("content"), "")
	request.Body = body
	_, err := ReadRequestBodyWithPreallocLimitAndBudget(request, 64<<20, func(stage string, size int64) error {
		require.Equal(t, "read", stage)
		require.Positive(t, size)
		return denied
	})
	require.ErrorIs(t, err, denied)
	require.Zero(t, body.reads)
}

func TestReadRequestBodyBudgetPreservesSourceError(t *testing.T) {
	sourceErr := errors.New("source transport failed")
	body := &budgetFailingBody{err: sourceErr}
	request := newRequestWithBody(t, []byte("content"), "gzip")
	request.Body = body
	_, err := ReadRequestBodyWithPreallocLimitAndBudget(request, 64<<20, func(string, int64) error { return nil })
	require.ErrorIs(t, err, sourceErr)
}

func TestReadRequestBodyCompressedBudgetPreservesRejection(t *testing.T) {
	for _, encoding := range []string{"gzip", "zstd", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			wire := compressedBudgetTestBody(t, encoding, []byte(strings.Repeat("content", 600000)))
			request := newRequestWithBody(t, wire, encoding)
			denied := errors.New("decoded budget denied")
			var peak int64
			_, err := ReadRequestBodyWithPreallocLimitAndBudget(request, 64<<20, func(stage string, size int64) error {
				peak = max(peak, size)
				if stage == "decode" && size > 1<<20 {
					return denied
				}
				return nil
			})
			require.ErrorIs(t, err, denied)
			require.Greater(t, peak, int64(1<<20))
			require.Equal(t, encoding, request.Header.Get("Content-Encoding"), "failed reads must preserve wire headers")
		})
	}
}

func TestReadRequestBodyCompressedBudgetKeepsDecodedLimit(t *testing.T) {
	for _, encoding := range []string{"gzip", "zstd", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			wire := compressedBudgetTestBody(t, encoding, []byte(strings.Repeat("x", 2<<20)))
			_, err := ReadRequestBodyWithPreallocLimitAndBudget(newRequestWithBody(t, wire, encoding), 1<<20, func(string, int64) error { return nil })
			var maxErr *http.MaxBytesError
			require.ErrorAs(t, err, &maxErr)
			require.Equal(t, int64(1<<20), maxErr.Limit)
		})
	}
}

func TestReadRequestBodyZstdBudgetAccountsAllFrameWindows(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithSingleSegment(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = encoder.Close() })
	first := encoder.EncodeAll([]byte(samplePayload), nil)
	secondBody := []byte(strings.Repeat("x", 4<<20))
	second := encoder.EncodeAll(secondBody, nil)
	// Standard skippable frame magic, length 3, then the opaque payload.
	skippable := []byte{0x50, 0x2a, 0x4d, 0x18, 3, 0, 0, 0, 1, 2, 3}
	wire := append(append(append([]byte{}, first...), skippable...), second...)
	window, err := zstdRequestWindow(wire, 64<<20)
	require.NoError(t, err)
	require.Equal(t, int64(len(secondBody)), window)
	var firstDecodeReservation int64
	body, err := ReadRequestBodyWithPreallocLimitAndBudget(newRequestWithBody(t, wire, "zstd"), 64<<20, func(stage string, size int64) error {
		if stage == "decode" && firstDecodeReservation == 0 {
			firstDecodeReservation = size
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, append([]byte(samplePayload), secondBody...), body)
	require.GreaterOrEqual(t, firstDecodeReservation, window+(128<<10), "history must be reserved before decoder construction")
}

func TestReadRequestBodyZstdBudgetRejectsMalformedFrames(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	t.Cleanup(func() { _ = encoder.Close() })
	valid := encoder.EncodeAll([]byte(samplePayload), nil)
	var header zstd.Header
	require.NoError(t, header.Decode(valid))
	invalidBlock := append([]byte{}, valid...)
	invalidBlock[header.HeaderSize] |= 6 // Reserved block type 3.
	for _, wire := range [][]byte{
		[]byte("invalid"), valid[:len(valid)-1], invalidBlock,
		append(append([]byte{}, valid...), 1, 2, 3),
		{0x50, 0x2a, 0x4d, 0x18, 4, 0, 0, 0, 1},
	} {
		_, err := ReadRequestBodyWithPreallocLimitAndBudget(newRequestWithBody(t, wire, "zstd"), 64<<20, func(string, int64) error { return nil })
		require.Error(t, err)
	}
}

type retainedBodyBudgetError struct{ limit int64 }

func (e *retainedBodyBudgetError) Error() string                 { return "retained body budget exceeded" }
func (e *retainedBodyBudgetError) RequestBodyBudgetLimit() int64 { return e.limit }

func TestReadRequestBodyBudgetAdaptsWithoutRetainingOverflow(t *testing.T) {
	const allowed = 1 << 20
	for _, tc := range []struct {
		name     string
		size     int
		accepted bool
	}{
		{"below boundary", allowed - 1, true},
		{"exact boundary", allowed, true},
		{"one byte above", allowed + 1, false},
		{"larger overflow", allowed + (1 << 20), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := bytes.Repeat([]byte{'x'}, tc.size)
			request := newRequestWithBody(t, body, "")
			request.ContentLength = -1
			denied := &retainedBodyBudgetError{limit: allowed}
			var largestSuccessful int64
			got, err := ReadRequestBodyWithPreallocLimitAndBudget(request, 64<<20, func(_ string, size int64) error {
				if size > allowed {
					return denied
				}
				largestSuccessful = max(largestSuccessful, size)
				return nil
			})
			require.LessOrEqual(t, largestSuccessful, int64(allowed), "no retained buffer can bypass successful reservation")
			if tc.accepted {
				require.NoError(t, err)
				require.Equal(t, body, got)
			} else {
				require.ErrorIs(t, err, denied)
				require.Nil(t, got)
			}
		})
	}
}

func TestReadRequestBodyBudgetBoundaryPreservesEndErrors(t *testing.T) {
	for _, sourceErr := range []error{io.ErrUnexpectedEOF, context.Canceled} {
		request := newRequestWithBody(t, nil, "")
		request.ContentLength = -1
		request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bytes.Repeat([]byte{'x'}, 1<<20)), iotest.ErrReader(sourceErr)))
		_, err := ReadRequestBodyWithPreallocLimitAndBudget(request, 64<<20, func(_ string, size int64) error {
			if size > 1<<20 {
				return &retainedBodyBudgetError{limit: 1 << 20}
			}
			return nil
		})
		require.ErrorIs(t, err, sourceErr, "one-byte EOF check cannot hide a truncated or cancelled source")
	}
}
