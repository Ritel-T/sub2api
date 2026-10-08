package repository

import (
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
)

// Body feedback only changes the protocol for later independent requests. It
// never retries a response whose headers or body have already been received.
func (s *httpUpstreamService) wrapOpenAIHTTP2Feedback(req *http.Request, resp *http.Response, profile service.HTTPUpstreamProfile, entry *upstreamClientEntry) {
	settings := s.resolveOpenAIHTTP2Settings()
	if profile != service.HTTPUpstreamProfileOpenAI || entry.protocolMode != upstreamProtocolModeOpenAIH2 || !isHTTPProxyKey(entry.proxyKey) || !settings.enabled || !settings.allowProxyFallbackToHTTP1 || resp == nil || resp.ProtoMajor != 2 || resp.Body == nil {
		return
	}
	resp.Body = &http2FeedbackBody{
		ReadCloser: resp.Body,
		failed: func(err error) {
			if req.Context().Err() == nil {
				s.recordOpenAIHTTP2Failure(profile, entry.protocolMode, entry.proxyKey, err)
			}
		},
		succeeded: func() {
			if req.Context().Err() == nil {
				s.recordOpenAIHTTP2Success(profile, entry.protocolMode, entry.proxyKey)
			}
		},
	}
}

type http2FeedbackBody struct {
	io.ReadCloser
	trace     *transportdiag.Trace
	once      sync.Once
	failed    func(error)
	succeeded func()
}

func (b *http2FeedbackBody) Read(p []byte) (int, error) {
	if b.trace != nil {
		b.trace.MarkResponseBodyRead()
	}
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(func() {
			// Normal EOF proves the transport body was consumed. SSE terminal
			// events remain the bridge's responsibility; headers or early Close
			// alone never reset the H2 error window.
			if errors.Is(err, io.EOF) {
				if b.succeeded != nil {
					b.succeeded()
				}
			} else if b.failed != nil {
				b.failed(err)
			}
		})
	}
	return n, err
}
