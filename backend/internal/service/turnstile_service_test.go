//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type turnstileResultVerifier struct {
	result *TurnstileVerifyResponse
	err    error
	calls  int
}

func (v *turnstileResultVerifier) VerifyToken(context.Context, string, string, string) (*TurnstileVerifyResponse, error) {
	v.calls++
	return v.result, v.err
}

type turnstileReplayStoreStub struct {
	claimed bool
	err     error
	hash    string
}

func (s *turnstileReplayStoreStub) ClaimToken(_ context.Context, tokenHash string) (bool, error) {
	s.hash = tokenHash
	return s.claimed, s.err
}

func TestTurnstileService_VerifyTokenWithSecret(t *testing.T) {
	networkErr := errors.New("siteverify unavailable")
	tests := []struct {
		name      string
		hostnames []string
		strict    bool
		token     string
		result    *TurnstileVerifyResponse
		verifyErr error
		wantErr   error
		wantCalls int
	}{
		{
			name: "strict context without configured hosts", strict: true, token: "token",
			result: &TurnstileVerifyResponse{Success: true}, wantErr: ErrTurnstileNotConfigured,
		},
		{
			name: "strict context with empty list", strict: true, hostnames: []string{}, token: "token",
			result: &TurnstileVerifyResponse{Success: true}, wantErr: ErrTurnstileNotConfigured,
		},
		{
			name: "strict context with blank hosts", strict: true, hostnames: []string{"", " \t"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true}, wantErr: ErrTurnstileNotConfigured,
		},
		{
			name: "strict context matching proof", strict: true, hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth", ChallengeTS: time.Now().UTC().Format(time.RFC3339)}, wantCalls: 1,
		},
		{
			name: "strict context still requires auth action", strict: true, hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "signup"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "strict approved hostname and action", hostnames: []string{"app.example.com", "login.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "login.example.com", Action: "auth"}, wantCalls: 1,
		},
		{
			name: "configuration whitespace is trimmed", hostnames: []string{" app.example.com "}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth"}, wantCalls: 1,
		},
		{
			name: "unapproved hostname", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "other.example.com", Action: "auth"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "hostname suffix cannot match", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com.attacker.test", Action: "auth"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "subdomain is not implicitly approved", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "child.app.example.com", Action: "auth"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "development hostname cannot use production allowlist", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "localhost", Action: "auth"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "missing hostname", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Action: "auth"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "wrong action", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "contact"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "missing action", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "blank configured entries do not enable legacy behavior", hostnames: []string{" "}, token: "token",
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth"}, wantErr: ErrTurnstileNotConfigured,
		},
		{
			name: "legacy deployment without hostname configuration", token: "token",
			result: &TurnstileVerifyResponse{Success: true}, wantCalls: 1,
		},
		{
			name: "rejected or replayed token", hostnames: []string{"app.example.com"}, token: "token",
			result: &TurnstileVerifyResponse{Success: false, ErrorCodes: []string{"timeout-or-duplicate"}, Hostname: "app.example.com", Action: "auth"}, wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "nil response", token: "token", wantErr: ErrTurnstileVerificationFailed, wantCalls: 1,
		},
		{
			name: "network error", token: "token", verifyErr: networkErr, wantErr: networkErr, wantCalls: 1,
		},
		{
			name: "empty token", wantErr: ErrTurnstileVerificationFailed,
		},
		{
			name: "blank token", token: " \t", wantErr: ErrTurnstileVerificationFailed,
		},
		{
			name: "leading whitespace cannot change replay digest", token: " token", wantErr: ErrTurnstileVerificationFailed,
		},
		{
			name: "trailing whitespace cannot change replay digest", token: "token\n", wantErr: ErrTurnstileVerificationFailed,
		},
		{
			name: "oversized token", token: strings.Repeat("x", 2049), wantErr: ErrTurnstileVerificationFailed,
		},
		{
			name: "maximum token size", token: strings.Repeat("x", 2048),
			result: &TurnstileVerifyResponse{Success: true}, wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := &turnstileResultVerifier{result: tt.result, err: tt.verifyErr}
			settings := &SettingService{cfg: &config.Config{Turnstile: config.TurnstileConfig{StrictContext: tt.strict, ExpectedHostnames: tt.hostnames}}}
			store := &turnstileReplayStoreStub{claimed: true}
			svc := NewTurnstileServiceWithReplayStore(settings, verifier, store)
			err := svc.VerifyTokenWithSecret(context.Background(), "secret", tt.token, "192.0.2.1")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantCalls, verifier.calls)
			if tt.wantCalls == 0 {
				require.Empty(t, store.hash, "invalid input or configuration must not reach the replay store")
			}
		})
	}
}

