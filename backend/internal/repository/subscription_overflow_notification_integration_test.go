//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Only isolated testcontainers PostgreSQL is used. This test never constructs
// the email worker or SMTP sender and all recipients use the reserved .invalid.
func TestSubscriptionOverflowOutboxConcurrentDedupClaimAndCrash(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	fixture := newSubscriptionOverflowIntegrationFixture(t)
	user := mustCreateUser(t, client, &service.User{Email: "overflow-" + uuid.NewString() + "@example.invalid", PasswordHash: "hash", Balance: 100})
	fixture.userID = user.ID
	source := mustCreateGroup(t, client, &service.Group{Name: "Overflow subscription " + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription})
	fixture.sourceGroupID = source.ID
	target := mustCreateGroup(t, client, &service.Group{Name: "Overflow PAYG " + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard})
	fixture.targetGroupID = target.ID
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: source.ID})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &source.ID, Key: "sk-overflow-" + uuid.NewString(), Name: "overflow"})
	fixture.keyID, fixture.rawKey = key.ID, key.Key
	window := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	e := service.SubscriptionOverflowBillingEvidence{SubscriptionID: sub.ID, SourceGroupID: source.ID, TargetGroupID: target.ID, Dimension: "daily", WindowStart: window, DailyUsageUSD: 10, DailyLimitUSD: 10, WeeklyUsageUSD: 70, WeeklyLimitUSD: 70, MonthlyUsageUSD: 300, MonthlyLimitUSD: 300, RateMultiplier: 0.42}
	repo := NewUsageBillingRepository(client, integrationDB)
	apply := func(evidence *service.SubscriptionOverflowBillingEvidence) error {
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: user.ID, BalanceCost: 0.5, SubscriptionOverflow: evidence})
		return err
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- apply(&e) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM subscription_overflow_email_outbox WHERE user_id=$1", user.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.InDelta(t, 94, balance, 1e-8)
	// Distinct dimensions and renewed windows have distinct durable identities.
	weekly := e
	weekly.Dimension = "weekly"
	require.NoError(t, apply(&weekly))
	renewed := e
	renewed.WindowStart = window.Add(24 * time.Hour)
	require.NoError(t, apply(&renewed))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM subscription_overflow_email_outbox WHERE user_id=$1", user.ID).Scan(&count))
	require.Equal(t, 3, count)
	queue := NewSubscriptionOverflowNotificationRepository(integrationDB)
	jobs := make(chan *service.SubscriptionOverflowNotification, 12)
	errs = make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); job, err := queue.Claim(ctx, time.Now().UTC()); jobs <- job; errs <- err }()
	}
	wg.Wait()
	close(jobs)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	seen := map[int64]bool{}
	for job := range jobs {
		if job == nil {
			continue
		}
		require.Equal(t, user.ID, job.Payload.UserID)
		require.False(t, seen[job.ID])
		seen[job.ID] = true
		require.Equal(t, 1, job.Attempts)
		require.NotEmpty(t, job.ClaimToken)
		require.InDelta(t, 0.42, job.Payload.RateMultiplier, 1e-10)
		require.Equal(t, 0.5, job.Payload.BalanceCost)
		require.Equal(t, user.Email, job.Payload.RecipientEmail)
		require.Equal(t, source.Name, job.Payload.SourceGroupName)
		require.False(t, job.Payload.ChargedAt.IsZero())
		require.NoError(t, queue.Complete(ctx, job.ID, job.ClaimToken, "sent", "", time.Time{}))
	}
	require.Len(t, seen, 3)
	var completed int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM subscription_overflow_email_outbox WHERE user_id=$1 AND status='sent' AND claimed_at IS NOT NULL AND completed_at IS NOT NULL", user.ID).Scan(&completed))
	require.Equal(t, 3, completed)
	// Simulate a crash after claim (possibly after SMTP accepted DATA). Recovery
	// records unknown delivery and cannot return that job to another sender.
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO subscription_overflow_email_outbox(user_id,subscription_id,dimension,window_start,payload,status,attempts,claim_token,claimed_at) VALUES($1,$2,'monthly',$3,'{}','sending',1,'crashed',$4)`, user.ID, sub.ID, window, time.Now().UTC().Add(-11*time.Minute))
	require.NoError(t, err)
	job, err := queue.Claim(ctx, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, job)
	var state, code string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status,last_error_code FROM subscription_overflow_email_outbox WHERE user_id=$1 AND claim_token='crashed'", user.ID).Scan(&state, &code))
	require.Equal(t, "unknown", state)
	require.Equal(t, "claim_expired", code)
	// A restart sees the same unique key even after the original job is terminal.
	require.NoError(t, apply(&e))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM subscription_overflow_email_outbox WHERE user_id=$1 AND dimension='daily' AND window_start=$2", user.ID, window).Scan(&count))
	require.Equal(t, 1, count, fmt.Sprintf("durable dedup user=%d", user.ID))
}

// These tests deliberately commit to the shared isolated database so xmin can
// prove atomicity. Register cleanup before creating fixtures, and retain only
// their concrete IDs so even an early assertion failure cannot leak today's
// usage or active users into the repository dashboard suites.
type subscriptionOverflowIntegrationFixture struct {
	userID, keyID, accountID     int64
	sourceGroupID, targetGroupID int64
	rawKey                       string
}

func newSubscriptionOverflowIntegrationFixture(t *testing.T) *subscriptionOverflowIntegrationFixture {
	t.Helper()
	fixture := &subscriptionOverflowIntegrationFixture{}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		deletions := []struct {
			query string
			id    int64
		}{
			{"DELETE FROM usage_logs WHERE user_id=$1", fixture.userID},
			{"DELETE FROM usage_billing_dedup WHERE api_key_id=$1", fixture.keyID},
			{"DELETE FROM usage_billing_dedup_archive WHERE api_key_id=$1", fixture.keyID},
			{"DELETE FROM subscription_overflow_email_outbox WHERE user_id=$1", fixture.userID},
			{"DELETE FROM user_group_rate_multipliers WHERE user_id=$1", fixture.userID},
			{"DELETE FROM user_subscriptions WHERE user_id=$1", fixture.userID},
			{"DELETE FROM api_keys WHERE id=$1", fixture.keyID},
			{"DELETE FROM users WHERE id=$1", fixture.userID},
			{"DELETE FROM accounts WHERE id=$1", fixture.accountID},
			{"DELETE FROM groups WHERE id=$1", fixture.sourceGroupID},
			{"DELETE FROM groups WHERE id=$1", fixture.targetGroupID},
		}
		for _, deletion := range deletions {
			if deletion.id == 0 {
				continue
			}
			if _, err := integrationDB.ExecContext(ctx, deletion.query, deletion.id); err != nil {
				t.Errorf("cleanup overflow fixture (%s, id=%d): %v", deletion.query, deletion.id, err)
			}
		}
		// Key/user/group deletion triggers can enqueue invalidations, so remove
		// this synthetic key's invalidations after all fixture deletion finishes.
		if fixture.rawKey != "" {
			_, err := integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key=encode(sha256(convert_to($1,'UTF8')),'hex')", fixture.rawKey)
			if err != nil {
				t.Errorf("cleanup overflow fixture auth invalidations: %v", err)
			}
		}
	})
	return fixture
}
