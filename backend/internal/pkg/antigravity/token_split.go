package antigravity

import (
	"math/rand/v2"
)

func SplitUncachedTokens(uncached int) (cacheCreation int, inputTokens int) {
	if uncached <= 0 {
		return 0, 0
	}

	if uncached <= 10 {
		return uncached, 0
	}

	ratio := rand.Float64()*0.05 + 0.90
	cacheCreation = int(float64(uncached) * ratio)
	inputTokens = uncached - cacheCreation

	return cacheCreation, inputTokens
}
