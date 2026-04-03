package repository

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSimCacheKey(t *testing.T) {
	tests := []struct {
		name        string
		groupID     int64
		sessionHash string
		expected    string
	}{
		{
			name:        "normal_ids",
			groupID:     1,
			sessionHash: "abc123",
			expected:    "simcache:1:abc123",
		},
		{
			name:        "zero_group",
			groupID:     0,
			sessionHash: "hash",
			expected:    "simcache:0:hash",
		},
		{
			name:        "empty_session_hash",
			groupID:     42,
			sessionHash: "",
			expected:    "simcache:42:",
		},
		{
			name:        "max_int64_group",
			groupID:     math.MaxInt64,
			sessionHash: "s",
			expected:    "simcache:9223372036854775807:s",
		},
		{
			name:        "complex_session_hash",
			groupID:     5,
			sessionHash: "sha256:abcdef0123456789",
			expected:    "simcache:5:sha256:abcdef0123456789",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSimCacheKey(tc.groupID, tc.sessionHash)
			require.Equal(t, tc.expected, got)
		})
	}
}
