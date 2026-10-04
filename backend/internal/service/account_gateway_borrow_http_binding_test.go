package service

import (
	"github.com/stretchr/testify/require"
	"maps"
	"testing"
	"time"
)

func TestGatewayBorrowHTTPContinuationKeepsVerifiedOwnerAndRoute(t *testing.T) {
	s, a, p := borrowSafetyService()
	ctx, w := WithGatewayBorrowRequestWitness(t.Context())
	w.Confirm(a.ID, "gpt-6-astra", p.cookie, "", time.Now().Add(time.Minute))
	s.recordGatewayBorrowHTTPResponse(ctx, a, "borrow_response", 7, 9, w)
	cookie, err := s.gatewayBorrowHTTPContinuation(ctx, a, "gpt-6-astra", "borrow_response", 7, 9)
	require.NoError(t, err)
	require.Equal(t, p.cookie, cookie)
	for _, tc := range []struct {
		id         string
		model      string
		key, group int64
	}{{"native_response", "gpt-6-astra", 7, 9}, {"borrow_response", "gpt-6.1-sol", 7, 9}, {"borrow_response", "gpt-6-astra", 8, 9}, {"borrow_response", "gpt-6-astra", 7, 10}} {
		_, err := s.gatewayBorrowHTTPContinuation(ctx, a, tc.model, tc.id, tc.key, tc.group)
		require.Error(t, err)
	}
	p.cookie = "rotated-route"
	_, err = s.gatewayBorrowHTTPContinuation(ctx, a, "gpt-6-astra", "borrow_response", 7, 9)
	require.Error(t, err)
}
func TestGatewayBorrowHTTPContinuationExpiredOrUnknownFailsClosed(t *testing.T) {
	s, a, p := borrowSafetyService()
	ctx, w := WithGatewayBorrowRequestWitness(t.Context())
	w.Confirm(a.ID, "gpt-6-astra", p.cookie, "", time.Now().Add(-time.Second))
	s.recordGatewayBorrowHTTPResponse(ctx, a, "expired", 7, 9, w)
	_, err := s.gatewayBorrowHTTPContinuation(ctx, a, "gpt-6-astra", "expired", 7, 9)
	require.Error(t, err)
}

func TestGatewayBorrowHTTPContinuationChangedWorkspaceIdentityRejected(t *testing.T) {
	s, a, p := borrowSafetyService()
	ctx, w := WithGatewayBorrowRequestWitness(t.Context())
	w.Confirm(a.ID, "gpt-6-astra", p.cookie, "", time.Now().Add(time.Minute))
	s.recordGatewayBorrowHTTPResponse(ctx, a, "owner_identity", 7, 9, w)
	a.Credentials = maps.Clone(a.Credentials)
	a.Credentials["chatgpt_account_id"] = "different-workspace"
	_, err := s.gatewayBorrowHTTPContinuation(ctx, a, "gpt-6-astra", "owner_identity", 7, 9)
	require.Error(t, err)
}
