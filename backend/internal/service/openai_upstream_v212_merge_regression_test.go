package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The upstream image guard and the Fork's Alpha borrow guard meet at the same
// capability boundary. Neither may disappear when that boundary changes.
func TestUpstreamV212MergeCapabilityContextPreservesImageAndBorrowGuards(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}},
	}
	imageCtx := WithOpenAIImageGenerationRequest(t.Context(), "gpt-6-astra", []byte(`{"tools":[{"type":"image_generation"}]}`))
	require.False(t, accountSupportsOpenAICapabilitiesForRequestContext(imageCtx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
	require.True(t, accountSupportsOpenAICapabilitiesForRequestContext(t.Context(), account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
	account.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-image-2": "gpt-image-2"}
	require.True(t, accountSupportsOpenAICapabilitiesForRequestContext(imageCtx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))

	_, borrowed, _ := borrowSafetyService()
	borrowed.Credentials["model_mapping"] = map[string]any{"protected-channel": "gpt-6-astra", "native-channel": "gpt-6-luna"}
	protectedCtx := WithOpenAIForwardModel(t.Context(), "protected-channel", false)
	require.False(t, accountSupportsOpenAICapabilitiesForRequestContext(protectedCtx, borrowed, "public-alias", OpenAIEndpointCapabilityAlphaSearch, ""))
	nativeCtx := WithOpenAIForwardModel(t.Context(), "native-channel", false)
	require.True(t, accountSupportsOpenAICapabilitiesForRequestContext(nativeCtx, borrowed, "public-alias", OpenAIEndpointCapabilityAlphaSearch, ""))
}

func TestUpstreamV212MergeCompactPayloadKeepsMandatoryBorrowDeny(t *testing.T) {
	svc, account, _ := borrowSafetyService()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses/compact", nil)
	_, err := svc.admitOpenAITurn(t.Context(), c, account, "gpt-6-astra", []byte(`{"model":"gpt-6-astra","input":[]}`))
	require.ErrorContains(t, err, "gateway_borrow_compact_unavailable")
}

func TestUpstreamV212MergeRetiredCompactOwnerDoesNotMove(t *testing.T) {
	for _, staleSnapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial owner", true: "latest owner"}[staleSnapshot], func(t *testing.T) {
			account := encryptedMessageCapabilityAccounts()[1]
			account.Credentials = map[string]any{"compact_model_mapping": map[string]any{"gpt-6-astra": "gpt-5.5"}}
			svc := &OpenAIGatewayService{
				cfg:         newSchedulerTestOpenAIWSV2Config(),
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
			}
			if staleSnapshot {
				stale := account
				stale.Credentials = map[string]any{"compact_model_mapping": map[string]any{"gpt-6-astra": "gpt-6.1-sol"}}
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
					accountsByID: map[int64]*Account{stale.ID: &stale},
				}}
			}
			ctx := context.Background()
			const responseID = "resp_v212_retired_compact_owner"
			store := svc.getOpenAIWSStateStore()
			require.NoError(t, store.BindResponseAccount(ctx, 0, responseID, account.ID, time.Hour))
			id, selected, _, _, err := svc.resolveAccountByPreviousResponseIDForCapability(ctx, nil, responseID, "gpt-6-astra", nil, OpenAIEndpointCapabilityResponses, true)
			require.ErrorIs(t, err, errOpenAIRequiredResponseOwnerUnavailable)
			require.Zero(t, id)
			require.Nil(t, selected)
			owner, err := store.GetResponseAccount(ctx, 0, responseID)
			require.NoError(t, err)
			require.Equal(t, account.ID, owner, "a required continuation binding survives the retirement veto")
		})
	}
}
