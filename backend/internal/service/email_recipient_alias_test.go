//go:build unit

package service

import (
	"context"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRewriteSMTPRecipientDomainExactBoundary(t *testing.T) {
	tests := []struct{ input, want string }{
		{"User+tag@ritelt.com", "User+tag@ritel.si"},
		{"User@RiTeLt.CoM", "User@ritel.si"},
		{"Display Name <User@ritelt.com>", "User@ritel.si"},
		{`"local@part"@ritelt.com`, `local@part@ritel.si`},
		{"user@sub.ritelt.com", "user@sub.ritelt.com"},
		{"user@notritelt.com", "user@notritelt.com"},
		{"user@ritelt.com.evil.example", "user@ritelt.com.evil.example"},
		{"user@ritel.si", "user@ritel.si"},
		{"user@example.com", "user@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			rewritten, err := rewriteSMTPRecipientDomain(tt.input, map[string]string{"ritelt.com": "ritel.si"})
			require.NoError(t, err)
			address, err := mail.ParseAddress(rewritten)
			require.NoError(t, err)
			require.Equal(t, tt.want, address.Address)
			original, err := mail.ParseAddress(tt.input)
			require.NoError(t, err)
			require.Equal(t, original.Name, address.Name)
		})
	}
	original := "User <User@ritelt.com>"
	disabled, err := rewriteSMTPRecipientDomain(original, nil)
	require.NoError(t, err)
	require.Equal(t, original, disabled)
	_, err = rewriteSMTPRecipientDomain("user@ritelt.com\r\nBcc: victim@example.com", map[string]string{"ritelt.com": "ritel.si"})
	require.Error(t, err)
}

func TestEmailDeliveryAliasesReachSMTPEnvelopeAndMIME(t *testing.T) {
	srv, port := startFakeSMTPServer(t, true, false)
	cfg := &config.Config{EmailDelivery: config.EmailDeliveryConfig{RecipientDomainAliases: []string{"ritelt.com=ritel.si"}}}
	svc, err := ProvideEmailService(nil, nil, cfg)
	require.NoError(t, err)
	body := `<p>Account User@ritelt.com</p><a href="https://dash.ritel.si/reset-password?email=User%40ritelt.com&amp;token=original">Reset</a>`
	require.NoError(t, svc.SendEmailWithConfig(smtpTestConfig(port, true), "Display <User@ritelt.com>", "Subject", body))
	srv.mu.Lock()
	messages := append([]string(nil), srv.messages...)
	commands := append([]string(nil), srv.commands...)
	srv.mu.Unlock()
	require.Len(t, messages, 1)
	var recipients []string
	for _, command := range commands {
		if strings.HasPrefix(command, "RCPT TO:") {
			recipients = append(recipients, command)
		}
	}
	require.Equal(t, []string{"RCPT TO:<User@ritel.si>"}, recipients)
	parsed, err := mail.ReadMessage(strings.NewReader(messages[0]))
	require.NoError(t, err)
	to, err := mail.ParseAddress(parsed.Header.Get("To"))
	require.NoError(t, err)
	require.Equal(t, "User@ritel.si", to.Address)
	require.Equal(t, "Display", to.Name)
	content, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	require.NoError(t, err)
	require.Equal(t, body+"\r\n", string(content))
	require.NotContains(t, parsed.Header.Get("From"), "ritel.si")
	// The provider owns a copy; later config edits cannot mutate live aliases.
	cfg.EmailDelivery.RecipientDomainAliases[0] = "ritelt.com=other.example"
	require.Equal(t, "ritel.si", svc.recipientDomainAliases["ritelt.com"])
}

type aliasIdentityCache struct {
	EmailCache
	verificationIdentity string
	resetIdentity        string
	code                 *VerificationCodeData
}

func (c *aliasIdentityCache) GetVerificationCode(context.Context, string) (*VerificationCodeData, error) {
	return nil, nil
}
func (c *aliasIdentityCache) SetVerificationCode(_ context.Context, email string, data *VerificationCodeData, _ time.Duration) error {
	c.verificationIdentity = email
	c.code = data
	return nil
}
func (c *aliasIdentityCache) SetPasswordResetToken(_ context.Context, email string, _ *PasswordResetTokenData, _ time.Duration) error {
	c.resetIdentity = email
	return nil
}

