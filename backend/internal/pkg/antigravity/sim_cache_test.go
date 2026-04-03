//go:build unit

package antigravity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplySimCacheOverride(t *testing.T) {
	tests := []struct {
		name              string
		override          *SimCacheOverride
		totalPromptTokens int
		wantRead          int
		wantCreation      int
		wantInput         int
		wantApplied       bool
	}{
		{
			name:              "nil override",
			override:          nil,
			totalPromptTokens: 100,
			wantApplied:       false,
		},
		{
			name: "first turn",
			override: &SimCacheOverride{
				IsFirstTurn: true,
			},
			totalPromptTokens: 100,
			wantRead:          0,
			wantCreation:      100,
			wantInput:         0,
			wantApplied:       true,
		},
		{
			name: "cache miss",
			override: &SimCacheOverride{
				IsMiss: true,
			},
			totalPromptTokens: 100,
			wantRead:          0,
			wantCreation:      100,
			wantInput:         0,
			wantApplied:       true,
		},
		{
			name: "cache hit",
			override: &SimCacheOverride{
				HistoryCachedTokenCount: 80,
			},
			totalPromptTokens: 100,
			wantRead:          80,
			wantCreation:      20,
			wantInput:         0,
			wantApplied:       true,
		},
		{
			name: "cache hit with history > total",
			override: &SimCacheOverride{
				HistoryCachedTokenCount: 120,
			},
			totalPromptTokens: 100,
			wantRead:          100,
			wantCreation:      0,
			wantInput:         0,
			wantApplied:       true,
		},
		{
			name: "zero tokens",
			override: &SimCacheOverride{
				HistoryCachedTokenCount: 80,
			},
			totalPromptTokens: 0,
			wantRead:          0,
			wantCreation:      0,
			wantInput:         0,
			wantApplied:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read, creation, input, applied := ApplySimCacheOverride(tt.override, tt.totalPromptTokens)
			assert.Equal(t, tt.wantApplied, applied)
			if applied {
				assert.Equal(t, tt.wantRead, read)
				assert.Equal(t, tt.wantCreation, creation)
				assert.Equal(t, tt.wantInput, input)
				assert.Equal(t, tt.totalPromptTokens, read+creation+input)
			}
		})
	}
}
