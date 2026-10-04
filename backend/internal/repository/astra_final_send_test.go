package repository

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBorrowFinalSendCallbackRunsAfterValidationBeforeBusiness(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	probes, business, callbacks := 0, 0, 0
	allowed := true
	wrapper := &astraRoutingUpstream{cfg: cfg}
	wrapper.delegate = gatewayPinDelegate{call: func(r *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(raw, "input.0.content.0.text").String() != "" {
			probes++
			if probes == 6 {
				allowed = false
			}
		} else {
			business++
		}
		return pinResponse(""), nil
	}}
	wrapper.SetAstraGatewayPreparer(func(context.Context, int64) error { return nil })
	now := time.Now()
	route := codexGatewayRouteFromResponse(pinResponse("__oailb=fixture; Path=/; Secure; Max-Age=230"), "/backend-api/codex/responses", now)
	wrapper.current(t.Context()).recordSource(299, now, route, true)
	ctx := service.WithGatewayBorrowFinalSendCheck(t.Context(), func(r *http.Request) error {
		callbacks++
		cookie, err := r.Cookie("__oailb")
		require.NoError(t, err)
		require.Equal(t, "fixture", cookie.Value)
		if !allowed {
			return errors.New("fixture_account_changed")
		}
		return nil
	})
	request := func() *http.Request {
		r, _ := http.NewRequestWithContext(ctx, "POST", "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
		r.Header.Set("Authorization", "Bearer target")
		return r
	}
	_, err := wrapper.Do(request(), "target", 300, 1)
	require.ErrorContains(t, err, "fixture_account_changed")
	require.Equal(t, 6, probes)
	require.Zero(t, business)
	require.Equal(t, 1, callbacks, "probes bypass final business callback")
	allowed = true
	resp, err := wrapper.Do(request(), "target", 300, 1)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 6, probes, "cached route needs no revalidation")
	require.Equal(t, 1, business)
	require.Equal(t, 2, callbacks)
}