func TestEmailDeliveryAliasesPreserveVerificationAndResetIdentity(t *testing.T) {
	srv := startNotificationEmailTestSMTPServer(t)
	repo := &notificationEmailMemorySettingRepo{values: srv.settings()}
	cache := &aliasIdentityCache{}
	svc, err := ProvideEmailService(repo, cache, &config.Config{EmailDelivery: config.EmailDeliveryConfig{RecipientDomainAliases: []string{"ritelt.com=ritel.si"}}})
	require.NoError(t, err)
	identity := "legacy-user@ritelt.com"
	require.NoError(t, svc.SendVerifyCode(context.Background(), identity, "Ritel SI"))
	require.Equal(t, identity, cache.verificationIdentity)
	require.Len(t, cache.code.Code, 6)
	require.Contains(t, srv.lastMessage(), "legacy-user@ritel.si")
	require.NoError(t, svc.SendPasswordResetEmail(context.Background(), identity, "Ritel SI", "https://dash.ritel.si/reset-password"))
	require.Equal(t, identity, cache.resetIdentity)
	require.Contains(t, srv.lastMessageBody(t), "email=legacy-user%40ritelt.com")
	require.NotContains(t, srv.lastMessageBody(t), "email=legacy-user%40ritel.si")
	parsed, err := mail.ReadMessage(strings.NewReader(srv.lastMessage()))
	require.NoError(t, err)
	to, err := mail.ParseAddress(parsed.Header.Get("To"))
	require.NoError(t, err)
	require.Equal(t, "legacy-user@ritel.si", to.Address)
}

func TestProvideEmailServiceRejectsInvalidAliases(t *testing.T) {
	_, err := ProvideEmailService(nil, nil, &config.Config{EmailDelivery: config.EmailDeliveryConfig{RecipientDomainAliases: []string{"*.ritelt.com=ritel.si"}}})
	require.Error(t, err)
}

func TestEmailDeliveryAliasesPreserveNotificationDedupIdentity(t *testing.T) {
	ctx := context.Background()
	server := startNotificationEmailTestSMTPServer(t)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(ctx, server.settings()))
	email, err := ProvideEmailService(repo, nil, &config.Config{EmailDelivery: config.EmailDeliveryConfig{RecipientDomainAliases: []string{"ritelt.com=ritel.si"}}})
	require.NoError(t, err)
	service := NewNotificationEmailService(repo, email)
	input := NotificationEmailSendInput{Event: NotificationEmailEventSubscriptionExpiryReminder, RecipientEmail: "legacy-user@ritelt.com", RecipientName: "User", UserID: 42, SourceType: "user_subscription", SourceID: "1234567890", ReminderKey: "7d", Variables: map[string]string{"subscription_group": "Codex", "expiry_time": "2026-05-27 12:00", "days_remaining": "7"}}
	require.NoError(t, service.Send(ctx, input))
	require.Equal(t, int64(1), server.messageCount())
	key := notificationEmailDeliveryKey(input.Event, input.SourceType, input.SourceID, input.RecipientEmail, input.ReminderKey)
	_, err = repo.GetValue(ctx, key)
	require.NoError(t, err)
	newKey := notificationEmailDeliveryKey(input.Event, input.SourceType, input.SourceID, "legacy-user@ritel.si", input.ReminderKey)
	_, err = repo.GetValue(ctx, newKey)
	require.ErrorIs(t, err, ErrSettingNotFound)
	require.NoError(t, service.Send(ctx, input))
	require.Equal(t, int64(1), server.messageCount())
	require.Equal(t, "legacy-user@ritelt.com", input.RecipientEmail)
	parsed, err := mail.ReadMessage(strings.NewReader(server.lastMessage()))
	require.NoError(t, err)
	recipient, err := mail.ParseAddress(parsed.Header.Get("To"))
	require.NoError(t, err)
	require.Equal(t, "legacy-user@ritel.si", recipient.Address)
}

func TestEmailDeliveryAliasesKeepExternalAndDisabledSMTPRecipients(t *testing.T) {
	for _, tt := range []struct {
		name, to string
		aliases  []string
	}{
		{name: "external", to: "external@example.net", aliases: []string{"ritelt.com=ritel.si"}},
		{name: "already new", to: "user@ritel.si", aliases: []string{"ritelt.com=ritel.si"}},
		{name: "disabled", to: "user@ritelt.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv, port := startFakeSMTPServer(t, true, false)
			svc, err := ProvideEmailService(nil, nil, &config.Config{EmailDelivery: config.EmailDeliveryConfig{RecipientDomainAliases: tt.aliases}})
			require.NoError(t, err)
			require.NoError(t, svc.SendEmailWithConfig(smtpTestConfig(port, true), tt.to, "Subject", "body"))
			srv.mu.Lock()
			messages := append([]string(nil), srv.messages...)
			commands := append([]string(nil), srv.commands...)
			srv.mu.Unlock()
			require.Contains(t, commands, "RCPT TO:<"+tt.to+">")
			require.Len(t, messages, 1)
			parsed, err := mail.ReadMessage(strings.NewReader(messages[0]))
			require.NoError(t, err)
			recipient, err := mail.ParseAddress(parsed.Header.Get("To"))
			require.NoError(t, err)
			require.Equal(t, tt.to, recipient.Address)
		})
	}
}
