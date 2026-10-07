//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInitialQualityAdminCreationAndReauthorization(t *testing.T) {
	repo := &longContextBillingRepoStub{}
	svc := &adminServiceImpl{accountRepo: repo}
	a, err := svc.CreateAccount(context.Background(), &CreateAccountInput{Name: "pending", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test"}, SkipDefaultGroupBind: true})
	require.NoError(t, err)
	require.Same(t, a, repo.createdAccount)
	require.Equal(t, true, repo.createdAccount.Extra[GatewayBorrowAccountQualityPendingKey])
	a.Extra[GatewayBorrowInitialReadyKey] = true
	a.Extra[GatewayBorrowAccountQualityPendingKey] = false
	a.Extra["quality_candy"] = map[string]any{"state": "healthy"}
	updated, err := svc.UpdateAccount(context.Background(), a.ID, &UpdateAccountInput{Credentials: map[string]any{"access_token": "reauthorized"}, Extra: map[string]any{}})
	require.NoError(t, err)
	require.Equal(t, false, updated.Extra[GatewayBorrowAccountQualityPendingKey])
	require.Equal(t, true, updated.Extra[GatewayBorrowInitialReadyKey])
	require.Equal(t, map[string]any{"state": "healthy"}, updated.Extra["quality_candy"])
}

func TestInitialQualityAccountServiceCreationAndUpdate(t *testing.T) {
	repo := &longContextBillingRepoStub{}
	svc := NewAccountService(repo, nil)
	a, err := svc.Create(context.Background(), CreateAccountRequest{Name: "pending", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test"}})
	require.NoError(t, err)
	require.Equal(t, true, a.Extra[GatewayBorrowAccountQualityPendingKey])
	a.Extra[GatewayBorrowInitialReadyKey] = true
	incoming := map[string]any{GatewayBorrowAccountQualityPendingKey: false, GatewayBorrowInitialReadyKey: false}
	updated, err := svc.Update(context.Background(), a.ID, UpdateAccountRequest{Extra: &incoming})
	require.NoError(t, err)
	require.Equal(t, true, updated.Extra[GatewayBorrowAccountQualityPendingKey])
	require.Equal(t, true, updated.Extra[GatewayBorrowInitialReadyKey])
}

func TestInitialQualityCRSNewCreationAndExistingUpdates(t *testing.T) {
	repo := newCRSLongContextAccountRepo()
	result := runCRSOpenAILongContextSync(t, repo, crsOpenAILongContextSource{collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "crs-token"}, extra: map[string]any{GatewayBorrowAccountQualityPendingKey: false, GatewayBorrowInitialReadyKey: true}})
	require.Equal(t, "created", result.Items[0].Action)
	created := repo.accounts["crs-openai-1"]
	require.Equal(t, true, created.Extra[GatewayBorrowAccountQualityPendingKey])
	require.Equal(t, false, created.Extra[GatewayBorrowInitialReadyKey])
	created.Extra[GatewayBorrowAccountQualityPendingKey] = false
	created.Extra[GatewayBorrowInitialReadyKey] = true
	created.Extra["quality_candy"] = map[string]any{"state": "healthy"}
	result = runCRSOpenAILongContextSync(t, repo, crsOpenAILongContextSource{collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "updated-crs-token"}, extra: map[string]any{GatewayBorrowAccountQualityPendingKey: true, GatewayBorrowInitialReadyKey: false, "quality_candy": map[string]any{"state": "degraded"}}})
	require.Equal(t, "updated", result.Items[0].Action)
	require.Equal(t, false, created.Extra[GatewayBorrowAccountQualityPendingKey])
	require.Equal(t, true, created.Extra[GatewayBorrowInitialReadyKey])
	require.Equal(t, map[string]any{"state": "healthy"}, created.Extra["quality_candy"])
	legacy := &Account{ID: 50, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"crs_account_id": "crs-openai-1"}}
	result = runCRSOpenAILongContextSync(t, newCRSLongContextAccountRepo(legacy), crsOpenAILongContextSource{collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "updated-crs-token"}})
	require.Equal(t, "updated", result.Items[0].Action)
	require.NotContains(t, legacy.Extra, GatewayBorrowAccountQualityPendingKey, "legacy reauth is not new creation")
}

func TestInitialQualityBulkExtraCannotSupplyManagedState(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: map[string]any{
		GatewayBorrowAccountQualityModeKey:    GatewayBorrowAccountQualityMode,
		GatewayBorrowAccountQualityPendingKey: false, GatewayBorrowInitialReadyKey: true,
		GatewayBorrowInitialReadyFingerprintKey: "spoofed", GatewayBorrowModelsKey: []string{},
		"quality_candy": map[string]any{"state": "healthy"}, "custom": "allowed",
	}})
	require.NoError(t, err)
	require.Equal(t, "allowed", repo.lastBulkUpdate.Extra["custom"])
	for _, key := range []string{GatewayBorrowAccountQualityModeKey, GatewayBorrowAccountQualityPendingKey, GatewayBorrowInitialReadyKey, GatewayBorrowInitialReadyFingerprintKey, GatewayBorrowModelsKey, "quality_candy"} {
		require.NotContains(t, repo.lastBulkUpdate.Extra, key)
	}
}
