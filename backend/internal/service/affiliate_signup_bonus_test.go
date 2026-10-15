//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type affiliateSignupRepoStub struct {
	AffiliateRepository
	inviterID  int64
	bound      bool
	bonusCalls int
	bindCalls  int
	amount     float64
	fail       error
	apply      func(float64)
}

func (r *affiliateSignupRepoStub) EnsureUserAffiliate(_ context.Context, userID int64) (*AffiliateSummary, error) {
	summary := &AffiliateSummary{UserID: userID}
	if r.bound {
		summary.InviterID = &r.inviterID
	}
	return summary, nil
}
func (r *affiliateSignupRepoStub) GetAffiliateByCode(_ context.Context, code string) (*AffiliateSummary, error) {
	if code != "VALID123" {
		return nil, ErrAffiliateProfileNotFound
	}
	return &AffiliateSummary{UserID: r.inviterID}, nil
}
func (r *affiliateSignupRepoStub) BindInviter(_ context.Context, _, _ int64) (bool, error) {
	r.bindCalls++
	r.bound = true
	return true, r.fail
}
func (r *affiliateSignupRepoStub) BindInviterWithSignupBonus(_ context.Context, _, _ int64, amount float64) (bool, error) {
	r.bonusCalls++
	if r.fail != nil {
		return false, r.fail
	}
	if r.bound {
		return false, nil
	}
	r.bound = true
	r.amount = amount
	if r.apply != nil {
		r.apply(amount)
	}
	return true, nil
}

func TestAffiliateSignupBonus_OnlyFirstValidBindingReceivesGift(t *testing.T) {
	for _, tc := range []struct {
		name, code, enabled, amount string
		alreadyBound                bool
		wantBonus, wantBind         int
		wantError                   error
	}{
		{"valid", " valid123 ", "true", "5", false, 1, 0, nil},
		{"no_code", "", "true", "5", false, 0, 0, nil},
		{"invalid_code", "MISSING", "true", "5", false, 0, 0, ErrAffiliateCodeInvalid},
		{"disabled", "VALID123", "false", "5", false, 0, 0, nil},
		{"already_bound", "VALID123", "true", "5", true, 0, 0, nil},
		{"zero_bonus", "VALID123", "true", "0", false, 0, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &affiliateSignupRepoStub{inviterID: 1, bound: tc.alreadyBound}
			settings := NewSettingService(&settingRepoStub{values: map[string]string{
				SettingKeyAffiliateEnabled:     tc.enabled,
				SettingKeyAffiliateSignupBonus: tc.amount,
			}}, &config.Config{})
			svc := NewAffiliateService(repo, settings, nil, nil)
			err := svc.BindInviterByCode(context.Background(), 2, tc.code)
			if tc.wantError != nil {
				require.ErrorIs(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantBonus, repo.bonusCalls)
			require.Equal(t, tc.wantBind, repo.bindCalls)
			if tc.wantBonus > 0 {
				require.Equal(t, 5.0, repo.amount)
				require.NoError(t, svc.BindInviterByCode(context.Background(), 2, tc.code))
				require.Equal(t, 1, repo.bonusCalls)
			}
		})
	}
}

func TestAffiliateSignupBonus_DoesNotBindWhenAtomicGrantFails(t *testing.T) {
	failure := errors.New("ledger write failed")
	repo := &affiliateSignupRepoStub{inviterID: 1, fail: failure}
	settings := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled: "true", SettingKeyAffiliateSignupBonus: "5",
	}}, &config.Config{})
	err := NewAffiliateService(repo, settings, nil, nil).BindInviterByCode(context.Background(), 2, "VALID123")
	require.ErrorIs(t, err, failure)
	require.False(t, repo.bound)
	require.Zero(t, repo.amount)
	require.Zero(t, repo.bindCalls)
}

