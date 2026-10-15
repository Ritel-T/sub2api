//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

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
			result: &TurnstileVerifyResponse{Success: true, Hostname: "app.example.com", Action: "auth"}, wantCalls: 1,
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
			svc := NewTurnstileService(settings, verifier)
			err := svc.VerifyTokenWithSecret(context.Background(), "secret", tt.token, "192.0.2.1")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantCalls, verifier.calls)
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
