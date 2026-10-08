package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/requestcapture"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	jsonUTF8BOMLen            = 3
	// maxDecompressedBodySize limits the decompressed request body to 64 MB
	// to prevent decompression bomb attacks.
	maxDecompressedBodySize = 64 << 20
)

// PrereadBody 回填已读取完成的请求体：作为 io.ReadCloser 可被再次顺序消费
// （multipart 流式解析），同时暴露 Bytes() 让 ReadRequestBodyWithPrealloc
// 直接返回原始切片，避免二次分配与复制。
//
// 注意：ReadRequestBodyWithPrealloc 对 PrereadBody 的快速路径不检查内部
// reader 是否已被（部分）消费——包装的字节完整且不可变，即使 reader 已被
// 流式消费过，Bytes() 也始终返回完整请求体。
type PrereadBody struct {
	body   []byte
	reader *bytes.Reader
}

// NewPrereadBody 包装一段已读取的请求体。
func NewPrereadBody(body []byte) *PrereadBody {
	return &PrereadBody{body: body, reader: bytes.NewReader(body)}
}

// Read 实现 io.Reader（转发给内部 bytes.Reader）。
func (p *PrereadBody) Read(b []byte) (int, error) {
	if p == nil {
		return 0, io.EOF
	}
	return p.reader.Read(b)
}

// Close 实现 io.Closer；请求体已在内存中，无需释放资源。
func (p *PrereadBody) Close() error { return nil }

// Bytes 返回完整的原始请求体切片。
func (p *PrereadBody) Bytes() []byte {
	if p == nil {
		return nil
	}
	return p.body
}

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
// 已由 PrereadBody 回填的请求体直接返回其完整切片（零拷贝），不检查内部
// reader 是否已被消费——见 PrereadBody 的文档说明。
func ReadRequestBodyWithPrealloc(req *http.Request) (result []byte, resultErr error) {
	return readRequestBodyWithPrealloc(req, maxDecompressedBodySize, false, nil)
}

// ReadRequestBodyWithPreallocLimit applies a caller-specific decoded body cap.
// It rejects compressed input that exceeds the cap instead of truncating it.
func ReadRequestBodyWithPreallocLimit(req *http.Request, maxDecodedBytes int64) (result []byte, resultErr error) {
	return readRequestBodyWithPrealloc(req, maxDecodedBytes, true, nil)
}

// RequestBodyReadBudget reserves capacity before allocating another bounded
// chunk or a decoder window. Bytes is the largest retained wire body, decoded
// body, or decoder window; callers must allow for their processing copies.
// Stage is "read" for wire bytes and "decode" for decoding allocations.
type RequestBodyReadBudget func(stage string, bytes int64) error

// RequestBodyBudgetLimitError optionally reports the retained-byte ceiling at
// a failed reservation. The reader can retry a smaller bounded allocation;
// every retry still reserves against the live budget before allocating.
type RequestBodyBudgetLimitError interface {
	error
	RequestBodyBudgetLimit() int64
}

// ReadRequestBodyWithPreallocLimitAndBudget additionally meters compressed
// requests during reading and decoding. A nil budget preserves the existing
// reader behavior. Budget errors retain their identity through decode errors.
func ReadRequestBodyWithPreallocLimitAndBudget(req *http.Request, maxDecodedBytes int64, budget RequestBodyReadBudget) ([]byte, error) {
	return readRequestBodyWithPrealloc(req, maxDecodedBytes, true, budget)
}

func readRequestBodyWithPrealloc(req *http.Request, maxDecodedBytes int64, strict bool, budget RequestBodyReadBudget) (result []byte, resultErr error) {
	defer func() {
		if resultErr == nil && req != nil {
			requestcapture.FromContext(req.Context()).ClientRequest(result, req.Header.Get("Content-Type"), req.Header)
		}
	}()

	if req == nil || req.Body == nil {
		return nil, nil
	}
	if preread, ok := req.Body.(*PrereadBody); ok {
		return preread.Bytes(), nil
	}

	capHint := requestBodyReadInitCap
	if req.ContentLength > 0 {
		switch {
		case req.ContentLength < int64(requestBodyReadInitCap):
			capHint = requestBodyReadInitCap
		case req.ContentLength > int64(requestBodyReadMaxInitCap):
			capHint = requestBodyReadMaxInitCap
		default:
			capHint = int(req.ContentLength)
		}
	}

	var wireBudget func(int64) error
	if budget != nil {
		wireBudget = func(size int64) error { return budget("read", size) }
	}
	raw, err := readRequestBodyChunks(req.Body, capHint, req.ContentLength, wireBudget)
	if err != nil {
		return nil, err
	}

	enc := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return raw, nil
	}

	decoded, err := decompressRequestBody(enc, raw, maxDecodedBytes, strict, budget)
	if err != nil {
		return nil, fmt.Errorf("decode Content-Encoding %q: %w", enc, err)
	}

	req.Header.Del("Content-Encoding")
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(decoded))

	return decoded, nil
}

