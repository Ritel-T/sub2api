package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"net/http"
)

type alphaSearchResponseTransformKey struct{}

type AlphaSearchResponseTransform func([]byte) ([]byte, bool, error)

// WithAlphaSearchResponseTransform applies a protocol adapter before any success
// bytes or charge are committed. Ordinary alpha requests have no adapter.
func WithAlphaSearchResponseTransform(ctx context.Context, transform AlphaSearchResponseTransform) context.Context {
	return context.WithValue(ctx, alphaSearchResponseTransformKey{}, transform)
}

func hasAlphaSearchResponseTransform(ctx context.Context) bool {
	transform, _ := ctx.Value(alphaSearchResponseTransformKey{}).(AlphaSearchResponseTransform)
	return transform != nil
}

func transformAlphaSearchResponse(ctx context.Context, body []byte) ([]byte, int, error) {
	transform, _ := ctx.Value(alphaSearchResponseTransformKey{}).(AlphaSearchResponseTransform)
	if transform == nil {
		return body, 1, nil
	}
	adapted, billable, err := transform(body)
	if err != nil {
		return nil, 0, fmt.Errorf("adapt search response: %w", err)
	}
	if !billable {
		return adapted, 0, nil
	}
	return adapted, 1, nil
}

// DeepSeekToolsSchedulingContext selects an explicitly configured OpenAI pool
// without replacing the caller's API key, billing group, or quota platform.
func (s *OpenAIGatewayService) DeepSeekToolsSchedulingContext(ctx context.Context, groupID int64) (context.Context, error) {
	if s != nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, fmt.Errorf("search bridge requires group-isolated scheduling")
	}
	if groupID <= 0 || s == nil || s.channelService == nil || s.channelService.groupRepo == nil {
		return nil, fmt.Errorf("search pool is not configured")
	}
	group, err := s.channelService.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil || group == nil || group.Platform != PlatformOpenAI || group.Status != StatusActive {
		return nil, fmt.Errorf("search pool is unavailable")
	}
	// The scheduler receives the pool ID separately. ctxkey.Group must stay
	// the authenticated caller group: profit pricing and quotas use it.
	return ctx, nil
}

func writeAdaptedAlphaSearchError(ctx context.Context, c *gin.Context, status int) bool {
	if !hasAlphaSearchResponseTransform(ctx) {
		return false
	}
	c.Writer.Header().Del("Content-Length")
	c.Writer.Header().Del("Content-Encoding")
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{"error": gin.H{"type": "upstream_error", "message": "Search backend request failed"}})
	return true
}
