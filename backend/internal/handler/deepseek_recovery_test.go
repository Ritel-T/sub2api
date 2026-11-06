//go:build unit

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type deepSeekRecoveryTestUpstream struct {
	service.HTTPUpstream
	mu       sync.Mutex
	ids      []int64
	bodies   [][]byte
	response func(int, *http.Request) (int, string)
}

func (u *deepSeekRecoveryTestUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	u.ids = append(u.ids, id)
	u.bodies = append(u.bodies, body)
	n := len(u.ids)
	u.mu.Unlock()
	status, data := u.response(n, req)
	ct := "text/event-stream"
	if !strings.HasPrefix(data, "data:") {
		ct = "application/json"
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {ct}, "X-Request-Id": {fmt.Sprintf("attempt-%d", n)}}, Body: io.NopCloser(strings.NewReader(data))}, nil
}

func deepSeekChunk(delta, finish string) string {
	return fmt.Sprintf("data: {\"id\":\"source\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, finish)
}

func deepSeekSuccess() string {
	return deepSeekChunk(`{"content":"SUCCESS"}`, "null") +
		deepSeekChunk(`{}`, `"stop"`) +
		"data: {\"id\":\"source\",\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3,\"total_tokens\":15}}\n\ndata: [DONE]\n\n"
}

func deepSeekHandlerFixture(t *testing.T, protocol string, upstream *deepSeekRecoveryTestUpstream, billing ...service.UsageBillingRepository) (*OpenAIGatewayHandler, *gin.Context, *httptest.ResponseRecorder, *openAIWSUsageHandlerUsageLogRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(17)
	var accounts []service.Account
	for i := int64(1); i <= 4; i++ {
		accounts = append(accounts, service.Account{ID: i, Name: fmt.Sprintf("deepseek-%d", i), Platform: "deepseek", Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Priority: int(i), Concurrency: 1, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "sk-fixture", "base_url": "https://deepseek.example.test", "api_protocol": "chat_completions"}})
	}
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 16)}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	slots := service.NewConcurrencyService(&concurrencyCacheMock{acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }})
	var billingRepo service.UsageBillingRepository
	if len(billing) > 0 {
		billingRepo = billing[0]
	}
	svc := service.NewOpenAIGatewayService(openAIImagesFailoverAccountRepo{accounts: accounts}, nil, usage, billingRepo, nil, nil, nil, nil, cfg, nil, slots, service.NewBillingService(cfg, nil), nil, cache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(svc, slots, cache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	body := `{"model":"deepseek-flash","stream":true,"max_tokens":64000,"messages":[{"role":"user","content":"hello"}]}`
	path := "/v1/chat/completions"
	if protocol == "messages" {
		path = "/v1/messages"
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	core, observed := observer.New(zap.DebugLevel)
	c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
	t.Cleanup(func() {
		if t.Failed() {
			for _, entry := range observed.All() {
				t.Log(entry.Message, entry.ContextMap())
			}
		}
	})
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.ClientRequestID, "deepseek-recovery-test"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 99, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: "deepseek", RateMultiplier: 1}, User: &service.User{ID: 100}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
	return h, c, rec, usage
}

func TestDeepSeekRecoveryHandlersSwitchAfterUpstreamFailure(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		for _, failure := range []string{"http400", "error200", "cyber400", "cyber200", "partial_eof", "resource", "reasoning_only"} {
			t.Run(protocol+"/"+failure, func(t *testing.T) {
				u := &deepSeekRecoveryTestUpstream{response: func(n int, _ *http.Request) (int, string) {
					if n > 1 {
						return 200, deepSeekSuccess()
					}
					switch failure {
					case "http400":
						return 400, `{"error":{"message":"bad upstream configuration"}}`
					case "error200":
						return 200, `{"error":{"message":"broken upstream"}}`
					case "cyber400":
						return 400, `{"error":{"code":"cyber_policy","message":"private upstream detail"},"usage":{"prompt_tokens":100,"completion_tokens":9}}`
					case "cyber200":
						return 200, "data: {\"error\":{\"code\":\"cyber_policy\",\"message\":\"private upstream detail\"},\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":9}}\n\n"
					case "partial_eof":
						return 200, deepSeekChunk(`{"content":"PARTIAL"}`, "null")
					case "resource":
						return 200, deepSeekChunk(`{"content":"PARTIAL"}`, "null") + deepSeekChunk(`{}`, `"insufficient_system_resource"`) + "data: [DONE]\n\n"
					default:
						return 200, deepSeekChunk(`{"reasoning_content":"thinking"}`, "null") + deepSeekChunk(`{}`, `"length"`) + "data: [DONE]\n\n"
					}
				}}
				h, c, rec, usage := deepSeekHandlerFixture(t, protocol, u)
				if protocol == "chat" {
					h.ChatCompletions(c)
				} else {
					h.Messages(c)
				}
				require.Len(t, u.ids, 2, rec.Body.String())
				require.NotEqual(t, u.ids[0], u.ids[1])
				require.Contains(t, rec.Body.String(), "SUCCESS")
				if dir := os.Getenv("DEEPSEEK_RECOVERY_FIXTURES"); dir != "" {
					require.NoError(t, os.MkdirAll(dir, 0700))
					require.NoError(t, os.WriteFile(filepath.Join(dir, protocol+"-"+failure+".sse"), rec.Body.Bytes(), 0600))
				}
				require.NotContains(t, rec.Body.String(), "broken upstream")
				require.NotContains(t, rec.Body.String(), "private upstream detail")
				require.Len(t, usage.created, 1)
				bill := <-usage.created
				require.Equal(t, u.ids[1], bill.AccountID)
				require.Equal(t, 3, bill.OutputTokens)
				if failure == "partial_eof" || failure == "resource" || failure == "reasoning_only" {
					require.Contains(t, rec.Body.String(), "正在重新生成")
				}
				if protocol == "chat" {
					require.Equal(t, 1, strings.Count(rec.Body.String(), "data: [DONE]"))
				} else {
					require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_start"))
					require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_stop"))
				}
			})
		}
	}
}