func newSignupBonusAuthTestService() (*AuthService, *userRepoStub, *affiliateSignupRepoStub) {
	users := &userRepoStub{nextID: 2}
	settings := map[string]string{
		SettingKeyRegistrationEnabled:  "true",
		SettingKeyDefaultBalance:       "0",
		SettingKeyAffiliateEnabled:     "true",
		SettingKeyAffiliateSignupBonus: "5",
	}
	svc := newAuthService(users, settings, nil, nil)
	svc.refreshTokenCache = &refreshTokenCacheStub{}
	repo := &affiliateSignupRepoStub{inviterID: 1}
	repo.apply = func(amount float64) {
		stored := *users.user
		stored.Balance += amount
		users.usersByID = map[int64]*User{stored.ID: &stored}
	}
	svc.affiliateService = NewAffiliateService(repo, svc.settingService, nil, nil)
	return svc, users, repo
}

func TestAuthService_SignupBonus_EmailResponseAndNoCode(t *testing.T) {
	for _, tc := range []struct {
		code string
		want float64
	}{
		{"VALID123", 5}, {"", 0}, {"INVALID", 0},
	} {
		t.Run(tc.code, func(t *testing.T) {
			svc, _, repo := newSignupBonusAuthTestService()
			_, user, err := svc.RegisterWithVerification(context.Background(), "new@example.com", "password123", "", "", "", tc.code)
			require.NoError(t, err)
			require.Equal(t, tc.want, user.Balance)
			if tc.want == 0 {
				require.Zero(t, repo.bonusCalls)
			}
		})
	}
}

func TestAuthService_SignupBonus_OAuthNewUsersAndExistingLogin(t *testing.T) {
	for _, provider := range []string{"linuxdo", "oidc", "wechat", "dingtalk"} {
		t.Run(provider, func(t *testing.T) {
			svc, _, repo := newSignupBonusAuthTestService()
			_, user, err := svc.LoginOrRegisterOAuthWithTokenPair(context.Background(), "oauth@example.com", "name", "", "VALID123", provider)
			require.NoError(t, err)
			require.Equal(t, 5.0, user.Balance)
			_, user, err = svc.LoginOrRegisterOAuthWithTokenPair(context.Background(), "oauth@example.com", "name", "", "VALID123", provider)
			require.NoError(t, err)
			require.Equal(t, 5.0, user.Balance)
			require.Equal(t, 1, repo.bonusCalls)
		})
	}
}

func TestAuthService_SignupBonus_VerifiedEmailAndPendingFinalization(t *testing.T) {
	for _, provider := range []string{"github", "google", "oidc"} {
		t.Run(provider, func(t *testing.T) {
			svc, _, repo := newSignupBonusAuthTestService()
			user, err := svc.createEmailOAuthUser(context.Background(), "oauth@example.com", "name", provider, "", "VALID123")
			require.NoError(t, err)
			require.Equal(t, 5.0, user.Balance)
			require.Equal(t, 1, repo.bonusCalls)
		})
	}
	svc, users, repo := newSignupBonusAuthTestService()
	user := &User{ID: 2, Email: "pending@example.com", Balance: 0}
	users.user = user
	require.NoError(t, svc.FinalizeOAuthEmailAccount(context.Background(), user, "", "google", "VALID123"))
	require.Equal(t, 5.0, user.Balance)
	require.NoError(t, svc.FinalizeOAuthEmailAccount(context.Background(), user, "", "google", "VALID123"))
	require.Equal(t, 1, repo.bonusCalls)
}

func TestAuthService_SignupBonus_ExistingOAuthAccountCannotClaim(t *testing.T) {
	svc, users, repo := newSignupBonusAuthTestService()
	users.user = &User{ID: 2, Email: "existing@example.com", Role: RoleUser, Status: StatusActive, Balance: 0}
	_, user, err := svc.LoginOrRegisterOAuthWithTokenPair(context.Background(), users.user.Email, "name", "", "VALID123", "linuxdo")
	require.NoError(t, err)
	require.Zero(t, user.Balance)
	require.Zero(t, repo.bonusCalls)
	require.False(t, repo.bound)
}
