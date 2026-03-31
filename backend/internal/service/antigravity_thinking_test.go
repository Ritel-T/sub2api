//go:build unit

package service

import (
	"testing"
)

func TestApplyThinkingModelSuffix(t *testing.T) {
	tests := []struct {
		name            string
		mappedModel     string
		thinkingEnabled bool
		expected        string
	}{
		{
			name:            "thinking enabled - claude-sonnet-4-5 becomes thinking version",
			mappedModel:     "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        "claude-sonnet-4-5-thinking",
		},
		{
			name:            "thinking enabled - claude-opus-4-6 remains unchanged",
			mappedModel:     "claude-opus-4-6",
			thinkingEnabled: true,
			expected:        "claude-opus-4-6",
		},
		{
			name:            "thinking enabled - gemini-2.5-flash remains unchanged",
			mappedModel:     "gemini-2.5-flash",
			thinkingEnabled: true,
			expected:        "gemini-2.5-flash",
		},
		{
			name:            "thinking enabled - already has suffix",
			mappedModel:     "claude-sonnet-4-5-thinking",
			thinkingEnabled: true,
			expected:        "claude-sonnet-4-5-thinking",
		},
		{
			name:            "thinking disabled - normal model remains unchanged",
			mappedModel:     "claude-sonnet-4-5",
			thinkingEnabled: false,
			expected:        "claude-sonnet-4-5",
		},
		{
			name:            "thinking disabled - existing thinking suffix remains unchanged",
			mappedModel:     "claude-sonnet-4-5-thinking",
			thinkingEnabled: false,
			expected:        "claude-sonnet-4-5-thinking",
		},
		{
			name:            "thinking disabled - opus remains unchanged",
			mappedModel:     "claude-opus-4-6-thinking",
			thinkingEnabled: false,
			expected:        "claude-opus-4-6-thinking",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyThinkingModelSuffix(tt.mappedModel, tt.thinkingEnabled)
			if result != tt.expected {
				t.Errorf("applyThinkingModelSuffix(%q, %v) = %q, want %q",
					tt.mappedModel, tt.thinkingEnabled, result, tt.expected)
			}
		})
	}
}
