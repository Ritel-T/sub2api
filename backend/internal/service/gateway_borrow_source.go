package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func gatewayBorrowSourceAccountBlock(account *Account, now time.Time) (string, *time.Time) {
	reason, retry := gatewayBorrowAccountBlock(account, "gpt-6-astra", now)
	switch reason {
	case "target_account_rate_limited":
		return "source_account_rate_limited", retry
	case "target_account_unavailable":
		return "source_account_unavailable", retry
	}
	if account != nil && account.RequiresGatewayBorrowUpstream("gpt-6-astra") {
		return "source_account_unavailable", nil
	}
	return "", nil
}

type gatewayBorrowSourceRetry struct {
	credential [32]byte
	until      time.Time
}

func (s *AccountTestService) gatewayBorrowSourceBlock(account *Account, now time.Time) (string, *time.Time) {
	if reason, retry := gatewayBorrowSourceAccountBlock(account, now); reason != "" {
		return reason, retry
	}
	if raw, found := s.gatewayBorrowSourceRetry.Load(account.ID); found {
		retry, ok := raw.(gatewayBorrowSourceRetry)
		if ok && retry.credential == gatewayBorrowCredentialFingerprint(account) && now.Before(retry.until) {
			return "source_account_rate_limited", &retry.until
		}
		s.gatewayBorrowSourceRetry.Delete(account.ID)
	}
	return "", nil
}

// Automatic source acquisition is a native, bounded request. A donor's short
// success cannot authorize recovery of its other models or clear any limit.
func (s *AccountTestService) prepareAutomaticGatewayBorrowSource(ctx context.Context, id int64) error {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return errors.New("source_account_unavailable")
	}
	if reason, _ := s.gatewayBorrowSourceBlock(account, time.Now()); reason != "" {
		return errors.New(reason)
	}
	account, headers, profile, err := s.prepareGatewayBorrowAccountIdentity(ctx, account, "gpt-6-astra")
	if err != nil {
		return err
	}
	if reason, _ := s.gatewayBorrowSourceBlock(account, time.Now()); reason != "" {
		return errors.New(reason)
	}
	ctx, cancel := context.WithTimeout(WithAstraSourceAcquisition(ctx), 50*time.Second)
	defer cancel()
	body, err := json.Marshal(createPelicanOpenAIPayload("gpt-6-astra", true, "Reply with OK only.", "medium"))
	if err != nil {
		return errors.New("source_test_failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("source_test_failed")
	}
	req.Host, req.Header = "chatgpt.com", headers
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	// A source uses its own native credentials and must never inherit a target
	// policy or go through a plugin before Cookie capture.
	response, err := s.httpUpstream.DoWithTLS(req, openAIAccountProxyURL(account), id, account.Concurrency, profile)
	if err != nil {
		return errors.New("source_test_failed")
	}
	if response == nil || response.Body == nil {
		return errors.New("source_test_failed")
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, openAICodexStateProbeMaxBody+1))
	if err != nil || len(raw) > openAICodexStateProbeMaxBody {
		return errors.New("source_test_failed")
	}
	quotaPayload := raw
	if response.StatusCode == http.StatusOK {
		quotaPayload = openAICodexStateStreamErrorPayload(raw)
	}
	quota := response.StatusCode == http.StatusTooManyRequests
	for _, path := range []string{"error.code", "error.type", "response.error.code", "response.error.type"} {
		code := gjson.GetBytes(quotaPayload, path).String()
		quota = quota || code == "usage_limit_reached" || code == "rate_limit_exceeded" || code == "rate_limit_error"
	}
	if quota {
		// This is provider quota evidence, never a successful recovery. Use the
		// existing reset parser; missing reset metadata gets only a short source
		// retry window rather than an invented full quota reset.
		fresh, readErr := s.accountRepo.GetByID(ctx, id)
		if readErr != nil || fresh == nil || gatewayBorrowCredentialFingerprint(fresh) != gatewayBorrowCredentialFingerprint(account) {
			return errors.New("source_account_unavailable")
		}
		// Do not shorten a newer native restriction. The no-reset fallback is
		// internal to source acquisition because SetModelRateLimit would replace
		// an administrator's concurrent longer model cooldown.
		if reason, _ := gatewayBorrowSourceAccountBlock(fresh, time.Now()); reason == "" {
			s.reconcileOpenAI429State(ctx, fresh, response.Header, quotaPayload)
		}
		s.gatewayBorrowSourceRetry.Store(id, gatewayBorrowSourceRetry{credential: gatewayBorrowCredentialFingerprint(fresh), until: time.Now().Add(5 * time.Minute)})
		return errors.New("source_account_rate_limited")
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return errors.New("source_account_unavailable")
	}
	if response.StatusCode != http.StatusOK || validateCodexProbeResponse(raw, "gpt-6-astra") != nil || strings.TrimSpace(gatewayBorrowProbeText(raw)) == "" {
		return errors.New("source_test_failed")
	}
	return nil
}
