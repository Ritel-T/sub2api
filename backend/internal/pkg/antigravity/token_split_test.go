//go:build unit

package antigravity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitUncachedTokens_Normal(t *testing.T) {
	uncached := 1000
	cacheCreation, inputTokens := SplitUncachedTokens(uncached)
	
	assert.GreaterOrEqual(t, cacheCreation, 900)
	assert.LessOrEqual(t, cacheCreation, 950)
	assert.Equal(t, uncached, cacheCreation+inputTokens)
}

func TestSplitUncachedTokens_Small(t *testing.T) {
	uncached := 5
	cacheCreation, inputTokens := SplitUncachedTokens(uncached)
	
	assert.Equal(t, 5, cacheCreation)
	assert.Equal(t, 0, inputTokens)
}

func TestSplitUncachedTokens_Zero(t *testing.T) {
	cacheCreation, inputTokens := SplitUncachedTokens(0)
	assert.Equal(t, 0, cacheCreation)
	assert.Equal(t, 0, inputTokens)
	
	cacheCreationNeg, inputTokensNeg := SplitUncachedTokens(-10)
	assert.Equal(t, 0, cacheCreationNeg)
	assert.Equal(t, 0, inputTokensNeg)
}

func TestSplitUncachedTokens_Distribution(t *testing.T) {
	uncached := 1000
	for i := 0; i < 100; i++ {
		cacheCreation, inputTokens := SplitUncachedTokens(uncached)
		assert.GreaterOrEqual(t, cacheCreation, 900)
		assert.LessOrEqual(t, cacheCreation, 950)
		assert.Equal(t, uncached, cacheCreation+inputTokens)
	}
}

func TestSplitUncachedTokens_TotalPreserved(t *testing.T) {
	inputs := []int{0, 1, 10, 11, 100, 999}
	for _, input := range inputs {
		cacheCreation, inputTokens := SplitUncachedTokens(input)
		assert.Equal(t, input, cacheCreation+inputTokens, "Total should be preserved for input %d", input)
	}
}
