//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Registration, referral credit, and IP quota claims share the same signup
// transaction. OAuth callers must preserve the quota rejection from that path.
func TestMergedOAuthSignupPreservesRegistrationIPLimit(t *testing.T) {
	newService := func() *AuthService {
		return newEmailOAuthAutoAuthService(
			&userRepoStub{createErr: ErrRegistrationIPLimit},
			map[string]string{SettingKeyRegistrationEnabled: "true"},
			nil,
		)
	}
	t.Run("OAuth callback", func(t *testing.T) {
		svc := newService()
		tokens, user, err := svc.LoginOrRegisterOAuthWithTokenPair(
			context.Background(), "linuxdo-quota@linuxdo-connect.invalid", "new user", "", "", "linuxdo",
		)
		require.ErrorIs(t, err, ErrRegistrationIPLimit)
		require.Nil(t, tokens)
		require.Nil(t, user)
	})
	t.Run("verified email OAuth", func(t *testing.T) {
		svc := newService()
		user, err := svc.createEmailOAuthUser(
			context.Background(), "quota@example.com", "new user", "github", "", "",
		)
		require.ErrorIs(t, err, ErrRegistrationIPLimit)
		require.Nil(t, user)
	})
}
