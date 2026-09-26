package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

type openAIEncryptedMessageContentContextKey struct{}

// WithOpenAIResponsesRequestCapabilities records structural requirements from the
// original Responses body. Only the boolean is retained, not message contents.
func WithOpenAIResponsesRequestCapabilities(ctx context.Context, body []byte) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIEncryptedMessageContentContextKey{}, openAIHasEncryptedMessageContent(body))
}

func openAIHasEncryptedMessageContent(body []byte) bool {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	encrypted := false
	input.ForEach(func(_, item gjson.Result) bool {
		kind := item.Get("type")
		messageType := kind.String()
		if !kind.Exists() {
			switch item.Get("role").String() {
			case "user", "assistant", "system", "developer":
				messageType = "message"
			}
		}
		switch messageType {
		case "message", "agent_message":
			content := item.Get("content")
			if content.IsArray() {
				content.ForEach(func(_, part gjson.Result) bool {
					encrypted = part.Get("type").String() == "encrypted_content"
					return !encrypted
				})
			}
		}
		return !encrypted
	})
	return encrypted
}

func openAIRequiresEncryptedMessageContent(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	required, _ := ctx.Value(openAIEncryptedMessageContentContextKey{}).(bool)
	return required
}

func openAIEncryptedMessageCapabilityMismatch(ctx context.Context, account *Account, requestedModel string) bool {
	if !openAIRequiresEncryptedMessageContent(ctx) {
		return false
	}
	if forward, ok := openAIForwardModelFromContext(ctx); ok && forward.model != "" {
		requestedModel = forward.model
	}
	return account.IsExcelBPSEnabledForModel(requestedModel)
}

// A required response owner must not become a load-balance miss just because its
// protocol changed to BPS. Keep the binding and fail this request instead.
func (s *OpenAIGatewayService) checkOpenAIEncryptedMessageResponseOwner(ctx context.Context, groupID *int64, responseID, requestedModel string) error {
	if !openAIRequiresEncryptedMessageContent(ctx) || strings.TrimSpace(responseID) == "" {
		return nil
	}
	store := s.getOpenAIWSStateStore()
	if store == nil {
		return nil
	}
	accountID, err := store.GetResponseAccount(ctx, derefGroupID(groupID), strings.TrimSpace(responseID))
	if err != nil || accountID <= 0 {
		return nil
	}
	var account *Account
	if s.accountRepo != nil {
		account, err = s.accountRepo.GetByID(ctx, accountID)
	} else {
		account, err = s.getSchedulableAccount(ctx, accountID)
	}
	if err != nil || account == nil {
		return fmt.Errorf("%w: encrypted message response owner unavailable", ErrNoAvailableAccounts)
	}
	if openAIEncryptedMessageCapabilityMismatch(ctx, account, requestedModel) {
		return fmt.Errorf("%w: encrypted_message_unsupported on required response owner", ErrNoAvailableAccounts)
	}
	return nil
}
