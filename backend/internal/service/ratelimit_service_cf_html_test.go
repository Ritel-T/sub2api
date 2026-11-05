//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const cfAccessDeniedHTML = `<!doctype html>
<html><head><title>Access denied | upstream.example used Cloudflare to restrict access | upstream.example | Cloudflare</title></head>
<body>Access denied</body></html>`

func TestCloudflareAccessDeniedHTMLSkipsAccountPenalty(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom=%t", custom), func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			counter := &countingOpenAI403CounterCache{}
			blocker := &runtimeBlockRecorder{}
			svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			svc.SetOpenAI403CounterCache(counter)
			svc.SetAccountRuntimeBlocker(blocker)
			account := &Account{ID: 901, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"custom_error_codes_enabled": custom, "custom_error_codes": []any{float64(403)},
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules":   []any{map[string]any{"error_code": float64(403), "keywords": []any{"Access denied"}, "duration_minutes": float64(10)}},
			}}
			body := []byte(cfAccessDeniedHTML)
			require.Equal(t, ErrorPolicySkipped, svc.CheckErrorPolicy(context.Background(), account, http.StatusForbidden, body))
			require.False(t, svc.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, body))
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.tempCalls)
			require.Zero(t, counter.increments)
			require.Empty(t, blocker.accounts)
		})
	}
}

func TestCloudflareAccessDeniedHTMLClassification(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"observed template", cfAccessDeniedHTML, true},
		{"truncated after title", `<!doctype html><html><head><title>Access denied | upstream.example used Cloudflare to restrict access | upstream.example | Cloudflare</title>`, true},
		{"uppercase", `  <!DOCTYPE HTML><HTML><TITLE>ACCESS DENIED | UPSTREAM.EXAMPLE USED CLOUDFLARE TO RESTRICT ACCESS</TITLE></HTML>`, true},
		{"generic HTML forbidden", `<html><title>403 Forbidden</title></html>`, false},
		{"permission HTML with CF footer", `<html><title>Access denied</title><body>API key lacks model permissions. Powered by Cloudflare</body></html>`, false},
		{"JSON quotes page", `{"error":{"message":"Access denied | upstream.example used Cloudflare to restrict access"}}`, false},
		{"plain text quotes page", `Access denied | upstream.example used Cloudflare to restrict access`, false},
		{"CF origin error", `{"cloudflare_error":true,"error_code":502,"error_name":"origin_bad_gateway"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, isCloudflareBotBlockResponse([]byte(tc.body))) })
	}
}

func TestCloudflareHTMLFixPreservesAnthropicCredentialAndPermissionFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"revoked key", `{"error":{"type":"authentication_error","message":"API key revoked"}}`, 403},
		{"insufficient balance", `{"error":{"type":"billing_error","message":"insufficient balance"}}`, 403},
		{"model permission", `{"error":{"type":"permission_error","message":"Model access denied"}}`, 403},
		{"HTML permission", `<html><title>Access denied</title><body>API key lacks permissions. Powered by Cloudflare</body></html>`, 403},
		{"JSON quotes CF page", `{"error":{"message":"Access denied | upstream.example used Cloudflare to restrict access"}}`, 403},
		{"401 unchanged", cfAccessDeniedHTML, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{ID: 902, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
			require.True(t, svc.HandleUpstreamError(context.Background(), account, tc.status, http.Header{}, []byte(tc.body)))
			require.Equal(t, 1, repo.setErrorCalls)
		})
	}
}
