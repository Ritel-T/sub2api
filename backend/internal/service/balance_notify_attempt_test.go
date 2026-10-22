//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type balanceClaimMail struct {
	calls atomic.Int32
	err   error
}

func (m *balanceClaimMail) SendEmail(context.Context, string, string, string) error {
	m.calls.Add(1)
	return m.err
}

type balanceMarkerFailRepo struct {
	*notificationEmailMemorySettingRepo
}

func (r *balanceMarkerFailRepo) Set(ctx context.Context, key, value string) error {
	if strings.HasPrefix(key, notificationEmailDeliveryKeyPrefix) {
		return errors.New("delivery marker unavailable")
	}
	return r.notificationEmailMemorySettingRepo.Set(ctx, key, value)
}

type balanceNoClaimRepo struct{ SettingRepository }

type balanceClaimErrorRepo struct {
	*notificationEmailMemorySettingRepo
}

func (r *balanceClaimErrorRepo) ClaimBalanceEmailAttempt(context.Context, string, string) (bool, error) {
	return false, errors.New("claim write unavailable")
}

func balanceClaimKeys(to string) (string, string) {
	key := notificationEmailDeliveryKey(NotificationEmailEventBalanceLow, "balance_low", "71", to, time.Now().UTC().Format("2006-01-02"))
	return key, balanceEmailAttemptKey(key)
}

func TestBalanceNotifyAttempt_CrossInstanceSingleWinner(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	mail := &balanceClaimMail{}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 32 {
		svc := &BalanceNotifyService{settingRepo: repo, emailService: mail}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			svc.sendBalanceLowEmails([]string{"user@example.invalid"}, 71, "User", "", 0, 10, "site", "")
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, int32(1), mail.calls.Load())
	key, attempt := balanceClaimKeys("user@example.invalid")
	_, err := repo.GetValue(context.Background(), key)
	require.NoError(t, err)
	_, err = repo.GetValue(context.Background(), attempt)
	require.NoError(t, err)
}

func TestBalanceNotifyAttempt_UnknownOutcomeOrMarkerFailureNeverResends(t *testing.T) {
	for _, scenario := range []string{"unknown SMTP result", "success but marker write failed"} {
		t.Run(scenario, func(t *testing.T) {
			base := newNotificationEmailMemorySettingRepo()
			var repo SettingRepository = base
			mail := &balanceClaimMail{}
			if scenario == "unknown SMTP result" {
				mail.err = errors.New("timeout after DATA")
			} else {
				repo = &balanceMarkerFailRepo{base}
			}
			for range 3 {
				// A new service has no in-memory cooldown/dedup state: only the durable
				// claim can keep a restart or another process from resending.
				svc := &BalanceNotifyService{settingRepo: repo, emailService: mail}
				svc.sendBalanceLowEmails([]string{"user@example.invalid"}, 71, "User", "", 0, 10, "site", "")
			}
			require.Equal(t, int32(1), mail.calls.Load())
			key, attempt := balanceClaimKeys("user@example.invalid")
			_, err := base.GetValue(context.Background(), key)
			require.ErrorIs(t, err, ErrSettingNotFound, "do not pretend an unknown delivery succeeded")
			owner, err := base.GetValue(context.Background(), attempt)
			require.NoError(t, err)
			require.NotEmpty(t, owner)
		})
	}
}

func TestBalanceNotifyAttempt_OnlyCertifiedUnsentMayRetry(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	mail := &balanceClaimMail{err: emailDefinitelyNotSent(errors.New("failed before DATA"))}
	svc := &BalanceNotifyService{settingRepo: repo, emailService: mail}
	svc.sendBalanceLowEmails([]string{"user@example.invalid"}, 71, "User", "", 0, 10, "site", "")
	_, attempt := balanceClaimKeys("user@example.invalid")
	_, err := repo.GetValue(context.Background(), attempt)
	require.ErrorIs(t, err, ErrSettingNotFound)
	mail.err = nil
	svc = &BalanceNotifyService{settingRepo: repo, emailService: mail}
	svc.sendBalanceLowEmails([]string{"user@example.invalid"}, 71, "User", "", 0, 10, "site", "")
	require.Equal(t, int32(2), mail.calls.Load())
}

func TestBalanceNotifyAttempt_UnavailableStoreFailsClosed(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "write failure", true: "missing port"}[missing], func(t *testing.T) {
			base := newNotificationEmailMemorySettingRepo()
			var repo SettingRepository = &balanceClaimErrorRepo{base}
			if missing {
				repo = &balanceNoClaimRepo{base}
			}
			mail := &balanceClaimMail{}
			svc := &BalanceNotifyService{settingRepo: repo, emailService: mail}
			svc.sendBalanceLowEmails([]string{"user@example.invalid"}, 71, "User", "", 0, 10, "site", "")
			require.Zero(t, mail.calls.Load())
		})
	}
}

func TestBalanceNotifyAttempt_ClaimReleaseMatchesOwner(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	ctx := context.Background()
	ok, err := repo.ClaimBalanceEmailAttempt(ctx, "attempt", "old")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.ClaimBalanceEmailAttempt(ctx, "attempt", "peer")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.ReleaseBalanceEmailAttempt(ctx, "attempt", "peer")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.ReleaseBalanceEmailAttempt(ctx, "attempt", "old")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.ClaimBalanceEmailAttempt(ctx, "attempt", "new")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.ReleaseBalanceEmailAttempt(ctx, "attempt", "old")
	require.NoError(t, err)
	require.False(t, ok)
	owner, err := repo.GetValue(ctx, "attempt")
	require.NoError(t, err)
	require.Equal(t, "new", owner)
}
