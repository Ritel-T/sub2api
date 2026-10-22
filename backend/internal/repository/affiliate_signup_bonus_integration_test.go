//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"sync"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAffiliateRepository_SignupBonus_ConcurrentGrantOnceAndSeparateFromRecharge(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewAffiliateRepository(client, integrationDB)
	binder := repo.(service.AffiliateSignupBonusBinder)
	inviter := mustCreateCommittedAffiliateWithdrawUser(t, "signup-inviter", 0, 0, 0)
	invitee := mustCreateCommittedAffiliateWithdrawUser(t, "signup-invitee", 0, 0, 0)
	const workers = 8
	start := make(chan struct{})
	bound := make([]bool, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			bound[i], errs[i] = binder.BindInviterWithSignupBonus(ctx, invitee.ID, inviter.ID, 5)
		}(i)
	}
	close(start)
	wg.Wait()
	count := 0
	for i := range workers {
		require.NoError(t, errs[i])
		if bound[i] {
			count++
		}
	}
	require.Equal(t, 1, count)
	require.InDelta(t, 10.5, querySingleFloat(t, ctx, client,
		"SELECT balance::double precision FROM users WHERE id=$1", invitee.ID), 1e-9)
	require.Zero(t, querySingleFloat(t, ctx, client,
		"SELECT total_recharged::double precision FROM users WHERE id=$1", invitee.ID))
	require.Equal(t, 1, querySingleInt(t, ctx, client,
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
	require.Zero(t, querySingleFloat(t, ctx, client,
		"SELECT aff_quota::double precision FROM user_affiliates WHERE user_id=$1", inviter.ID))
	require.Equal(t, 1, querySingleInt(t, ctx, client,
		"SELECT count(*) FROM user_affiliate_ledger WHERE user_id=$1 AND action='signup_bonus' AND source_user_id=$2 AND amount=5 AND balance_after=10.5",
		invitee.ID, inviter.ID))
	filter := service.AffiliateRecordFilter{Search: invitee.Email, Page: 1, PageSize: 100}
	_, rebateCount, err := repo.ListAffiliateRebateRecords(ctx, filter)
	require.NoError(t, err)
	require.Zero(t, rebateCount, "signup credit must not appear as a recharge rebate")
	_, transferCount, err := repo.ListAffiliateTransferRecords(ctx, filter)
	require.NoError(t, err)
	require.Zero(t, transferCount, "signup credit must not appear as a quota transfer")
}

func TestAffiliateRepository_SignupBonus_OuterTransactionRollback(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewAffiliateRepository(client, integrationDB)
	binder := repo.(service.AffiliateSignupBonusBinder)
	inviter := mustCreateCommittedAffiliateWithdrawUser(t, "rollback-inviter", 0, 0, 0)
	invitee := mustCreateCommittedAffiliateWithdrawUser(t, "rollback-invitee", 0, 0, 0)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(ctx, tx)
	bound, err := binder.BindInviterWithSignupBonus(txCtx, invitee.ID, inviter.ID, 5)
	require.NoError(t, err)
	require.True(t, bound)
	require.InDelta(t, 10.5, querySingleFloat(t, txCtx, tx.Client(),
		"SELECT balance::double precision FROM users WHERE id=$1", invitee.ID), 1e-9)
	require.NoError(t, tx.Rollback())
	require.InDelta(t, 5.5, querySingleFloat(t, ctx, client,
		"SELECT balance::double precision FROM users WHERE id=$1", invitee.ID), 1e-9)
	require.Equal(t, 1, querySingleInt(t, ctx, client,
		"SELECT count(*) FROM user_affiliates WHERE user_id=$1 AND inviter_id IS NULL", invitee.ID))
	require.Zero(t, querySingleInt(t, ctx, client,
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
	require.Zero(t, querySingleInt(t, ctx, client,
		"SELECT count(*) FROM user_affiliate_ledger WHERE user_id=$1", invitee.ID))
}

func TestAffiliateRepository_SignupBonus_LedgerFailureRollsBackCreditAndBinding(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewAffiliateRepository(client, integrationDB)
	binder := repo.(service.AffiliateSignupBonusBinder)
	inviter := mustCreateCommittedAffiliateWithdrawUser(t, "ledger-inviter", 0, 0, 0)
	invitee := mustCreateCommittedAffiliateWithdrawUser(t, "ledger-invitee", 0, 0, 0)
	_, err := client.ExecContext(ctx,
		"INSERT INTO user_affiliate_ledger(user_id,action,amount) VALUES($1,'signup_bonus',5)", invitee.ID)
	require.NoError(t, err)
	bound, err := binder.BindInviterWithSignupBonus(ctx, invitee.ID, inviter.ID, 5)
	require.Error(t, err, "unique bonus audit entry must prevent repeat funding even after a manual binding reset")
	require.False(t, bound)
	require.InDelta(t, 5.5, querySingleFloat(t, ctx, client,
		"SELECT balance::double precision FROM users WHERE id=$1", invitee.ID), 1e-9)
	require.Equal(t, 1, querySingleInt(t, ctx, client,
		"SELECT count(*) FROM user_affiliates WHERE user_id=$1 AND inviter_id IS NULL", invitee.ID))
	require.Zero(t, querySingleInt(t, ctx, client,
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
}

type signupIntegrationSettings struct {
	service.SettingRepository
	failBonusRead bool
}

func (r *signupIntegrationSettings) GetValue(_ context.Context, key string) (string, error) {
	values := map[string]string{
		service.SettingKeyRegistrationEnabled:  "true",
		service.SettingKeyDefaultBalance:       "0",
		service.SettingKeyAffiliateEnabled:     "true",
		service.SettingKeyAffiliateSignupBonus: "5",
	}
	value, ok := values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}
func (r *signupIntegrationSettings) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		if r.failBonusRead && key == service.SettingKeyAffiliateSignupBonus {
			return nil, errors.New("settings read failure")
		}
		value, err := r.GetValue(ctx, key)
		if errors.Is(err, service.ErrSettingNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

type signupFailureAffiliateRepo struct {
	service.AffiliateRepository
	client *dbent.Client
}

func (r *signupFailureAffiliateRepo) BindInviterWithSignupBonus(ctx context.Context, userID, inviterID int64, amount float64) (bool, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return false, errors.New("signup transaction missing")
	}
	// Force the actual ledger insert to fail after the balance update by
	// reserving its unique user key inside the same registration transaction.
	_, err := tx.Client().ExecContext(ctx, "INSERT INTO user_affiliate_ledger(user_id,action,amount) VALUES($1,'signup_bonus',5)", userID)
	if err != nil {
		return false, err
	}
	return r.AffiliateRepository.(service.AffiliateSignupBonusBinder).BindInviterWithSignupBonus(ctx, userID, inviterID, amount)
}

type signupFailureUserRepo struct{ service.UserRepository }

func (r *signupFailureUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return nil, errors.New("balance readback failed")
}

func TestAffiliateSignupRegistrationPostgres_GrantFailuresLeaveNoRegisteredAccount(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	realUsers := NewUserRepository(client, integrationDB)
	realAffiliate := NewAffiliateRepository(client, integrationDB)
	inviter := mustCreateCommittedAffiliateWithdrawUser(t, "registration-inviter", 0, 0, 0)
	profile, err := realAffiliate.EnsureUserAffiliate(ctx, inviter.ID)
	require.NoError(t, err)
	for _, failure := range []string{"ledger_insert", "balance_readback", "settings_read", "none", "invalid_code"} {
		t.Run(failure, func(t *testing.T) {
			var users service.UserRepository = realUsers
			var affiliates service.AffiliateRepository = realAffiliate
			settingsRepo := &signupIntegrationSettings{failBonusRead: failure == "settings_read"}
			if failure == "ledger_insert" {
				affiliates = &signupFailureAffiliateRepo{AffiliateRepository: realAffiliate, client: client}
			}
			if failure == "balance_readback" {
				users = &signupFailureUserRepo{realUsers}
			}
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "integration-only-signup", ExpireHour: 1}, Default: config.DefaultConfig{UserConcurrency: 1}}
			settings := service.NewSettingService(settingsRepo, cfg)
			affiliate := service.NewAffiliateService(affiliates, settings, nil, nil)
			auth := service.NewAuthService(client, users, nil, nil, cfg, settings, nil, nil, nil, nil, nil, affiliate, nil)
			email := fmt.Sprintf("signup-%s-%d@example.com", failure, time.Now().UnixNano())
			code := profile.AffCode
			if failure == "invalid_code" {
				code = "MISSINGCODE"
			}
			_, registered, err := auth.RegisterWithVerification(ctx, email, "integration-password", "", "", "", code)
			if failure != "none" && failure != "invalid_code" {
				require.Error(t, err)
				require.Nil(t, registered)
				require.Zero(t, querySingleInt(t, ctx, client, "SELECT count(*) FROM users WHERE email=$1", email), "failed gift cannot leave an account that is unable to retry registration")
			} else {
				require.NoError(t, err)
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id=$1", registered.ID)
				})
				expected := 5.0
				if failure == "invalid_code" {
					expected = 0
				}
				require.Equal(t, expected, registered.Balance)
				require.InDelta(t, expected, querySingleFloat(t, ctx, client, "SELECT balance::double precision FROM users WHERE id=$1", registered.ID), 1e-9)
				if failure == "none" {
					admin := service.NewAdminService(cfg, realUsers, nil, nil, nil, nil, NewRedeemCodeRepository(client),
						nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, affiliate, nil, nil, nil)
					history, total, recharged, historyErr := admin.GetUserBalanceHistory(ctx, registered.ID, 1, 20, "")
					require.NoError(t, historyErr)
					require.EqualValues(t, 1, total)
					require.Zero(t, recharged)
					require.Equal(t, service.RedeemTypeAffiliateSignupBonus, history[0].Type)
					require.Equal(t, 5.0, history[0].Value)
					history, total, _, historyErr = admin.GetUserBalanceHistory(ctx, registered.ID, 1, 20, service.RedeemTypeAffiliateSignupBonus)
					require.NoError(t, historyErr)
					require.EqualValues(t, 1, total)
					require.Len(t, history, 1)
					_, total, _, historyErr = admin.GetUserBalanceHistory(ctx, registered.ID, 1, 20, service.RedeemTypeAffiliateBalance)
					require.NoError(t, historyErr)
					require.Zero(t, total)
				}
			}
		})
	}
}
