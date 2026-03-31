package service

import "testing"

func TestEffectiveConcurrency(t *testing.T) {
	tests := []struct {
		name               string
		account            *Account
		requestedModel     string
		isModelRateLimited bool
		want               int
	}{
		{
			name:               "nil account returns 0",
			account:            nil,
			requestedModel:     "claude-sonnet-4",
			isModelRateLimited: false,
			want:               0,
		},
		{
			name: "unlimited stays unlimited",
			account: &Account{
				Platform:    PlatformAntigravity,
				Concurrency: 0,
			},
			requestedModel:     "claude-sonnet-4",
			isModelRateLimited: false,
			want:               0,
		},
		{
			name: "non-antigravity keeps configured concurrency",
			account: &Account{
				Platform:    PlatformOpenAI,
				Concurrency: 17,
			},
			requestedModel:     "gpt-5",
			isModelRateLimited: true,
			want:               17,
		},
		{
			name: "antigravity quota tier uses quota concurrency",
			account: &Account{
				Platform:    PlatformAntigravity,
				Concurrency: 77,
			},
			requestedModel:     "claude-sonnet-4",
			isModelRateLimited: false,
			want:               opusClawQuotaConcurrency,
		},
		{
			name: "antigravity credits tier uses credits concurrency",
			account: &Account{
				Platform:    PlatformAntigravity,
				Concurrency: 77,
			},
			requestedModel:     "claude-sonnet-4",
			isModelRateLimited: true,
			want:               opusClawCreditsConcurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveConcurrencyForSlot(tt.account, tt.requestedModel, tt.isModelRateLimited)
			if got != tt.want {
				t.Fatalf("effectiveConcurrencyForSlot() = %d, want %d", got, tt.want)
			}
		})
	}
}