func TestDeepSeekRecoveryHandlersBoundAttempts(t *testing.T) {
	u := &deepSeekRecoveryTestUpstream{response: func(int, *http.Request) (int, string) { return 503, `{"error":{"message":"overloaded"}}` }}
	h, c, rec, usage := deepSeekHandlerFixture(t, "chat", u)
	h.ChatCompletions(c)
	require.Len(t, u.ids, 3)
	require.Equal(t, u.ids[2], c.GetInt64(opsAccountIDKey))
	require.Len(t, usage.created, 0)
	require.Contains(t, rec.Body.String(), "error")
	seen := map[int64]bool{}
	for _, id := range u.ids {
		require.False(t, seen[id])
		seen[id] = true
	}
}

func TestDeepSeekRecoveryHandlersNeverReplayExposedTools(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			u := &deepSeekRecoveryTestUpstream{response: func(int, *http.Request) (int, string) {
				return 200, deepSeekChunk(`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"path\":"}}]}`, "null")
			}}
			h, c, rec, usage := deepSeekHandlerFixture(t, protocol, u)
			if protocol == "chat" {
				h.ChatCompletions(c)
			} else {
				h.Messages(c)
			}
			require.Len(t, u.ids, 1, rec.Body.String())
			require.Contains(t, rec.Body.String(), "error")
			require.LessOrEqual(t, len(usage.created), 1)
		})
	}
}

func TestDeepSeekRecoveryScope(t *testing.T) {
	require.True(t, deepSeekRecoveryEnabled("deepseek", "deepseek-flash", []byte(`{}`)))
	require.False(t, deepSeekRecoveryEnabled(service.PlatformOpenAI, "deepseek-flash", []byte(`{}`)))
	require.False(t, deepSeekRecoveryEnabled("deepseek", "kimi-k2", []byte(`{}`)))
	require.False(t, deepSeekRecoveryEnabled("deepseek", "deepseek-flash", []byte(`{"n":2}`)))
}

type deepSeekRecoveryBilling struct {
	service.UsageBillingRepository
	events       []service.UsageBillingEvent
	applications int
	failApply    bool
}

