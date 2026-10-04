package service

import (
	"context"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type delayedBorrowFinalSendUpstream struct {
	*borrowSafetyProvider
	afterVerification func()
	business          int
}
type finalBorrowSendRepo struct {
	AccountRepository
	account *Account
}

func (r *finalBorrowSendRepo) GetOpenAITurnAdmission(context.Context, int64) (*Account, *Account, error) {
	return r.account, nil, nil
}
func (p *delayedBorrowFinalSendUpstream) Do(req *http.Request, proxy string, id int64, n int) (*http.Response, error) {
	return p.DoWithTLS(req, proxy, id, n, nil)
}
func (p *delayedBorrowFinalSendUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	// Stand in for the repository's six verification requests. No business
	// bytes have been sent when the account changes here.
	if p.afterVerification != nil {
		p.afterVerification()
	}
	if err := CheckGatewayBorrowFinalSend(req); err != nil {
		return nil, err
	}
	p.business++
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"status\":\"completed\"}}\n\n"))}, nil
}

func TestGatewayBorrowRechecksAccountAfterSlowVerification(t *testing.T) {
	for _, change := range []string{"unchanged", "pause", "proxy", "policy", "token", "model quota", "auth header"} {
		t.Run(change, func(t *testing.T) {
			gateway, account, provider := borrowSafetyService()
			latest := *account
			latest.Credentials = maps.Clone(account.Credentials)
			latest.Extra = maps.Clone(account.Extra)
			repo := &finalBorrowSendRepo{account: account}
			gateway.accountRepo = repo
			upstream := &delayedBorrowFinalSendUpstream{borrowSafetyProvider: provider}
			upstream.afterVerification = func() {
				switch change {
				case "pause":
					latest.Schedulable = false
				case "proxy":
					id := int64(999)
					latest.ProxyID = &id
				case "policy":
					latest.Extra[GatewayBorrowModelsKey] = []string{}
				case "token":
					latest.Credentials["access_token"] = "changed-fixture-token"
				case "model quota":
					latest.Extra[modelRateLimitsKey] = map[string]any{"gpt-6-astra": map[string]any{"rate_limit_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}
				}
				repo.account = &latest
			}
			gateway.httpUpstream = upstream
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader("{\"model\":\"gpt-6-astra\"}"))
			req.Header.Set("Authorization", "Bearer "+account.GetOpenAIAccessToken())
			if change == "auth header" {
				req.Header.Set("Authorization", "Bearer wrong-fixture-token")
			}
			resp, err := gateway.doOpenAIProxyAttempt(req, account, runtimeProxyEgress{})
			if change == "unchanged" {
				require.NoError(t, err)
				require.Equal(t, 1, upstream.business)
				_ = resp.Body.Close()
			} else {
				require.Error(t, err)
				require.Zero(t, upstream.business)
			}
		})
	}
}

func TestGatewayBorrowFinalCheckSkipsValidationProbe(t *testing.T) {
	calls := 0
	ctx := WithGatewayBorrowFinalSendCheck(context.Background(), func(*http.Request) error { calls++; return denyOpenAITurn("fixture") })
	ctx = context.WithValue(ctx, openAICodexStateProbeContextKey{}, true)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, CheckGatewayBorrowFinalSend(req))
	require.Zero(t, calls)
}
