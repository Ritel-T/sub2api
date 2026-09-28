package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

type excelBPSAttachmentError struct {
	status     int
	retryAfter string
	kind       string
	cause      error
}

func excelBPSAttachmentFailureKind(err error) string {
	var attachment *excelBPSAttachmentError
	if errors.As(err, &attachment) {
		if attachment.kind != "" {
			return attachment.kind
		}
		return "upstream_http"
	}
	if errors.Is(err, basispoints.ErrAttachmentBusy) {
		return "upload_capacity"
	}
	if errors.Is(err, context.Canceled) {
		return "request_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "invalid_attachment_response"
}

func (e *excelBPSAttachmentError) Error() string {
	return fmt.Sprintf("excel BPS attachment returned HTTP %d", e.status)
}

// Retain cancellation and timeout causes without exposing transport URLs or credentials.
func (e *excelBPSAttachmentError) Unwrap() error { return e.cause }

func excelBPSAttachmentTransportError(err error) error {
	kind := "transport_error"
	var timeout net.Error
	if errors.Is(err, context.Canceled) {
		kind = "request_canceled"
	} else if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		kind = "timeout"
	}
	return &excelBPSAttachmentError{status: http.StatusBadGateway, kind: kind, cause: err}
}

func (s *OpenAIGatewayService) uploadExcelBPSAttachment(ctx context.Context, account *Account, token, accountID, proxyURL string, img basispoints.InlineAttachment) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileExcelBPS))
	reader, contentType, length, err := img.Multipart()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, basispoints.AttachmentsURL, reader)
	if err != nil {
		return "", err
	}
	auth, err := newExcelBPSRequest(ctx, nil, token, accountID)
	if err != nil {
		return "", err
	}
	req.Header = auth.Header
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.ContentLength = length
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", excelBPSAttachmentTransportError(err)
	}
	// Release the account's upstream connection slot before starting Responses.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode == http.StatusTooManyRequests {
			s.recordExcelBPSRateLimit(ctx, account, resp.Header, raw)
		}
		s.handleExcelBPSUnauthorized(ctx, account, resp.StatusCode, resp.Header, raw)
		status := resp.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return "", &excelBPSAttachmentError{status: status, retryAfter: resp.Header.Get("Retry-After"), cause: readErr}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return "", excelBPSAttachmentTransportError(err)
	}
	if len(raw) > 64<<10 {
		return "", fmt.Errorf("invalid Excel BPS attachment response")
	}
	var result struct {
		OpenAIFileID string `json:"openai_file_id"`
	}
	if json.Unmarshal(raw, &result) != nil || !basispoints.ValidAttachmentID(result.OpenAIFileID) {
		return "", fmt.Errorf("invalid Excel BPS attachment response")
	}
	return result.OpenAIFileID, nil
}