// Read bounded chunks as bytes arrive, then assemble the exact-size result.
// This avoids doubling large buffers or eagerly allocating an untrusted
// Content-Length before the corresponding bytes have arrived.
func readRequestBodyChunks(reader io.Reader, initialCapacity int, contentLength int64, budget func(int64) error) ([]byte, error) {
	capacity := initialCapacity
	var chunks [][]byte
	total := 0
	for {
		chunkCapacity := capacity
		if remaining := contentLength - int64(total); remaining >= 0 && remaining < int64(chunkCapacity) {
			chunkCapacity = int(remaining) + 1
		}
		var saturatedBudgetErr error
		if budget != nil {
			for {
				err := budget(int64(total) + int64(chunkCapacity))
				if err == nil {
					break
				}
				var limited RequestBodyBudgetLimitError
				if !errors.As(err, &limited) {
					return nil, err
				}
				remaining := limited.RequestBodyBudgetLimit() - int64(total)
				if remaining > 0 && remaining < int64(chunkCapacity) {
					chunkCapacity = int(remaining)
					continue
				}
				if remaining == 0 && total > 0 {
					// At an exact retained-body boundary, EOF must not require
					// another retained chunk. One fixed scratch byte distinguishes
					// EOF from overflow; overflow is never appended or accepted.
					saturatedBudgetErr = err
					break
				}
				return nil, err
			}
		}
		var chunk []byte
		n := 0
		var err error
		if saturatedBudgetErr != nil {
			var probe [1]byte
			read, readErr := reader.Read(probe[:])
			if read > 0 {
				return nil, saturatedBudgetErr
			}
			if readErr == nil {
				return nil, io.ErrNoProgress
			}
			err = readErr
		} else {
			chunk = make([]byte, chunkCapacity)
			for n < len(chunk) && err == nil {
				var read int
				read, err = reader.Read(chunk[n:])
				n += read
			}
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		if n > 0 {
			chunks = append(chunks, chunk[:n])
			total += n
		}
		if err != nil {
			if len(chunks) == 0 {
				return chunk[:0], nil
			}
			if len(chunks) == 1 {
				return chunks[0], nil
			}
			body := make([]byte, total)
			offset := 0
			for _, part := range chunks {
				offset += copy(body[offset:], part)
			}
			return body, nil
		}
		if capacity < requestBodyReadMaxInitCap {
			capacity *= 2
			if capacity > requestBodyReadMaxInitCap {
				capacity = requestBodyReadMaxInitCap
			}
		}
	}
}

// ReadLenientJSONRequestBodyWithPrealloc reads a request body and normalizes
// JSON string control bytes before strict validation.
func ReadLenientJSONRequestBodyWithPrealloc(req *http.Request, maxNormalizedBytes int64) ([]byte, error) {
	body, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		return nil, err
	}
	return NormalizeLenientJSONRequestBody(body, maxNormalizedBytes)
}