func TestTurnstileService_VerifyTokenUsesConfiguredContext(t *testing.T) {
	verifier := &turnstileResultVerifier{result: &TurnstileVerifyResponse{Success: true, Hostname: "localhost", Action: "auth"}}
	settings := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyTurnstileEnabled:   "true",
		SettingKeyTurnstileSecretKey: "secret",
	}}, &config.Config{Turnstile: config.TurnstileConfig{ExpectedHostnames: []string{"app.example.com"}}})
	svc := NewTurnstileService(settings, verifier)
	require.ErrorIs(t, svc.VerifyToken(context.Background(), "token", "192.0.2.1"), ErrTurnstileVerificationFailed)
	require.Equal(t, 1, verifier.calls)
}

func TestTurnstileService_MissingDependenciesFailClosed(t *testing.T) {
	var svc *TurnstileService
	require.ErrorIs(t, svc.VerifyToken(context.Background(), "token", ""), ErrTurnstileNotConfigured)
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""), ErrTurnstileNotConfigured)
	require.ErrorIs(t, svc.ValidateSecretKey(context.Background(), "secret"), ErrTurnstileNotConfigured)

	svc = NewTurnstileService(nil, &turnstileResultVerifier{})
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""), ErrTurnstileNotConfigured)
	svc.settingService = &SettingService{}
	require.ErrorIs(t, svc.VerifyTokenWithSecret(context.Background(), "secret", "token", ""), ErrTurnstileNotConfigured)
	require.ErrorIs(t, svc.ValidateSecretKey(context.Background(), "secret"), ErrTurnstileVerificationFailed)
}

func TestTurnstileService_ValidateSecretKey(t *testing.T) {
	networkErr := errors.New("siteverify unavailable")
	tests := []struct {
		name   string
		result *TurnstileVerifyResponse
		err    error
		want   error
	}{
		{name: "explicit invalid response proves secret accepted", result: &TurnstileVerifyResponse{ErrorCodes: []string{"invalid-input-response"}}},
		{name: "invalid secret", result: &TurnstileVerifyResponse{ErrorCodes: []string{"invalid-input-secret"}}, want: ErrTurnstileInvalidSecretKey},
		{name: "missing secret", result: &TurnstileVerifyResponse{ErrorCodes: []string{"missing-input-secret"}}, want: ErrTurnstileInvalidSecretKey},
		{name: "invalid response does not override invalid secret", result: &TurnstileVerifyResponse{ErrorCodes: []string{"invalid-input-response", "invalid-input-secret"}}, want: ErrTurnstileInvalidSecretKey},
		{name: "invalid response does not override missing secret", result: &TurnstileVerifyResponse{ErrorCodes: []string{"invalid-input-response", "missing-input-secret"}}, want: ErrTurnstileInvalidSecretKey},
		{name: "unexpected success", result: &TurnstileVerifyResponse{Success: true}, want: ErrTurnstileVerificationFailed},
		{name: "success with invalid response error", result: &TurnstileVerifyResponse{Success: true, ErrorCodes: []string{"invalid-input-response"}}, want: ErrTurnstileVerificationFailed},
		{name: "empty errors", result: &TurnstileVerifyResponse{}, want: ErrTurnstileVerificationFailed},
		{name: "internal error", result: &TurnstileVerifyResponse{ErrorCodes: []string{"internal-error"}}, want: ErrTurnstileVerificationFailed},
		{name: "invalid response with internal error", result: &TurnstileVerifyResponse{ErrorCodes: []string{"invalid-input-response", "internal-error"}}, want: ErrTurnstileVerificationFailed},
		{name: "unknown error", result: &TurnstileVerifyResponse{ErrorCodes: []string{"unexpected"}}, want: ErrTurnstileVerificationFailed},
		{name: "nil result", want: ErrTurnstileVerificationFailed},
		{name: "network error", err: networkErr, want: networkErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := &turnstileResultVerifier{result: tt.result, err: tt.err}
			svc := NewTurnstileService(nil, verifier)
			err := svc.ValidateSecretKey(context.Background(), "secret")
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}
			require.Equal(t, 1, verifier.calls)
		})
	}
}

