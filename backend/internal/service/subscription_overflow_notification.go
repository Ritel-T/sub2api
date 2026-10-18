package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SubscriptionOverflowBillingEvidence is the immutable routing snapshot plus
// the multiplier used by actual billing, rather than a price from preflight.
type SubscriptionOverflowBillingEvidence struct {
	SubscriptionID  int64     `json:"subscription_id"`
	SourceGroupID   int64     `json:"source_group_id"`
	TargetGroupID   int64     `json:"target_group_id"`
	Dimension       string    `json:"dimension"`
	WindowStart     time.Time `json:"window_start"`
	DailyUsageUSD   float64   `json:"daily_usage_usd"`
	DailyLimitUSD   float64   `json:"daily_limit_usd"`
	WeeklyUsageUSD  float64   `json:"weekly_usage_usd"`
	WeeklyLimitUSD  float64   `json:"weekly_limit_usd"`
	MonthlyUsageUSD float64   `json:"monthly_usage_usd"`
	MonthlyLimitUSD float64   `json:"monthly_limit_usd"`
	RateMultiplier  float64   `json:"rate_multiplier"`
}

func (e *SubscriptionOverflowBillingEvidence) Valid() bool {
	if e == nil || e.SubscriptionID <= 0 || e.SourceGroupID <= 0 || e.TargetGroupID <= 0 || e.SourceGroupID == e.TargetGroupID || e.WindowStart.IsZero() {
		return false
	}
	if e.Dimension != "daily" && e.Dimension != "weekly" && e.Dimension != "monthly" {
		return false
	}
	for _, v := range []float64{e.DailyUsageUSD, e.DailyLimitUSD, e.WeeklyUsageUSD, e.WeeklyLimitUSD, e.MonthlyUsageUSD, e.MonthlyLimitUSD, e.RateMultiplier} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	usage, limit := e.DailyUsageUSD, e.DailyLimitUSD
	switch e.Dimension {
	case "weekly":
		usage, limit = e.WeeklyUsageUSD, e.WeeklyLimitUSD
	case "monthly":
		usage, limit = e.MonthlyUsageUSD, e.MonthlyLimitUSD
	}
	return limit > 0 && usage >= limit
}

type SubscriptionOverflowNotificationPayload struct {
	SubscriptionOverflowBillingEvidence
	UserID          int64     `json:"user_id"`
	RecipientEmail  string    `json:"recipient_email"`
	RecipientName   string    `json:"recipient_name"`
	SourceGroupName string    `json:"source_group_name"`
	TargetGroupName string    `json:"target_group_name"`
	BalanceCost     float64   `json:"balance_cost"`
	BalanceAfter    float64   `json:"balance_after"`
	ChargedAt       time.Time `json:"charged_at"`
}

type SubscriptionOverflowNotification struct {
	ID         int64
	ClaimToken string
	Attempts   int
	Payload    SubscriptionOverflowNotificationPayload
}

type SubscriptionOverflowNotificationRepository interface {
	Claim(context.Context, time.Time) (*SubscriptionOverflowNotification, error)
	Complete(context.Context, int64, string, string, string, time.Time) error
}

type subscriptionOverflowEmailSender interface {
	Send(context.Context, NotificationEmailSendInput) error
}

// SubscriptionOverflowNotificationService sends only committed outbox records.
// A claim is persisted before SMTP; an abandoned or ambiguous claim is never
// automatically replayed. Only known pre-delivery failures get bounded retries.
type SubscriptionOverflowNotificationService struct {
	repo      SubscriptionOverflowNotificationRepository
	sender    subscriptionOverflowEmailSender
	stop      chan struct{}
	startOnce sync.Once
	once      sync.Once
	wg        sync.WaitGroup
}

func NewSubscriptionOverflowNotificationService(repo SubscriptionOverflowNotificationRepository, sender *NotificationEmailService) *SubscriptionOverflowNotificationService {
	svc := &SubscriptionOverflowNotificationService{repo: repo, stop: make(chan struct{})}
	if sender != nil {
		svc.sender = sender
	}
	return svc
}