func (b *deepSeekRecoveryBilling) StageUsageBilling(_ context.Context, c *service.UsageBillingCommand, u *service.UsageLog) error {
	b.events = append(b.events, service.UsageBillingEvent{Version: 1, Command: *c, Usage: *u})
	return nil
}
func (b *deepSeekRecoveryBilling) ApplyStagedUsageBilling(context.Context, service.UsageBillingEventKey) (*service.UsageBillingRecovery, error) {
	b.applications++
	if b.failApply {
		return nil, fmt.Errorf("fixture billing unavailable")
	}
	return &service.UsageBillingRecovery{Event: b.events[len(b.events)-1], Result: service.UsageBillingApplyResult{Applied: true}}, nil
}
func (*deepSeekRecoveryBilling) PendingUsageBilling(context.Context, int) ([]service.UsageBillingEventKey, error) {
	return nil, nil
}
func (*deepSeekRecoveryBilling) AcknowledgeUsageBillingCache(context.Context, service.UsageBillingEventKey) error {
	return nil
}

func TestDeepSeekRecoveryOnlyFinalAttemptStagesCustomerCharge(t *testing.T) {
	for _, billError := range []bool{false, true} {
		t.Run(fmt.Sprint(billError), func(t *testing.T) {
			u := &deepSeekRecoveryTestUpstream{response: func(n int, _ *http.Request) (int, string) {
				if n == 1 {
					return 200, deepSeekChunk(`{"content":"partial"}`, "null") + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":99}}\n\n"
				}
				return 200, deepSeekSuccess()
			}}
			b := &deepSeekRecoveryBilling{failApply: billError}
			h, c, rec, _ := deepSeekHandlerFixture(t, "chat", u, b)
			// Exercise the real coordinator + regular durable billing path after
			// admission. The fixture deliberately supplies no real DB or secrets.
			h.cfg.RunMode = "standard"
			key, _ := middleware2.GetAPIKeyFromContext(c)
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			ctx, at := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), key.GroupID)
			c.Request = c.Request.WithContext(ctx)
			h.runDeepSeekRecovery(c, deepSeekRecoveryRequest{apiKey: key, model: "deepseek-flash", forwardModel: "deepseek-flash", protocol: "chat", stream: true, body: body, pricingAt: at,
				forward: func(ctx context.Context, child *gin.Context, a *service.Account) (*service.OpenAIForwardResult, error) {
					return h.gatewayService.ForwardAsChatCompletions(ctx, child, a, body, "", "")
				}}, logger.FromContext(ctx))
			require.Len(t, u.ids, 2, rec.Body.String())
			require.Len(t, b.events, 1)
			require.Equal(t, 1, b.applications)
			cmd := b.events[0].Command
			require.Equal(t, u.ids[1], cmd.AccountID)
			require.Equal(t, 3, cmd.OutputTokens)
			require.Greater(t, cmd.BalanceCost, 0.0)
			require.Equal(t, "client:deepseek-recovery-test", cmd.RequestID)
			require.Contains(t, rec.Body.String(), "SUCCESS")
		})
	}
}

func TestDeepSeekRecoveryHandlersPreserveLengthAndCompleteTools(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		for _, tool := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tool=%v", protocol, tool), func(t *testing.T) {
				first, finish := `{"content":"useful partial answer"}`, `"length"`
				if tool {
					first = `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}`
					finish = `"tool_calls"`
				}
				u := &deepSeekRecoveryTestUpstream{response: func(int, *http.Request) (int, string) {
					return 200, deepSeekChunk(first, "null") + deepSeekChunk(`{}`, finish) + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n"
				}}
				h, c, rec, usage := deepSeekHandlerFixture(t, protocol, u)
				if protocol == "chat" {
					h.ChatCompletions(c)
				} else {
					h.Messages(c)
				}
				require.Len(t, u.ids, 1, rec.Body.String())
				require.Len(t, usage.created, 1)
				require.NotContains(t, rec.Body.String(), "正在重新生成")
				if tool {
					require.Contains(t, rec.Body.String(), "read_file")
				} else if protocol == "chat" {
					require.Contains(t, rec.Body.String(), `"finish_reason":"length"`)
				} else {
					require.Contains(t, rec.Body.String(), `"stop_reason":"max_tokens"`)
				}
			})
		}
	}
}