func TestTurnstileService_StrictReplayDependencyAndFailure(t *testing.T) {
	for _, test := range []struct {
		name  string
		store *turnstileReplayStoreStub
		want  error
	}{
		{name: "missing dependency", want: ErrTurnstileNotConfigured},
		{name: "claimed by previous request", store: &turnstileReplayStoreStub{}, want: ErrTurnstileVerificationFailed},
		{name: "store error", store: &turnstileReplayStoreStub{err: errors.New("redis unavailable")}, want: ErrServiceUnavailable},
		{name: "first claim", store: &turnstileReplayStoreStub{claimed: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := &SettingService{cfg: &config.Config{Turnstile: config.TurnstileConfig{StrictContext: true, ExpectedHostnames: []string{"app.example.com"}}}}
			verifier := &turnstileResultVerifier{result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth", ChallengeTS: time.Now().UTC().Format(time.RFC3339)}}
			svc := NewTurnstileService(settings, verifier)
			if test.store != nil {
				svc = NewTurnstileServiceWithReplayStore(settings, verifier, test.store)
			}
			err := svc.VerifyTokenWithSecret(context.Background(), "secret", "private-token", "")
			if test.want == nil {
				require.NoError(t, err)
				require.Equal(t, 1, verifier.calls)
			} else {
				require.ErrorIs(t, err, test.want)
				require.Zero(t, verifier.calls)
			}
			if test.store != nil {
				digest := sha256.Sum256([]byte("private-token"))
				require.Equal(t, hex.EncodeToString(digest[:]), test.store.hash)
			}
		})
	}
}

func TestTurnstileService_StrictContextChallengeTime(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		ts   string
		ok   bool
	}{
		{name: "fresh", ts: now.Add(-time.Minute).Format(time.RFC3339), ok: true},
		{name: "exact maximum age", ts: now.Add(-5 * time.Minute).Format(time.RFC3339), ok: true},
		{name: "expired", ts: now.Add(-5*time.Minute - time.Nanosecond).Format(time.RFC3339Nano)},
		{name: "missing"},
		{name: "malformed", ts: "not-a-time"},
		{name: "within future clock skew", ts: now.Add(time.Minute).Format(time.RFC3339), ok: true},
		{name: "too far in future", ts: now.Add(time.Minute + time.Nanosecond).Format(time.RFC3339Nano)},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := &SettingService{cfg: &config.Config{Turnstile: config.TurnstileConfig{StrictContext: true, ExpectedHostnames: []string{"app.example.com"}}}}
			verifier := &turnstileResultVerifier{result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth", ChallengeTS: test.ts}}
			svc := NewTurnstileServiceWithReplayStore(settings, verifier, &turnstileReplayStoreStub{claimed: true})
			svc.now = func() time.Time { return now }
			err := svc.VerifyTokenWithSecret(context.Background(), "secret", "token", "")
			if test.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrTurnstileVerificationFailed)
			}
			require.Equal(t, 1, verifier.calls)
		})
	}
}
