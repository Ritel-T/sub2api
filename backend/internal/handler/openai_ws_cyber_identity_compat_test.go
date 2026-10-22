package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real downstream/upstream sockets reproduce the pooled connection header/body
// mismatch. Only an actual upstream cyber event may trip the connection gate.
func TestOpenAIResponsesWebSocketCyberIdentityCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name         string
		strict       bool
		cyberOnTurn2 bool
	}{
		{name: "compatible drift continues without a policy hit"},
		{name: "ambiguous hit stops connection without poisoning old thread", cyberOnTurn2: true},
		{name: "strict mode still rejects drift", strict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var received atomic.Int32
			upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.CloseNow() }()
				for {
					ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
					_, _, err := conn.Read(ctx)
					cancel()
					if err != nil {
						return
					}
					turn := received.Add(1)
					event := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_ok_%d","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`, turn)
					if turn == 2 && tc.cyberOnTurn2 {
						event = `{"type":"response.failed","response":{"id":"resp_cyber","model":"gpt-5.1","error":{"code":"cyber_policy","message":"blocked by upstream policy"},"usage":{"input_tokens":1,"output_tokens":1}}}`
					}
					ctx, cancel = context.WithTimeout(r.Context(), 3*time.Second)
					err = conn.Write(ctx, coderws.MessageText, []byte(event))
					cancel()
					if err != nil {
						return
					}
				}
			}))
			defer upstreamServer.Close()
			strict := "false"
			if tc.strict {
				strict = "true"
			}
			h := newOpenAIWSPassthroughHandlerHarnessWithOptions(t, upstreamServer.URL,
				http.Header{"Thread_id": []string{"thread-a"}}, map[string]string{
					service.SettingKeyCyberSessionIdentityStrictEnabled: strict,
				})
			payloads := []string{
				`{"type":"response.create","model":"gpt-5.1","response":{"client_metadata":{"thread_id":"thread-a"},"input":"first"}}`,
				`{"type":"response.create","model":"gpt-5.1","response":{"client_metadata":{"thread_id":"thread-b"},"input":"second"}}`,
				`{"type":"response.create","model":"gpt-5.1","response":{"input":"third without identity"}}`,
			}
			for i, payload := range payloads {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				err := h.clientConn.Write(ctx, coderws.MessageText, []byte(payload))
				cancel()
				require.NoError(t, err)
				ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
				_, event, err := h.clientConn.Read(ctx)
				cancel()
				if i == 2 && tc.cyberOnTurn2 {
					var closeErr coderws.CloseError
					require.ErrorAs(t, err, &closeErr, "a real policy hit must stop even an identity-less follow-up")
					require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
					require.Contains(t, closeErr.Reason, "cyber-security policy")
					break
				}
				require.NoError(t, err)
				if i == 1 && tc.strict {
					require.Equal(t, "cyber_session_identity_conflict", gjson.GetBytes(event, "error.code").String())
					ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
					_, _, err = h.clientConn.Read(ctx)
					cancel()
					var closeErr coderws.CloseError
					require.ErrorAs(t, err, &closeErr)
					require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
					break
				}
				want := "response.completed"
				if i == 1 && tc.cyberOnTurn2 {
					want = "response.failed"
				}
				require.Equal(t, want, gjson.GetBytes(event, "type").String(), string(event))
			}
			if !tc.strict && !tc.cyberOnTurn2 {
				require.NoError(t, h.clientConn.Close(coderws.StatusNormalClosure, "done"))
			}
			select {
			case <-h.handlerDone:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not finish")
			}
			wantReceived := int32(3)
			if tc.strict {
				wantReceived = 1
			} else if tc.cyberOnTurn2 {
				wantReceived = 2
			}
			require.Equal(t, wantReceived, received.Load(), "rejected follow-up must never reach upstream")
			if tc.cyberOnTurn2 {
				require.Eventually(t, func() bool { return len(h.moderationRepo.logSnapshot()) == 1 }, 3*time.Second, 10*time.Millisecond)
			} else {
				require.Empty(t, h.moderationRepo.logSnapshot())
			}
			keyCtx := newTestGinContext()
			keyCtx.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
			keyCtx.Request.Header.Set("thread_id", "thread-a")
			oldKey := service.CyberSessionExplicitBlockKey(h.apiKey.ID, keyCtx, []byte(payloads[0]))
			keyCtx.Request.Header.Del("thread_id")
			newKey := service.CyberSessionExplicitBlockKey(h.apiKey.ID, keyCtx, []byte(payloads[1]))
			store, ok := h.gatewayCache.(service.CyberSessionBlockStore)
			require.True(t, ok)
			matched, err := store.FindCyberSessionBlocked(context.Background(), []string{oldKey, newKey})
			require.NoError(t, err)
			require.Empty(t, matched, "a cyber hit with ambiguous identity must not poison the old header thread")
		})
	}
}