func (s *SubscriptionOverflowNotificationService) Start() {
	if s == nil || s.repo == nil || s.sender == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-s.stop:
					return
				default:
				}
				s.runOnce()
				select {
				case <-s.stop:
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (s *SubscriptionOverflowNotificationService) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	s.wg.Wait()
}

func (s *SubscriptionOverflowNotificationService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	job, err := s.repo.Claim(ctx, time.Now().UTC())
	if err != nil {
		slog.Warn("subscription overflow outbox claim failed")
		return
	}
	if job == nil {
		return
	}
	state, code, next := "sent", "", time.Time{}
	if strings.TrimSpace(job.Payload.RecipientEmail) == "" {
		state, code = "suppressed", "recipient_missing"
	} else if err = s.sender.Send(ctx, subscriptionOverflowEmailInput(job)); err != nil {
		state, code = "unknown", "delivery_uncertain"
		if subscriptionOverflowDefinitelyNotSent(err) {
			state, code = "failed", "delivery_not_sent"
			if job.Attempts < 3 {
				state = "pending"
				next = time.Now().UTC().Add(time.Duration(job.Attempts*job.Attempts) * time.Minute)
			}
		}
	}
	// Use a fresh bounded context even if SMTP used the original deadline.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err = s.repo.Complete(finishCtx, job.ID, job.ClaimToken, state, code, next); err != nil {
		// Keep the durable claim. Recovery marks it unknown rather than resending.
		slog.Warn("subscription overflow outbox completion failed", "id", job.ID)
		return
	}
	if state != "sent" && state != "suppressed" {
		slog.Warn("subscription overflow email delivery stopped", "id", job.ID, "state", state, "reason", code)
	}
}

func subscriptionOverflowDefinitelyNotSent(err error) bool {
	var smtpErr emailNotSentError
	var templateErr notificationEmailTemplateError
	var configErr notificationEmailConfigError
	return errors.As(err, &smtpErr) || errors.As(err, &templateErr) || errors.As(err, &configErr)
}

func subscriptionOverflowEmailInput(job *SubscriptionOverflowNotification) NotificationEmailSendInput {
	p := job.Payload
	name := func(value string, id int64) string {
		if strings.TrimSpace(value) != "" {
			return value
		}
		return fmt.Sprintf("#%d", id)
	}
	number := func(value float64) string { return strconv.FormatFloat(value, 'f', 8, 64) }
	limit := func(value float64) string {
		if value <= 0 {
			return "—"
		}
		return number(value)
	}
	return NotificationEmailSendInput{
		Event:          NotificationEmailEventSubscriptionOverflow,
		RecipientEmail: p.RecipientEmail, RecipientName: firstNonEmpty(p.RecipientName, p.RecipientEmail), UserID: p.UserID,
		// The outbox owns deduplication. Leaving SourceType/SourceID empty avoids
		// a second non-atomic settings-based delivery marker after SMTP.
		Variables: map[string]string{
			"subscription_group": name(p.SourceGroupName, p.SourceGroupID),
			"subscription_id":    strconv.FormatInt(p.SubscriptionID, 10),
			"target_group":       name(p.TargetGroupName, p.TargetGroupID),
			"quota_dimension":    p.Dimension, "window_start": p.WindowStart.UTC().Format(time.RFC3339),
			"daily_usage": number(p.DailyUsageUSD), "daily_limit": limit(p.DailyLimitUSD),
			"weekly_usage": number(p.WeeklyUsageUSD), "weekly_limit": limit(p.WeeklyLimitUSD),
			"monthly_usage": number(p.MonthlyUsageUSD), "monthly_limit": limit(p.MonthlyLimitUSD),
			"rate_multiplier": strconv.FormatFloat(p.RateMultiplier, 'f', -1, 64),
			"balance_charge":  number(p.BalanceCost), "current_balance": number(p.BalanceAfter),
			"triggered_at": p.ChargedAt.UTC().Format(time.RFC3339),
		},
	}
}
