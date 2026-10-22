package service

import "context"

// balanceEmailAttemptStore provides a durable, cross-process single-winner
// claim. Claims have no automatic expiry: an interrupted SMTP exchange can have
// delivered its message even when no success marker was recorded.
type balanceEmailAttemptStore interface {
	ClaimBalanceEmailAttempt(ctx context.Context, key, owner string) (bool, error)
	ReleaseBalanceEmailAttempt(ctx context.Context, key, owner string) (bool, error)
}

func balanceEmailAttemptKey(deliveryKey string) string {
	return "balance_email_attempt:" + notificationEmailHash(deliveryKey)
}
