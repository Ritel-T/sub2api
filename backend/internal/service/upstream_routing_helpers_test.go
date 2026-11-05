//go:build unit

package service

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
)

// Shared requests for protocol-routing behavior tests.
type routingMatrixIngress struct {
	name          string
	path          string
	responsesBody bool
	forward       func(*OpenAIGatewayService, *gin.Context, *Account, []byte) error
}

func routingMatrixIngresses() []routingMatrixIngress {
	return []routingMatrixIngress{
		{
			name: "responses", path: "/v1/responses", responsesBody: true,
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.Forward(context.Background(), c, account, body)
				return err
			},
		},
		{
			name: "chat", path: "/v1/chat/completions",
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				return err
			},
		},
		{
			name: "chat_responses_shape", path: "/v1/chat/completions", responsesBody: true,
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				return err
			},
		},
		{
			name: "messages", path: "/v1/messages",
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				return err
			},
		},
	}
}

type routingMatrixCase struct {
	ingress routingMatrixIngress
	model   string
}

func (tc routingMatrixCase) body() []byte {
	switch {
	case tc.ingress.responsesBody:
		return []byte(fmt.Sprintf(`{"model":%q,"input":"hello","max_output_tokens":32,"stream":false}`, tc.model))
	case tc.ingress.name == "messages":
		return []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`, tc.model))
	default:
		return []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":false}`, tc.model))
	}
}
