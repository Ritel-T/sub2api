//go:build unit

package antigravity

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRandomID_Charset(t *testing.T) {
	const validChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	validSet := make(map[byte]struct{}, len(validChars))
	for i := 0; i < len(validChars); i++ {
		validSet[validChars[i]] = struct{}{}
	}

	for i := 0; i < 50; i++ {
		id := generateRandomID()
		require.Len(t, id, 12)
		for j := 0; j < len(id); j++ {
			_, ok := validSet[id[j]]
			require.True(t, ok, "ID 包含非法字符: %c (ID=%s)", id[j], id)
		}
	}
}

func TestGenerateRandomID_ConcurrentUniqueness(t *testing.T) {
	// 验证并发调用不会产生重复 ID
	const goroutines = 100
	results := make([]string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = generateRandomID()
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, goroutines)
	for _, id := range results {
		assert.False(t, seen[id], "并发调用产生了重复 ID: %s", id)
		seen[id] = true
	}
}

func BenchmarkGenerateRandomID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = generateRandomID()
	}
}
