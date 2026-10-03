package usagestats

// AccountStats 账号使用统计
//
// cost: 既有账号统计口径（可含自定义定价、账号倍率）
// api_equivalent_cost: 官方 API 原价等值；codex-auto-review 按请求持久化的内部原价计入。
// 值为 nil 表示所有请求均无法核价；部分缺价时返回已核价小计与缺价请求数。
// standard_cost: 标准费用（使用 total_cost，不含倍率）
// user_cost: 用户/API Key 口径费用（使用 actual_cost，受分组倍率影响）
type AccountStats struct {
	Requests                            int64    `json:"requests"`
	Tokens                              int64    `json:"tokens"`
	Cost                                float64  `json:"cost"`
	StandardCost                        float64  `json:"standard_cost"`
	UserCost                            float64  `json:"user_cost"`
	APIEquivalentCost                   *float64 `json:"api_equivalent_cost"`
	APIEquivalentUnpricedRequests       int64    `json:"api_equivalent_unpriced_requests"`
	APIEquivalentInternalPricedRequests int64    `json:"api_equivalent_internal_priced_requests"`
}