func decompressRequestBody(encoding string, raw []byte, maxDecodedBytes int64, strict bool, budget RequestBodyReadBudget) ([]byte, error) {
	retained := int64(len(raw))
	readDecoded := func(reader io.Reader) ([]byte, error) {
		limit := maxDecodedBytes
		if strict {
			limit++
		}
		var decoded []byte
		var err error
		if budget == nil {
			decoded, err = io.ReadAll(io.LimitReader(reader, limit))
		} else {
			decoded, err = readRequestBodyChunks(io.LimitReader(reader, limit), requestBodyReadInitCap, limit, func(size int64) error {
				return budget("decode", max(retained, size))
			})
		}
		if strict && int64(len(decoded)) > maxDecodedBytes {
			return nil, &http.MaxBytesError{Limit: maxDecodedBytes}
		}
		return decoded, err
	}
	switch encoding {
	case "zstd":
		var options []zstd.DOption
		if budget != nil {
			window, err := zstdRequestWindow(raw, maxDecodedBytes)
			if err != nil {
				return nil, err
			}
			// The synchronous decoder allocates history before returning its
			// first output byte. Reserve every frame's largest window up front.
			retained = max(retained, window+(128<<10))
			if err := budget("decode", retained); err != nil {
				return nil, err
			}
			options = []zstd.DOption{
				zstd.WithDecodeBuffersBelow(0), zstd.WithDecoderConcurrency(1),
				zstd.WithDecoderMaxMemory(uint64(max(maxDecodedBytes, 1024))),
			}
		}
		dec, err := zstd.NewReader(bytes.NewReader(raw), options...)
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		return readDecoded(dec)
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		return readDecoded(gr)
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		return readDecoded(zr)
	default:
		return nil, errors.New("unsupported Content-Encoding")
	}
}

// zstdRequestWindow walks frame and block boundaries without decoding payloads.
// It also covers concatenated and skippable frames, so a later large window
// cannot allocate outside the request's reservation.
func zstdRequestWindow(raw []byte, maxDecodedBytes int64) (int64, error) {
	var largest uint64
	for len(raw) > 0 {
		var header zstd.Header
		if err := header.Decode(raw); err != nil {
			return 0, err
		}
		raw = raw[header.HeaderSize:]
		if header.Skippable {
			if uint64(header.SkippableSize) > uint64(len(raw)) {
				return 0, io.ErrUnexpectedEOF
			}
			raw = raw[header.SkippableSize:]
			continue
		}
		window := header.WindowSize
		if header.SingleSegment {
			window = max(header.FrameContentSize, 1024)
		}
		if window > uint64(max(maxDecodedBytes, 1024)) {
			return 0, &http.MaxBytesError{Limit: maxDecodedBytes}
		}
		largest = max(largest, window)
		for {
			if len(raw) < 3 {
				return 0, io.ErrUnexpectedEOF
			}
			block := uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16
			raw = raw[3:]
			size := int(block >> 3)
			switch (block >> 1) & 3 {
			case 1: // RLE blocks retain one wire byte.
				size = 1
			case 3:
				return 0, errors.New("invalid zstd block type")
			}
			if size > len(raw) {
				return 0, io.ErrUnexpectedEOF
			}
			raw = raw[size:]
			if block&1 != 0 {
				break
			}
		}
		if header.HasCheckSum {
			if len(raw) < 4 {
				return 0, io.ErrUnexpectedEOF
			}
			raw = raw[4:]
		}
	}
	return int64(largest), nil
}

// NormalizeLenientJSONRequestBody escapes raw control bytes that broken
// OpenAI-compatible clients sometimes place inside JSON strings.
func NormalizeLenientJSONRequestBody(body []byte, maxNormalizedBytes int64) ([]byte, error) {
	if maxNormalizedBytes <= 0 {
		maxNormalizedBytes = maxDecompressedBodySize
	}

	body = trimUTF8BOM(body)
	if len(body) == 0 {
		return body, nil
	}
	if int64(len(body)) > maxNormalizedBytes {
		return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
	}

	var out []byte
	inString := false
	escaped := false
	for i, b := range body {
		if inString && isJSONControlByte(b) {
			if out == nil {
				capHint := len(body) + 6
				if int64(capHint) > maxNormalizedBytes {
					capHint = int(maxNormalizedBytes)
				}
				out = make([]byte, 0, capHint)
				out = append(out, body[:i]...)
			}
			if int64(len(out)+6) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = appendJSONUnicodeEscape(out, b)
			escaped = false
			continue
		}

		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		}

		if out != nil {
			if int64(len(out)+1) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = append(out, b)
		}
	}
	if out != nil {
		return out, nil
	}
	return body, nil
}

func trimUTF8BOM(body []byte) []byte {
	if len(body) >= jsonUTF8BOMLen && body[0] == 0xef && body[1] == 0xbb && body[2] == 0xbf {
		return body[jsonUTF8BOMLen:]
	}
	return body
}

func isJSONControlByte(b byte) bool {
	return b < 0x20 || b == 0x7f
}

func appendJSONUnicodeEscape(dst []byte, b byte) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
}
