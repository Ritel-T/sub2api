package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type compatibleBorrowRevisionProvider struct {
	*borrowSafetyProvider
	previous   string
	compatible bool
}

func (p *compatibleBorrowRevisionProvider) CodexGatewayPinWSBindingValidForRevision(_ context.Context, id int64, model, cookie, revision string) bool {
	return p.compatible && id == p.snapshot.Targets[0].AccountID && model == "gpt-6-astra" && cookie == p.cookie && revision == p.previous
}

func TestBorrowHTTPAndWSKeepOnlyExactMigratedRevisionProof(t *testing.T) {
	s, account, original := borrowSafetyService()
	settings := s.cfg.AstraRouting(t.Context())
	settings.Revision = "before"
	original.snapshot.Revision = settings.Revision
	s.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	provider := &compatibleBorrowRevisionProvider{borrowSafetyProvider: original, previous: "before", compatible: true}
	s.httpUpstream = provider
	ctx, witness := WithGatewayBorrowRequestWitness(t.Context())
	expires := time.Now().Add(time.Minute)
	witness.Confirm(account.ID, "gpt-6-astra", provider.cookie, "before", expires)
	s.recordGatewayBorrowHTTPResponse(ctx, account, "exact-old-response", 7, 9, witness)
	headers := http.Header{"Cookie": {"__oailb=" + provider.cookie}}
	binding := s.bindOpenAIWSHandshake(account, "gpt-6-astra", headers)
	settings.Revision = "after-unrelated-source-removal"
	provider.snapshot.Revision = settings.Revision
	cookie, err := s.gatewayBorrowHTTPContinuation(ctx, account, "gpt-6-astra", "exact-old-response", 7, 9)
	require.NoError(t, err, "only an exact migrated proof authorizes the previous revision")
	require.Equal(t, provider.cookie, cookie)
	require.NoError(t, s.checkOpenAIWSBinding(account, "gpt-6-astra", binding))
	require.Equal(t, "before", binding.borrowRevision)
	provider.compatible = false
	_, err = s.gatewayBorrowHTTPContinuation(ctx, account, "gpt-6-astra", "exact-old-response", 7, 9)
	require.Error(t, err, "unknown old revisions cannot be generally accepted")
	require.Error(t, s.checkOpenAIWSBinding(account, "gpt-6-astra", binding))
}
