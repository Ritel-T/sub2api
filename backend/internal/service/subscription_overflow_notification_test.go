package service

import (
	"context"
	"errors"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"

	"github.com/stretchr/testify/require"
)

type overflowNotificationRepoStub struct {
	job         *SubscriptionOverflowNotification
	state, code string
	next        time.Time
	completeErr error
	claimCalls  int
}

func (r *overflowNotificationRepoStub) Claim(context.Context, time.Time) (*SubscriptionOverflowNotification, error) {
	r.claimCalls++
	return r.job, nil
}
func (r *overflowNotificationRepoStub) Complete(_ context.Context, _ int64, _ string, state, code string, next time.Time) error {
	r.state, r.code, r.next = state, code, next
	return r.completeErr
}

type overflowSenderStub struct {
	err   error
	calls int
	input NotificationEmailSendInput
}

func (s *overflowSenderStub) Send(_ context.Context, in NotificationEmailSendInput) error {
	s.calls++
	s.input = in
	return s.err
}

func overflowTestJob() *SubscriptionOverflowNotification {
	return &SubscriptionOverflowNotification{ID: 1, ClaimToken: "claim", Attempts: 1, Payload: SubscriptionOverflowNotificationPayload{
		SubscriptionOverflowBillingEvidence: SubscriptionOverflowBillingEvidence{SubscriptionID: 11, SourceGroupID: 2, TargetGroupID: 3, Dimension: "daily", WindowStart: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), DailyUsageUSD: 10, DailyLimitUSD: 10, WeeklyUsageUSD: 20, WeeklyLimitUSD: 70, MonthlyUsageUSD: 30, MonthlyLimitUSD: 300, RateMultiplier: 0.42},
		UserID:                              4, RecipientEmail: "user@example.invalid", RecipientName: "<img src=x onerror=alert(1)>", SourceGroupName: "<script>source</script>", TargetGroupName: "PAYG & Special", BalanceCost: 0.125, BalanceAfter: 9.875, ChargedAt: time.Now().UTC(),
	}}
}

func TestSubscriptionOverflowNotificationRetryClassification(t *testing.T) {
	for _, tt := range []struct {
		name     string
		err      error
		attempts int
		want     string
	}{
		{"success", nil, 1, "sent"},
		{"dial_known_not_sent", notificationEmailDeliveryErr(emailDefinitelyNotSent(errors.New("dial failed"))), 1, "pending"},
		{"template_not_sent", notificationEmailTemplateErr(errors.New("invalid template")), 1, "pending"},
		{"retry_limit", emailDefinitelyNotSent(errors.New("dial failed")), 3, "failed"},
		{"data_acceptance_unknown", notificationEmailDeliveryErr(errors.New("close writer: timeout")), 1, "unknown"},
		{"unclassified_unknown", errors.New("delivery marker write failed"), 1, "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			job := overflowTestJob()
			job.Attempts = tt.attempts
			repo := &overflowNotificationRepoStub{job: job}
			sender := &overflowSenderStub{err: tt.err}
			svc := &SubscriptionOverflowNotificationService{repo: repo, sender: sender}
			svc.runOnce()
			require.Equal(t, tt.want, repo.state)
			require.Equal(t, 1, sender.calls)
			require.Equal(t, tt.want == "pending", !repo.next.IsZero())
		})
	}
}

func TestSubscriptionOverflowNotificationMissingRecipientSuppressed(t *testing.T) {
	job := overflowTestJob()
	job.Payload.RecipientEmail = ""
	repo := &overflowNotificationRepoStub{job: job}
	sender := &overflowSenderStub{}
	(&SubscriptionOverflowNotificationService{repo: repo, sender: sender}).runOnce()
	require.Equal(t, "suppressed", repo.state)
	require.Zero(t, sender.calls)
}

func TestSubscriptionOverflowNotificationTemplatesEscapeAllUserValues(t *testing.T) {
	job := overflowTestJob()
	input := subscriptionOverflowEmailInput(job)
	require.Empty(t, input.SourceType)
	require.Empty(t, input.SourceID)
	require.Empty(t, input.RawHTMLVariables)
	require.Equal(t, "0.42", input.Variables["rate_multiplier"])
	require.Equal(t, "0.12500000", input.Variables["balance_charge"])
	for _, locale := range []string{notificationEmailDefaultLocale, notificationEmailLocaleChinese} {
		tmpl := notificationEmailOfficialTemplates[NotificationEmailEventSubscriptionOverflow][locale]
		input.Variables["recipient_name"] = input.RecipientName
		input.Variables["site_name"] = "Site"
		rendered, err := renderNotificationEmail(input.Event, tmpl.Subject, tmpl.HTML, input.Variables, nil)
		require.NoError(t, err)
		require.NotContains(t, rendered.HTML, "<script>")
		require.NotContains(t, rendered.HTML, "<img src=x")
		require.Contains(t, rendered.HTML, "&lt;script&gt;source&lt;/script&gt;")
		require.Contains(t, rendered.HTML, "PAYG &amp; Special")
		require.Contains(t, rendered.HTML, "0.12500000")
		require.Contains(t, rendered.HTML, "9.87500000")
	}
}

