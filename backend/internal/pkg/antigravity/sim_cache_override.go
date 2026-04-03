package antigravity

// SimCacheOverride 模拟缓存决策（在 Forward 前由 Handler 计算）。
// 非 nil 时，transform 层根据此决策 + Gemini 返回的 promptTokenCount 计算最终 usage 分配。
// [OpusClaw Patch] simulated cache billing
type SimCacheOverride struct {
	HistoryCachedTokenCount int  // 上一轮累积的 prompt token 数（来自 Redis）
	IsMiss                  bool // 本轮是否缓存丢失（概率掷骰结果）
	IsFirstTurn             bool // 是否第 1 轮（无历史状态）
}

// ApplySimCacheOverride 根据决策和实际 promptTokenCount 计算最终 usage 分配。
// 当 override 为 nil 时返回 false，调用方应 fallback 到 SplitUncachedTokens。
// [OpusClaw Patch] simulated cache billing
func ApplySimCacheOverride(override *SimCacheOverride, totalPromptTokens int) (cacheRead, cacheCreation, inputTokens int, applied bool) {
	if override == nil {
		return 0, 0, 0, false
	}

	if override.IsFirstTurn || override.IsMiss {
		return 0, totalPromptTokens, 0, true
	}

	// 命中
	cacheRead = override.HistoryCachedTokenCount
	if cacheRead > totalPromptTokens {
		cacheRead = totalPromptTokens
	}

	cacheCreation = totalPromptTokens - cacheRead
	inputTokens = 0
	applied = true
	return
}
