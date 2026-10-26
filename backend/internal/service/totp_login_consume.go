package service

import "context"

// totpLoginSessionConsumer is optional so existing TotpCache implementations
// remain compatible. Login completion must fail closed without atomic consume.
type totpLoginSessionConsumer interface {
	ConsumeLoginSession(context.Context, string) (*TotpLoginSession, error)
}

// ConsumeLoginSession atomically takes a pending login challenge. A missing or
// already consumed challenge returns nil, nil. Errors have no retry fallback:
// the cache may already have consumed the challenge before losing its reply.
func (s *TotpService) ConsumeLoginSession(ctx context.Context, tempToken string) (*TotpLoginSession, error) {
	if s == nil || s.cache == nil {
		return nil, ErrServiceUnavailable
	}
	consumer, ok := s.cache.(totpLoginSessionConsumer)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	session, err := consumer.ConsumeLoginSession(ctx, tempToken)
	if err != nil {
		// Cache errors can contain keys or payloads. Do not expose them to callers.
		return nil, ErrServiceUnavailable
	}
	return session, nil
}
