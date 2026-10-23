package service

import (
	"context"
	"fmt"
	"log/slog"

	"time"
)

const (
	balanceNotifyRetryCooldown = time.Minute
	balanceNotifyMaxActive     = 4
	balanceNotifyMaxUsers      = 4096
)

type balanceNotifyAttempt struct {
	next   time.Time
	active bool
}

// CheckBalanceOnRejection does no settings/SMTP I/O on the request path.
// A certified unsent attempt may retry after cooldown; durable claims keep
// indeterminate SMTP outcomes from being resent by another instance or restart.
func (s *BalanceNotifyService) CheckBalanceOnRejection(user *User, balance float64) {
	if !s.canNotifyBalance(user) {
		return
	}
	s.scheduleBalanceEmail(user, func(ctx context.Context, snapshot *User) {
		threshold, rechargeURL, ok := s.resolveUserEffectiveThreshold(ctx, snapshot)
		if ok && balance < threshold {
			s.sendResolvedBalanceEmail(ctx, snapshot, balance, threshold, rechargeURL)
		}
	})
}

func (s *BalanceNotifyService) sendResolvedBalanceEmail(ctx context.Context, user *User, balance, threshold float64, rechargeURL string) {
	if s.notificationEmailService != nil && s.notificationEmailService.qq != nil {
		email, err := s.notificationEmailService.qq.Route(ctx, NotificationEmailSendInput{Event: NotificationEmailEventBalanceLow, UserID: user.ID, SourceType: "balance_low", SourceID: fmt.Sprint(user.ID), ReminderKey: time.Now().UTC().Format("2006-01-02"), Variables: map[string]string{"current_balance": fmt.Sprintf("%.2f", balance), "threshold": fmt.Sprintf("%.2f", threshold)}})
		if err != nil {
			slog.Warn("QQ balance notification failed", "user_id", user.ID)
		}
		if !email {
			return
		}
	}
	recipients := s.collectBalanceNotifyRecipients(user)
	if len(recipients) == 0 {
		return
	}
	s.sendBalanceLowEmails(recipients, user.ID, user.Username, user.Email, balance, threshold, s.getSiteName(ctx), rechargeURL)
}

func (s *BalanceNotifyService) beginBalanceEmail(userID int64, now time.Time) bool {
	s.balanceMu.Lock()
	defer s.balanceMu.Unlock()
	attempt := s.balanceAttempts[userID]
	if attempt.active || now.Before(attempt.next) || s.balanceActive >= balanceNotifyMaxActive {
		return false
	}
	if len(s.balanceAttempts) >= balanceNotifyMaxUsers {
		for id, entry := range s.balanceAttempts {
			if !entry.active && !now.Before(entry.next) {
				delete(s.balanceAttempts, id)
			}
		}
		if len(s.balanceAttempts) >= balanceNotifyMaxUsers {
			return false
		}
	}
	if s.balanceAttempts == nil {
		s.balanceAttempts = make(map[int64]balanceNotifyAttempt)
	}
	s.balanceAttempts[userID] = balanceNotifyAttempt{active: true}
	s.balanceActive++
	return true
}

func (s *BalanceNotifyService) scheduleBalanceEmail(user *User, send func(context.Context, *User)) {
	if !s.beginBalanceEmail(user.ID, time.Now()) {
		return
	}
	snapshot := *user
	snapshot.BalanceNotifyExtraEmails = append([]NotifyEmailEntry(nil), user.BalanceNotifyExtraEmails...)
	if user.BalanceNotifyThreshold != nil {
		threshold := *user.BalanceNotifyThreshold
		snapshot.BalanceNotifyThreshold = &threshold
	}
	go func() {
		defer func() {
			s.balanceMu.Lock()
			s.balanceAttempts[snapshot.ID] = balanceNotifyAttempt{next: time.Now().Add(balanceNotifyRetryCooldown)}
			s.balanceActive--
			s.balanceMu.Unlock()
			if recover() != nil {
				slog.Error("panic in balance notification", "user_id", snapshot.ID)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), emailSendTimeout)
		defer cancel()
		send(ctx, &snapshot)
	}()
}