func TestDeepSeekRecoveryHandlersPreserveInputAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			var cancel context.CancelFunc
			u := &deepSeekRecoveryTestUpstream{response: func(n int, _ *http.Request) (int, string) {
				if n == 1 {
					if cancelled {
						cancel()
					}
					return 400, `{"error":{"message":"upstream"}}`
				}
				return 200, deepSeekSuccess()
			}}
			h, c, rec, _ := deepSeekHandlerFixture(t, "chat", u)
			ctx, stop := context.WithCancel(c.Request.Context())
			cancel = stop
			defer stop()
			c.Request = c.Request.WithContext(ctx)
			body := `{"model":"deepseek-flash","stream":true,"max_tokens":1,"messages":[{"role":"user","content":[{"type":"text","text":"what is here"},{"type":"image_url","image_url":{"url":"data:image/png;base64,c3ludGhldGlj"}}]}]}`
			c.Request.Body = io.NopCloser(strings.NewReader(body))
			c.Request.ContentLength = int64(len(body))
			h.ChatCompletions(c)
			if cancelled {
				require.Len(t, u.ids, 1)
			} else {
				require.Len(t, u.ids, 2, rec.Body.String())
			}
			for _, out := range u.bodies {
				require.Equal(t, int64(1), gjson.GetBytes(out, "max_tokens").Int())
				require.Contains(t, string(out), "data:image/png;base64,c3ludGhldGlj")
			}
		})
	}
}

func TestDeepSeekRecoveryHandlersStructuredOutputDoesNotAppendRestart(t *testing.T) {
	u := &deepSeekRecoveryTestUpstream{response: func(int, *http.Request) (int, string) { return 200, deepSeekChunk(`{"content":"{\"x\":"}`, "null") }}
	h, c, rec, _ := deepSeekHandlerFixture(t, "chat", u)
	body := `{"model":"deepseek-flash","stream":true,"response_format":{"type":"json_object"},"messages":[{"role":"user","content":"give JSON"}]}`
	c.Request.Body = io.NopCloser(strings.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	h.ChatCompletions(c)
	require.Len(t, u.ids, 1, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "正在重新生成")
}

func TestDeepSeekRecoveryOperationObservation(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		for _, scenario := range []string{"recovered", "tool", "reasoning_only", "budget"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				u := &deepSeekRecoveryTestUpstream{response: func(n int, _ *http.Request) (int, string) {
					if scenario == "recovered" && n == 1 {
						return 502, `{"error":{"message":"upstream failed"}}`
					}
					if scenario == "tool" {
						return 200, deepSeekChunk(`{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"read","arguments":"{}"}}]}`, "null") + deepSeekChunk(`{}`, `"tool_calls"`) + "data: [DONE]\n\n"
					}
					if scenario == "reasoning_only" {
						return 200, deepSeekChunk(`{"reasoning_content":"think"}`, "null") + deepSeekChunk(`{}`, `"stop"`) + "data: [DONE]\n\n"
					}
					if scenario == "budget" {
						return 200, deepSeekChunk(`{"content":"partial"}`, "null") + deepSeekChunk(`{}`, `"length"`) + "data: [DONE]\n\n"
					}
					return 200, deepSeekSuccess()
				}}
				h, c, _, _ := deepSeekHandlerFixture(t, protocol, u)
				collector := requesttiming.New(time.Now(), -1)
				c.Request = c.Request.WithContext(requesttiming.With(c.Request.Context(), collector))
				requesttiming.BeginOperation(c.Request.Context(), protocol, "http", "application")
				var snapshot requesttiming.Snapshot
				collector.WhenFinished(func(s requesttiming.Snapshot) { snapshot = s })
				if protocol == "chat" {
					h.ChatCompletions(c)
				} else {
					h.Messages(c)
				}
				collector.Finish(200, false)
				require.NotNil(t, snapshot.Operation)
				op := snapshot.Operation
				switch scenario {
				case "recovered":
					require.Equal(t, "completed", op.FinalOutcome)
					require.True(t, op.Recovered)
					require.Equal(t, 2, op.AttemptCount)
				case "tool":
					require.Equal(t, "completed", op.FinalOutcome)
					require.Equal(t, "tool", op.ContentKind)
					require.NotNil(t, op.FirstUsableMS)
				case "reasoning_only":
					require.Equal(t, "incomplete", op.FinalOutcome)
					require.Equal(t, "reasoning_only", op.Reason)
					require.Nil(t, op.FirstUsableMS)
				case "budget":
					require.Equal(t, "incomplete", op.FinalOutcome)
					require.Equal(t, "output_budget", op.Reason)
				}
			})
		}
	}
}
