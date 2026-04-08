package service

import "time"

func testRateLimitEntry(resetAt time.Time) map[string]any {
	return map[string]any{
		"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
}

func testRateLimitEntryAt(rateLimitedAt, resetAt time.Time) map[string]any {
	return map[string]any{
		"rate_limited_at":     rateLimitedAt.UTC().Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
}

func testModelRateLimits(entries map[string]map[string]any) map[string]any {
	limits := make(map[string]any, len(entries))
	for key, value := range entries {
		limits[key] = value
	}
	return map[string]any{
		modelRateLimitsKey: limits,
	}
}

func testAccountWithModelRateLimits(entries map[string]map[string]any) *Account {
	return &Account{Extra: testModelRateLimits(entries)}
}

func testAccountWithCreditsPolicy(resetAt time.Time) *Account {
	return &Account{
		Platform: PlatformAntigravity,
		Extra: map[string]any{
			"allow_overages": true,
			modelRateLimitsKey: map[string]any{
				creditsExhaustedKey: testRateLimitEntry(resetAt),
			},
		},
	}
}
