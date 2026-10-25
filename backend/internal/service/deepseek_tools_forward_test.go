package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/deepseektools"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Every credential in this fixture is an invented constant. httpUpstreamRecorder
// consumes every outbound request; no token lookup or external HTTP is needed.
func deepSeekToolsForwardFixture(t *testing.T, pat bool, upstream *httpUpstreamRecorder) (*OpenAIGatewayService, *Account, *gin.Context, *httptest.ResponseRecorder, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body := []byte(`{"id":"fixture-deepseek-search","model":"gpt-6-astra","commands":{"search_query":[{"q":"fixture query"}]}}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/search", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	account := &Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "fixture-oauth-token",
			"chatgpt_account_id": "fixture-chatgpt-account",
		},
	}
	if pat {
		account.Credentials["access_token"] = "at-fixture-test-only"
		account.Credentials["auth_mode"] = OpenAIAuthModePersonalAccessToken
	}
	return &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}, account, c, rec, body
}

func TestDeepSeekToolsForwardHTTP400IsSanitizedOnlyForBridge(t *testing.T) {
	const wire = `{"error":{"type":"invalid_request_error","message":"FIXTURE_UPSTREAM_PRIVATE_DETAIL"},"encrypted_output":"FIXTURE_ENCRYPTED_OUTPUT"}`
	for _, pat := range []bool{false, true} {
		name := "native"
		if pat {
			name = "pat"
		}
		t.Run(name, func(t *testing.T) {
			for _, bridge := range []bool{false, true} {
				mode := "ordinary_alpha"
				if bridge {
					mode = "bridge"
				}
				t.Run(mode, func(t *testing.T) {
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusBadRequest,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(wire)),
					}}
					svc, account, c, rec, body := deepSeekToolsForwardFixture(t, pat, upstream)
					ctx := context.Background()
					if bridge {
						ctx = WithAlphaSearchResponseTransform(ctx, func([]byte) ([]byte, bool, error) {
							t.Fatal("an HTTP failure must not enter the success-result adapter")
							return nil, false, nil
						})
					}
					result, err := svc.ForwardAlphaSearch(ctx, c, account, body)
					require.NoError(t, err)
					require.Nil(t, result, "HTTP 400 must not return a billable search result")
					require.Equal(t, http.StatusBadRequest, rec.Code)
					require.Len(t, upstream.requests, 1)
					if bridge {
						require.True(t, gjson.Valid(rec.Body.String()), "bridge errors must be local JSON")
						require.True(t, gjson.Get(rec.Body.String(), "error").IsObject())
						require.NotEmpty(t, gjson.Get(rec.Body.String(), "error.message").String())
						require.NotContains(t, rec.Body.String(), "encrypted_output")
						require.NotContains(t, rec.Body.String(), "FIXTURE_ENCRYPTED_OUTPUT")
						require.NotContains(t, rec.Body.String(), "FIXTURE_UPSTREAM_PRIVATE_DETAIL")
					} else {
						require.Equal(t, wire, rec.Body.String(), "ordinary alpha must preserve its error passthrough")
					}
					wantURL := chatgptCodexAlphaSearchURL
					if pat {
						wantURL = chatgptCodexURL
					}
					require.Equal(t, wantURL, upstream.lastReq.URL.String(), "exercise both native and PAT forwarding branches")
				})
			}
		})
	}
}

func TestDeepSeekToolsForwardTransportErrorIsTerminal(t *testing.T) {
	failures := map[string]error{
		"unexpected_eof":     io.ErrUnexpectedEOF,
		"wrapped_post_error": &url.Error{Op: "Post", URL: "https://fixture.invalid/search", Err: io.ErrUnexpectedEOF},
	}
	for _, pat := range []bool{false, true} {
		name := "native"
		if pat {
			name = "pat"
		}
		t.Run(name, func(t *testing.T) {
			for failureName, failure := range failures {
				t.Run(failureName, func(t *testing.T) {
					upstream := &httpUpstreamRecorder{err: failure}
					svc, account, c, rec, body := deepSeekToolsForwardFixture(t, pat, upstream)
					ctx := WithAlphaSearchResponseTransform(context.Background(), func([]byte) ([]byte, bool, error) {
						t.Fatal("transport failure must not enter the response adapter")
						return nil, false, nil
					})
					result, err := svc.ForwardAlphaSearch(ctx, c, account, body)
					require.Error(t, err)
					require.Nil(t, result)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover), "an unknown POST outcome must never authorize handler failover")
					require.Len(t, upstream.requests, 1)
					require.NotEmpty(t, upstream.lastBody, "the fixture consumed the POST before losing its response")
					require.False(t, c.Writer.Written())
					require.Empty(t, rec.Body.String())
				})
			}
		})
	}
}

func TestDeepSeekToolsForwardNativeResultControlsBillingBeforeWrite(t *testing.T) {
	request, err := deepseektools.ParseRequest("search", []byte(`{"query":"fixture query","need_content":true}`))
	require.NoError(t, err)
	cases := []struct {
		name, wire string
		calls      int
		wantError  bool
	}{
		{"native_output_with_empty_results", `{"results":[],"output":"Fixture page (https://example.com/page)\nL0: Actual fixture page text","encrypted_output":"FIXTURE_SECRET"}`, 1, false},
		{"confirmed_empty", `{"results":[],"output":"","encrypted_output":"FIXTURE_SECRET"}`, 0, false},
		{"unrecognized_output", `{"results":[],"output":"Unstructured fixture answer","encrypted_output":"FIXTURE_SECRET"}`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(tc.wire)),
			}}
			svc, account, c, rec, body := deepSeekToolsForwardFixture(t, false, upstream)
			transforms := 0
			ctx := WithAlphaSearchResponseTransform(context.Background(), func(raw []byte) ([]byte, bool, error) {
				transforms++
				require.False(t, c.Writer.Written(), "conversion must precede all success bytes")
				return deepseektools.AdaptResponse(raw, request)
			})
			result, err := svc.ForwardAlphaSearch(ctx, c, account, body)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, 1, transforms)
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, result)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover))
				require.False(t, c.Writer.Written())
				require.Empty(t, rec.Body.String())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.calls, result.WebSearchCalls)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "openai", gjson.Get(rec.Body.String(), "provider").String())
			require.Len(t, gjson.Get(rec.Body.String(), "results").Array(), tc.calls)
			require.NotContains(t, rec.Body.String(), "encrypted_output")
			require.NotContains(t, rec.Body.String(), "FIXTURE_SECRET")
		})
	}
}

func TestDeepSeekToolsCallerRateMultiplierForUsage(t *testing.T) {
	factor := 5.0
	account := &Account{ID: 42, GroupRateMultiplier: &factor}
	for _, tc := range []struct {
		name     string
		preserve bool
		want     float64
	}{
		{"bridge_preserves_caller_0_6", true, 0.6},
		{"ordinary_alpha_retains_account_factor", false, 3.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &OpenAIRecordUsageInput{Account: account, PreserveCallerGroupMultiplier: tc.preserve}
			got := callerRateMultiplierForUsage(0.6, input.Account, input.PreserveCallerGroupMultiplier)
			require.InDelta(t, tc.want, got, 1e-12)
			require.Same(t, account, input.Account, "retain the actual upstream account for statistics")
			require.Same(t, &factor, account.GroupRateMultiplier)
			require.Equal(t, 5.0, account.UserGroupRateMultiplier(), "billing must not mutate the shared account")
		})
	}
}

type deepSeekToolsForwardGroupRepo struct {
	GroupRepository
	group *Group
}

func (r *deepSeekToolsForwardGroupRepo) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	if r.group == nil || r.group.ID != id {
		return nil, errors.New("fixture group not found")
	}
	return r.group, nil
}

func TestDeepSeekToolsSchedulingRejectsSimpleModeWithValidPool(t *testing.T) {
	pool := &Group{ID: 12, Platform: PlatformOpenAI, Status: StatusActive}
	caller := &Group{ID: 17, Platform: PlatformDeepseek, Status: StatusActive}
	ctx := context.WithValue(context.Background(), ctxkey.Group, caller)
	for _, mode := range []string{config.RunModeStandard, config.RunModeSimple} {
		t.Run(mode, func(t *testing.T) {
			svc := &OpenAIGatewayService{
				cfg:            &config.Config{RunMode: mode},
				channelService: &ChannelService{groupRepo: &deepSeekToolsForwardGroupRepo{group: pool}},
			}
			selected, err := svc.DeepSeekToolsSchedulingContext(ctx, 12)
			if mode == config.RunModeSimple {
				require.Error(t, err, "Simple mode must not expand a configured pool into all OpenAI accounts")
				require.Nil(t, selected)
			} else {
				require.NoError(t, err)
				require.Same(t, caller, selected.Value(ctxkey.Group))
			}
			require.Same(t, caller, ctx.Value(ctxkey.Group))
		})
	}
}
