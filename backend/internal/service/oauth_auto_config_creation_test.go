//go:build unit

package service

import (
	"context"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
	"maps"
	"testing"
)

type nativeCreationRepoStub struct {
	AdminAccountRepository
	t        *testing.T
	a        *Account
	failure  string
	recorded int
}

func (r *nativeCreationRepoStub) Create(ctx context.Context, a *Account) error {
	require.NotNil(r.t, dbent.TxFromContext(ctx))
	n := *a
	n.ID = 31
	n.Extra = maps.Clone(a.Extra)
	r.a = &n
	a.ID = 31
	return nil
}
func (r *nativeCreationRepoStub) UpdateExtra(ctx context.Context, _ int64, extra map[string]any) error {
	require.NotNil(r.t, dbent.TxFromContext(ctx))
	maps.Copy(r.a.Extra, extra)
	return nil
}

func (r *nativeCreationRepoStub) BindGroups(ctx context.Context, _ int64, g []int64) error {
	require.NotNil(r.t, dbent.TxFromContext(ctx))
	if r.failure == "bind" {
		return errors.New("bind failure")
	}
	r.a.GroupIDs = append([]int64(nil), g...)
	return nil
}
func (r *nativeCreationRepoStub) GetByID(ctx context.Context, _ int64) (*Account, error) {
	require.NotNil(r.t, dbent.TxFromContext(ctx))
	n := *r.a
	n.Extra = maps.Clone(r.a.Extra)
	return &n, nil
}
func (r *nativeCreationRepoStub) MarkGatewayBorrowInitialReadyIfObserved(ctx context.Context, id int64, o GatewayBorrowInitialReadyObservation) (GatewayBorrowPolicyResult, error) {
	require.NotNil(r.t, dbent.TxFromContext(ctx))
	require.Equal(r.t, r.a.ID, id)
	require.Equal(r.t, GatewayBorrowInitialConfiguration(r.a), o.ExpectedConfig)
	require.Equal(r.t, GatewayBorrowCredentialSHA256(r.a.Credentials), o.CredentialSHA256)
	if r.failure == "ready" {
		return GatewayBorrowPolicyResult{}, errors.New("ready failure")
	}
	if r.failure == "cas" {
		return GatewayBorrowPolicyResult{Skipped: true, Reason: "configuration_changed"}, nil
	}
	if !r.a.Schedulable {
		require.True(r.t, o.AllowPaused)
	}
	r.a.Extra[GatewayBorrowInitialReadyKey] = true
	r.a.Extra[GatewayBorrowInitialReadyFingerprintKey] = GatewayBorrowInitialReadyFingerprint(r.a)
	return GatewayBorrowPolicyResult{Applied: true}, nil
}
func (r *nativeCreationRepoStub) RecordAutoConfigInitial(ctx context.Context, _ *Account, _ []int64) error {
	require.Nil(r.t, dbent.TxFromContext(ctx))
	r.recorded++
	return nil
}
func nativeCreationFixture(t *testing.T) (*adminServiceImpl, *nativeCreationRepoStub, sqlmock.Sqlmock, *accountOpsSettingsStub) {
	db, m, e := sqlmock.New()
	require.NoError(t, e)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	lf := 20
	c := DefaultOAuthAutoConfig()
	c.Enabled = true
	c.Revision = "native-v1"
	c.GroupIDs = []int64{2}
	c.OpenAIAccountDefaults = &OpenAIAccountDefaultsConfig{Enabled: true, Templates: map[string]OAuthAccountTemplate{"business": {Concurrency: 20, LoadFactor: &lf, Priority: 2, ModelMappings: []OAuthModelMappingRule{{From: "gpt-6-astra", To: "gpt-6-astra"}}}}}
	raw, e := json.Marshal(c)
	require.NoError(t, e)
	settings := &accountOpsSettingsStub{raw: string(raw)}
	r := &nativeCreationRepoStub{t: t}
	return &adminServiceImpl{entClient: client, settingService: NewSettingService(settings, nil), accountRepo: r, groupRepo: autoConfigGroups{group: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive}}}, r, m, settings
}
func nativeCreationInput() *CreateAccountInput {
	return &CreateAccountInput{Name: "native", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture", "chatgpt_account_id": "owner", "plan_type": "self_serve_business_prolite"}, Concurrency: 1, Priority: 50, GroupIDs: []int64{9}, SkipMixedChannelCheck: true}
}
func TestNativeOAuthCreationAtomicLifecycle(t *testing.T) {
	for _, failure := range []string{"", "bind", "ready", "cas"} {
		t.Run(failure, func(t *testing.T) {
			s, r, m, _ := nativeCreationFixture(t)
			r.failure = failure
			m.ExpectBegin()
			if failure == "" {
				m.ExpectCommit()
			} else {
				m.ExpectRollback()
			}
			a, e := s.CreateAccount(context.Background(), nativeCreationInput())
			if failure != "" {
				require.Error(t, e)
				require.Nil(t, a)
				require.Zero(t, r.recorded)
			} else {
				require.NoError(t, e)
				require.Equal(t, 20, a.Concurrency)
				require.Equal(t, 20, *a.LoadFactor)
				require.Equal(t, 2, a.Priority)
				require.Equal(t, []int64{2, 9}, a.GroupIDs)
				require.True(t, GatewayBorrowInitialReadyMatchesCurrent(a))
				require.Equal(t, true, a.Extra[GatewayBorrowAccountQualityPendingKey])
				require.Equal(t, 1, r.recorded)
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
func TestNativeOAuthCreationOuterTransactionDefersEffects(t *testing.T) {
	s, r, m, _ := nativeCreationFixture(t)
	m.ExpectBegin()
	tx, e := s.entClient.Tx(context.Background())
	require.NoError(t, e)
	a, e := s.CreateAccount(dbent.NewTxContext(context.Background(), tx), nativeCreationInput())
	require.NoError(t, e)
	require.True(t, GatewayBorrowInitialReadyMatchesCurrent(a))
	require.Zero(t, r.recorded)
	m.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.Zero(t, r.recorded)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestNativeOAuthCreationSnapshotAndObserverBoundary(t *testing.T) {
	s, _, m, stored := nativeCreationFixture(t)
	m.ExpectBegin()
	m.ExpectCommit()
	_, e := s.withOAuthAccountCreation(context.Background(), nativeCreationInput(), func(ctx context.Context) (*Account, error) {
		stored.raw = "{invalid later setting"
		c, e := getOAuthAutoConfigForCreation(ctx, s.settingService)
		require.NoError(t, e)
		require.Equal(t, "native-v1", c.Revision)
		return &Account{}, nil
	})
	require.NoError(t, e)
	_, e = s.withOAuthAccountCreation(WithObserverScope(context.Background(), []int64{9}), nativeCreationInput(), func(ctx context.Context) (*Account, error) {
		require.False(t, nativeOAuthAccountCreation(ctx))
		return &Account{}, nil
	})
	require.NoError(t, e)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestNativeOAuthCRSPausedBirthFingerprint(t *testing.T) {
	s, r, m, _ := nativeCreationFixture(t)
	a := &Account{Name: "CRS", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: nativeCreationInput().Credentials, Status: StatusError, Schedulable: false, Extra: map[string]any{"crs_account_id": "fixture"}}
	m.ExpectBegin()
	m.ExpectCommit()
	syncer := &CRSSyncService{nativeCreate: s.createNativeSyncedAccount}
	require.NoError(t, syncer.createSyncedAccount(context.Background(), a))
	require.Equal(t, StatusError, a.Status)
	require.False(t, a.Schedulable)
	require.Equal(t, 20, a.Concurrency)
	require.Equal(t, []int64{2}, a.GroupIDs)
	require.True(t, GatewayBorrowInitialReadyMatchesCurrent(a))
	require.Equal(t, 1, r.recorded)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestGatewayBorrowReadyHTTPJSONCannotSetAllowPaused(t *testing.T) {
	var o GatewayBorrowInitialReadyObservation
	require.NoError(t, json.Unmarshal([]byte("{\"AllowPaused\":true,\"allow_paused\":true}"), &o))
	require.False(t, o.AllowPaused)
}

func TestNativeDefaultsManagedMarkersOnOrdinaryEdits(t *testing.T) {
	for _, key := range []string{"openai_account_defaults_revision", nativeOAuthAccountDefaultsIdentityKey} {
		incoming := map[string]any{key: "forged", "custom": true}
		current := map[string]any{key: "owned"}
		require.Equal(t, "owned", MergeOpenAIGatewayAccountQualityExtra(incoming, current)[key])
		require.Equal(t, "owned", MergeOpenAIGatewayAccountQualityExtra(nil, current)[key])
		require.NotContains(t, MergeOpenAIGatewayAccountQualityExtra(incoming, nil), key)
		require.NotContains(t, stripGatewayBorrowAccountQualityManagedExtraUpdates(incoming, false), key)
		require.Equal(t, "forged", incoming[key])
	}
}
func TestGenericAccountCreateCannotImportNativeDefaultsOwnership(t *testing.T) {
	repo := &longContextBillingRepoStub{}
	extra := map[string]any{"openai_account_defaults_revision": "forged", nativeOAuthAccountDefaultsIdentityKey: "forged", "custom": true}
	a, err := NewAccountService(repo, nil).Create(context.Background(), CreateAccountRequest{Name: "generic", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture"}, Extra: extra})
	require.NoError(t, err)
	require.NotContains(t, a.Extra, "openai_account_defaults_revision")
	require.NotContains(t, a.Extra, nativeOAuthAccountDefaultsIdentityKey)
	require.Equal(t, "forged", extra["openai_account_defaults_revision"])
	require.Equal(t, true, a.Extra["custom"])
}

type nativeObserverRepoStub struct {
	AdminAccountRepository
	created *Account
}

func (r *nativeObserverRepoStub) Create(_ context.Context, a *Account) error {
	a.ID = 88
	r.created = a
	return nil
}
func (r *nativeObserverRepoStub) BindGroups(_ context.Context, _ int64, _ []int64) error { return nil }
func TestObserverCreateCannotSpoofNativeDefaultsOwnership(t *testing.T) {
	s, _, m, _ := nativeCreationFixture(t)
	repo := &nativeObserverRepoStub{}
	s.accountRepo = repo
	in := nativeCreationInput()
	in.Extra = map[string]any{"openai_account_defaults_revision": "forged", nativeOAuthAccountDefaultsIdentityKey: "forged"}
	a, err := s.CreateAccount(WithObserverScope(context.Background(), []int64{9}), in)
	require.NoError(t, err)
	require.NotContains(t, a.Extra, "openai_account_defaults_revision")
	require.NotContains(t, a.Extra, nativeOAuthAccountDefaultsIdentityKey)
	require.Equal(t, 1, a.Concurrency)
	require.Equal(t, 50, a.Priority)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestNativeDefaultsBirthProofAllowsOnlySameIdentityTokenRefresh(t *testing.T) {
	s, _, m, _ := nativeCreationFixture(t)
	m.ExpectBegin()
	m.ExpectCommit()
	a, err := s.CreateAccount(context.Background(), nativeCreationInput())
	require.NoError(t, err)
	require.True(t, nativeOAuthAccountDefaultsIdentityMatches(a))
	for _, change := range []string{"token", "owner", "user", "proxy", "groups", "mapping", "capacity"} {
		t.Run(change, func(t *testing.T) {
			n := *a
			n.Credentials = maps.Clone(a.Credentials)
			switch change {
			case "token":
				n.Credentials["access_token"] = "refreshed"
			case "owner":
				n.Credentials["chatgpt_account_id"] = "another-owner"
			case "user":
				n.Credentials["chatgpt_user_id"] = "another-user"
			case "proxy":
				id := int64(99)
				n.ProxyID = &id
			case "groups":
				n.GroupIDs = []int64{99}
			case "mapping":
				n.Credentials["model_mapping"] = map[string]any{"gpt-6-luna": "gpt-6-luna"}
			case "capacity":
				n.Concurrency++
			}
			require.Equal(t, change == "token", nativeOAuthAccountDefaultsIdentityMatches(&n))
		})
	}
	require.NoError(t, m.ExpectationsWereMet())
}
