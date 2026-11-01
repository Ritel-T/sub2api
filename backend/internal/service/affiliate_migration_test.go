//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type migrationAffiliateRepo struct {
	AffiliateRepository
	inviterID       *int64
	bindCalls       int
	bonusCalls      int
	reads           []int64
	code            string
	lose            bool
	omitPersistence bool
	readbackErr     error
}

func (r *migrationAffiliateRepo) EnsureUserAffiliate(_ context.Context, id int64) (*AffiliateSummary, error) {
	r.reads = append(r.reads, id)
	summary := &AffiliateSummary{UserID: id, AffCode: "CUSTOM-CURRENT"}
	if id == 300 {
		if r.bindCalls > 0 && r.readbackErr != nil {
			return nil, r.readbackErr
		}
		summary.InviterID = r.inviterID
	}
	return summary, nil
}
func (r *migrationAffiliateRepo) GetAffiliateByCode(_ context.Context, code string) (*AffiliateSummary, error) {
	r.code = code
	return &AffiliateSummary{UserID: 52}, nil
}
func (r *migrationAffiliateRepo) BindInviter(_ context.Context, userID, inviterID int64) (bool, error) {
	r.bindCalls++
	if r.lose {
		winner := int64(127)
		r.inviterID = &winner
		return false, nil
	}
	if !r.omitPersistence {
		r.inviterID = &inviterID
	}
	return true, nil
}
func (r *migrationAffiliateRepo) BindInviterWithSignupBonus(context.Context, int64, int64, float64) (bool, error) {
	r.bonusCalls++
	return false, errors.New("migration must not grant signup credit")
}

type migrationUserRepo struct {
	UserRepository
	user  *User
	err   error
	reads []int64
}

func (r *migrationUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	r.reads = append(r.reads, id)
	return r.user, r.err
}
func (r *migrationUserRepo) GetUserAvatar(context.Context, int64) (*UserAvatar, error) {
	return nil, nil
}

func TestAffiliateMigrationCatalogAndExistingBinding(t *testing.T) {
	ctx := context.Background()
	repo := &migrationAffiliateRepo{}
	svc := NewAffiliateService(repo, nil, nil, nil)
	result, err := svc.GetAffiliateMigration(ctx, 300)
	require.NoError(t, err)
	require.False(t, result.Bound)
	require.Equal(t, []AffiliateMigrationOption{
		{"ritel", "**廷 / Ritel （站长）"},
		{"perry", "潘* / Perry / pR （图片站站长）"},
		{"ryverse", "杨* / 汽修二班cyh / 吳菸麵"},
		{"oilnight", "金** / OilNight33 / Rupture"},
		{"hythyr", "侯** / Hythyr"},
		{"joyce", "彭** / 蓬蒿人 / Joyce Peng"},
		{"alaric", "刘* / A2 / Shinoaki"},
		{"xenoge", "高** / 搞打折"},
	}, result.Options)
	self, err := svc.GetAffiliateMigration(ctx, 52)
	require.NoError(t, err)
	require.Len(t, self.Options, 7)
	for _, option := range self.Options {
		require.NotEqual(t, "hythyr", option.Key)
	}
	original := int64(24)
	repo.inviterID = &original
	result, err = svc.GetAffiliateMigration(ctx, 300)
	require.NoError(t, err)
	require.True(t, result.Bound)
	require.NotNil(t, result.Options)
	require.Empty(t, result.Options)
	for _, key := range []string{"hythyr", "unapproved"} {
		_, err = svc.BindMigrationInviter(ctx, 300, key, nil)
		require.ErrorIs(t, err, ErrAffiliateAlreadyBound)
	}
	require.Equal(t, original, *repo.inviterID)
	require.Zero(t, repo.bindCalls)
	require.Zero(t, repo.bonusCalls)
}

func TestAffiliateMigrationRejectsInvalidAndUnavailableInviters(t *testing.T) {
	deletedAt := time.Now()
	for _, tc := range []struct {
		name       string
		key        string
		caller     int64
		target     *User
		lookupErr  error
		want       error
		wantLookup bool
	}{
		{name: "unknown", key: "unknown", caller: 300, want: ErrAffiliateInviterInvalid},
		{name: "raw_id", key: "210", caller: 300, want: ErrAffiliateInviterInvalid},
		{name: "self", key: "hythyr", caller: 52, want: ErrAffiliateInviterInvalid},
		{name: "inactive", key: "hythyr", caller: 300, target: &User{ID: 52, Status: "disabled"}, want: ErrAffiliateInviterUnavailable, wantLookup: true},
		{name: "deleted", key: "hythyr", caller: 300, target: &User{ID: 52, Status: StatusActive, DeletedAt: &deletedAt}, want: ErrAffiliateInviterUnavailable, wantLookup: true},
		{name: "missing", key: "hythyr", caller: 300, lookupErr: ErrUserNotFound, want: ErrAffiliateInviterUnavailable, wantLookup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &migrationAffiliateRepo{}
			users := &migrationUserRepo{user: tc.target, err: tc.lookupErr}
			svc := NewAffiliateService(repo, nil, nil, nil)
			_, err := svc.BindMigrationInviter(context.Background(), tc.caller, tc.key, NewUserService(users, nil, nil, nil))
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.wantLookup, len(users.reads) != 0)
			if tc.wantLookup {
				require.Equal(t, []int64{52}, users.reads)
			}
			require.Zero(t, repo.bindCalls)
			require.Zero(t, repo.bonusCalls)
			require.Equal(t, []int64{tc.caller}, repo.reads)
		})
	}
}

func TestAffiliateMigrationBindsWithoutRewardsAndConfirmsPersistence(t *testing.T) {
	readFailure := errors.New("readback failed")
	for _, tc := range []struct {
		name    string
		lose    bool
		omit    bool
		readErr error
		want    error
	}{
		{name: "current_code_no_signup_reward"},
		{name: "concurrent_winner_preserved", lose: true, want: ErrAffiliateAlreadyBound},
		{name: "unpersisted_bind_cannot_succeed", omit: true, want: ErrAffiliateBindingUnconfirmed},
		{name: "readback_failure_cannot_succeed", readErr: readFailure, want: readFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &migrationAffiliateRepo{lose: tc.lose, omitPersistence: tc.omit, readbackErr: tc.readErr}
			settings := NewSettingService(&settingRepoStub{values: map[string]string{
				SettingKeyAffiliateEnabled: "true", SettingKeyAffiliateSignupBonus: "5",
				SettingKeyAffiliateFirstRechargeBonus: "5", SettingKeyAffiliateFirstRechargeMinAmount: "10",
			}}, &config.Config{})
			users := &migrationUserRepo{user: &User{ID: 52, Status: StatusActive}}
			svc := NewAffiliateService(repo, settings, nil, nil)
			result, err := svc.BindMigrationInviter(context.Background(), 300, "hythyr", NewUserService(users, nil, nil, nil))
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, &AffiliateMigration{Bound: true, Options: []AffiliateMigrationOption{}}, result)
				require.Equal(t, int64(52), *repo.inviterID)
			}
			require.Equal(t, []int64{52}, users.reads)
			require.Equal(t, "CUSTOM-CURRENT", repo.code)
			require.Equal(t, 1, repo.bindCalls)
			require.Zero(t, repo.bonusCalls)
			if tc.lose {
				require.Equal(t, int64(127), *repo.inviterID)
				require.Equal(t, []int64{300, 52}, repo.reads)
			} else {
				require.Equal(t, []int64{300, 52, 300}, repo.reads)
			}
		})
	}
}
