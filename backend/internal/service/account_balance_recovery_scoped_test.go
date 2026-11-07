package service

import (
	"context"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

type balanceRecoveryScopedRepo struct {
	AccountRepository
	account  *Account
	snapshot *AccountBalanceRecoverySnapshot
	calls    int
	inTx     bool
}

func (r *balanceRecoveryScopedRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *balanceRecoveryScopedRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}
func (r *balanceRecoveryScopedRepo) GetAccountBalanceRecoverySnapshot(context.Context, int64) (*AccountBalanceRecoverySnapshot, error) {
	return r.snapshot, nil
}
func (r *balanceRecoveryScopedRepo) RecoverAccountBalanceIfObserved(ctx context.Context, id int64, req AccountBalanceRecoveryRequest) (*AccountBalanceRecoveryResult, error) {
	r.calls++
	r.inTx = dbent.TxFromContext(ctx) != nil
	return &AccountBalanceRecoveryResult{AccountID: id, Status: StatusActive, Schedulable: *req.TargetSchedulable}, nil
}
func TestBalanceRecoveryScopedReadAndRecoveryStayWithinScope(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		group       int64
		target      bool
		code        int
		calls       int
	}{
		{"read cannot recover", []string{"accounts.read"}, 18, false, 404, 0},
		{"recover can retain manual false", []string{"accounts.recover"}, 18, false, 200, 1},
		{"recover alone cannot enable", []string{"accounts.recover"}, 18, true, 403, 0},
		{"recover plus status can enable", []string{"accounts.recover", "accounts.status"}, 18, true, 200, 1},
		{"other group denied", []string{"accounts.recover", "accounts.status"}, 19, true, 404, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			r := &balanceRecoveryScopedRepo{account: &Account{ID: 194, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, GroupIDs: []int64{18}, Schedulable: false}}
			s := &adminServiceImpl{accountRepo: r, entClient: client}
			ctx := adminaccess.WithPrincipal(context.Background(), &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: tc.permissions, GroupIDs: []int64{tc.group}, AccountTypes: []string{AccountTypeAPIKey}}}}})
			m.ExpectBegin()
			m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("SELECT id FROM accounts").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("SELECT account_id FROM account_groups").WillReturnResult(sqlmock.NewResult(0, 1))
			if tc.code == 200 {
				m.ExpectCommit()
			} else {
				m.ExpectRollback()
			}
			req := AccountBalanceRecoveryRequest{ExpectedUpdatedAt: time.Now(), ExpectedFingerprint: strings.Repeat("a", 64), TargetSchedulable: &tc.target, Confirm: true, FundingConfirmed: true}
			_, err = s.RecoverAccountBalance(ctx, 194, req)
			require.Equal(t, tc.code, infraerrors.Code(err))
			require.Equal(t, tc.calls, r.calls)
			if tc.calls > 0 {
				require.True(t, r.inTx)
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
	now := time.Now().UTC()
	r := &balanceRecoveryScopedRepo{account: &Account{ID: 194, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, GroupIDs: []int64{18}}, snapshot: balanceRecoveryTestSnapshot(now)}
	s := &adminServiceImpl{accountRepo: r}
	for _, group := range []int64{18, 19} {
		ctx := adminaccess.WithPrincipal(context.Background(), &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.read"}, GroupIDs: []int64{group}, AccountTypes: []string{AccountTypeAPIKey}}}}})
		_, err := s.PreviewAccountBalanceRecovery(ctx, 194)
		if group == 18 {
			require.NoError(t, err)
		} else {
			require.Equal(t, 404, infraerrors.Code(err))
		}
	}
}