func TestSubscriptionOverflowBillingEvidenceRequiresRealExhaustion(t *testing.T) {
	e := overflowTestJob().Payload.SubscriptionOverflowBillingEvidence
	require.True(t, e.Valid())
	e.DailyUsageUSD = 9
	require.False(t, e.Valid())
	e.DailyUsageUSD = 10
	e.DailyLimitUSD = 0
	require.False(t, e.Valid())
	e.DailyLimitUSD = 10
	e.Dimension = "weekly"
	require.False(t, e.Valid())
	e.WeeklyUsageUSD = 70
	require.True(t, e.Valid())
	e.WindowStart = time.Time{}
	require.False(t, e.Valid())
}

func TestSubscriptionOverflowMissingSMTPIsKnownNotSentAndPreservesCause(t *testing.T) {
	email := NewEmailService(newNotificationEmailMemorySettingRepo(), nil)
	err := email.SendEmail(context.Background(), "user@example.invalid", "test", "test")
	require.ErrorIs(t, err, ErrEmailNotConfigured)
	require.True(t, subscriptionOverflowDefinitelyNotSent(notificationEmailDeliveryErr(err)))
	job := overflowTestJob()
	repo := &overflowNotificationRepoStub{job: job}
	sender := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), email)
	(&SubscriptionOverflowNotificationService{repo: repo, sender: sender}).runOnce()
	require.Equal(t, "pending", repo.state)
	job.Attempts = 3
	(&SubscriptionOverflowNotificationService{repo: repo, sender: sender}).runOnce()
	require.Equal(t, "failed", repo.state)
}

func TestSubscriptionOverflowEmailWrapperPreservesIsAndAs(t *testing.T) {
	cause := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("test connection refused")}
	err := notificationEmailDeliveryErr(emailDefinitelyNotSent(cause))
	require.ErrorIs(t, err, cause)
	var original *net.OpError
	require.ErrorAs(t, err, &original)
	require.Same(t, cause, original)
	require.Equal(t, cause.Error(), err.Error())
}

func TestSubscriptionOverflowEmailUsesConfiguredDeliveryAlias(t *testing.T) {
	// The SMTP server is an in-process loopback fixture, never an external relay.
	ctx := context.Background()
	server := startNotificationEmailTestSMTPServer(t)
	settings := newNotificationEmailMemorySettingRepo()
	require.NoError(t, settings.SetMultiple(ctx, server.settings()))
	email, err := ProvideEmailService(settings, nil, &config.Config{EmailDelivery: config.EmailDeliveryConfig{RecipientDomainAliases: []string{"legacy.invalid=delivery.invalid"}}})
	require.NoError(t, err)
	job := overflowTestJob()
	job.Payload.RecipientEmail = "user@legacy.invalid"
	repo := &overflowNotificationRepoStub{job: job}
	sender := NewNotificationEmailService(settings, email)
	(&SubscriptionOverflowNotificationService{repo: repo, sender: sender}).runOnce()
	require.Equal(t, "sent", repo.state)
	require.Equal(t, int64(1), server.messageCount())
	message, err := mail.ReadMessage(strings.NewReader(server.lastMessage()))
	require.NoError(t, err)
	recipient, err := mail.ParseAddress(message.Header.Get("To"))
	require.NoError(t, err)
	require.Equal(t, "user@delivery.invalid", recipient.Address)
	require.Equal(t, "user@legacy.invalid", job.Payload.RecipientEmail)
}

func TestSubscriptionOverflowWorkerLifecycleIsSafeWithNilAndIdleSender(t *testing.T) {
	var absent *SubscriptionOverflowNotificationService
	require.NotPanics(t, func() { absent.Start(); absent.Stop() })
	for _, withSender := range []bool{false, true} {
		name := "nil_sender"
		if withSender {
			name = "idle_sender"
		}
		t.Run(name, func(t *testing.T) {
			repo := &overflowNotificationRepoStub{}
			var sender *NotificationEmailService
			if withSender {
				sender = NewNotificationEmailService(nil, nil)
			}
			svc := NewSubscriptionOverflowNotificationService(repo, sender)
			if !withSender {
				require.Nil(t, svc.sender, "typed nil must not become a live sender interface")
			}
			svc.Start()
			svc.Start()
			stopped := make(chan struct{})
			go func() { svc.Stop(); svc.Stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("worker did not stop")
			}
			require.LessOrEqual(t, repo.claimCalls, 1, "Start must not create duplicate workers")
			if !withSender {
				require.Zero(t, repo.claimCalls)
			}
		})
	}
}
