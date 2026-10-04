package service

import "time"

// Automatic warming is not recovery authorization. Respect known native
// quota/auth/overload windows without consulting BPS's independent cooldown.
func gatewayBorrowAccountBlock(account *Account, model string, now time.Time) (string, *time.Time) {
	if account == nil || !account.IsOpenAIOAuthLike() || account.Status != StatusActive ||
		!account.Schedulable || (account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt)) {
		return "target_account_unavailable", nil
	}
	var retry *time.Time
	for _, until := range []*time.Time{account.RateLimitResetAt, account.TempUnschedulableUntil, account.OverloadUntil, account.modelRateLimitResetAt(model)} {
		if until != nil && now.Before(*until) && (retry == nil || until.After(*retry)) {
			copyUntil := *until
			retry = &copyUntil
		}
	}
	if retry != nil {
		return "target_account_rate_limited", retry
	}
	return "", nil
}
