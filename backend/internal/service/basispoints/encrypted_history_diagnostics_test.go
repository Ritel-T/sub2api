package basispoints

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryptedHistoryDiagnosticsExcludePayloadAndIdentities(t *testing.T) {
	item := object{"type": "agent_message", "author": "PRIVATE_AGENT", "recipient": "root", "content": []any{
		object{"type": "input_text", "text": "PRIVATE_MESSAGE"},
		object{"type": "encrypted_content", "encrypted_content": "PRIVATE_CIPHERTEXT"},
		object{"type": "PRIVATE_TYPE"},
	}}
	err := historyContentError(errors.New("encrypted history"), item)
	require.Contains(t, err.Error(), "item_type=agent_message")
	require.Contains(t, err.Error(), "author_is_root=false; recipient_is_root=true")
	require.Contains(t, err.Error(), "content_types=input_text,encrypted_content,unknown")
	require.NotContains(t, err.Error(), "PRIVATE")
	for _, tc := range []struct {
		recipient any
		want      string
	}{{"/root", "true"}, {"/root/worker", "false"}, {nil, "false"}} {
		item["recipient"] = tc.recipient
		diagnostic := historyContentError(errors.New("encrypted history"), item)
		require.Contains(t, diagnostic.Error(), "recipient_is_root="+tc.want)
	}
	item["type"] = "PRIVATE_TYPE"
	item["recipient"] = "PRIVATE_RECIPIENT"
	err = historyContentError(errors.New("encrypted history"), item)
	require.Contains(t, err.Error(), "item_type=unknown")
	require.NotContains(t, err.Error(), "PRIVATE")
	require.False(t, strings.Contains(err.Error(), "recipient_is_root=true"))
}
