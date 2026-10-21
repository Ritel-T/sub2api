package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestClassifyOpenAIWSFirstMessageError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		kind   string
		normal bool
	}{
		{"timeout", service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "missing first response.create message", context.DeadlineExceeded), "first_message_timeout", false},
		{"canceled", fmt.Errorf("client: %w", context.Canceled), "request_canceled", true},
		{"normal_close", coderws.CloseError{Code: coderws.StatusNormalClosure}, "normal_close", true},
		{"eof", fmt.Errorf("read frame: %w", io.EOF), "transport_closed", false},
		{"unexpected_eof", fmt.Errorf("read frame: %w", io.ErrUnexpectedEOF), "transport_closed", false},
		{"protocol_error", coderws.CloseError{Code: coderws.StatusProtocolError}, "websocket_close", false},
		{"unknown", errors.New("read failed"), "read_error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, normal := classifyOpenAIWSFirstMessageError(tc.err)
			require.Equal(t, tc.kind, kind)
			require.Equal(t, tc.normal, normal)
		})
	}
}
