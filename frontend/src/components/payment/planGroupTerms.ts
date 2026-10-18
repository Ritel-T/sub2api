/** Group quota semantics match Group.Has*Limit: only positive values impose a cap. */
export function hasPositivePlanLimit(value: number | null | undefined): boolean {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

// Match only explicit quota claims, never ordinary benefits mentioning a day/week.
const explicitQuotaClaim = /^(?:[日周月](?:限额|额度|配额|用量|预算)|每[日周月](?:限额|额度|配额|用量|预算)|(?:daily|weekly|monthly)\s+(?:quota|limit|allowance|budget)|(?:quota|limit|allowance|budget)\s+per\s+(?:day|week|month))\s*[:：=]?\s*(?:[$￥¥＄]?\s*\d|unlimited|无限(?:制)?|不限额)/i
const currencyPerPeriod = /^(?:[$￥¥＄]\s*\d[\d,.]*|\d[\d,.]*\s*(?:USD|CNY|美元|美金|元|刀))\s*(?:\/|per\s+)\s*(?:day|week|month|[日周月])\s*(?:(?:quota|limit|allowance|budget|额度|配额|限额|预算|用量)\s*)?$/i
const periodCurrency = /^每[日周月]\s*[:：=]?\s*(?:[$￥¥＄]\s*\d[\d,.]*|\d[\d,.]*\s*(?:USD|CNY|美元|美金|元|刀|[$￥¥＄]))\s*(?:额度|配额|限额|预算|用量)?\s*$/i

/** Hide stale manual quota claims; structured group limits are shown above them. */
export function planCustomFeatures(features: string[]): string[] {
  return features.filter(feature => !explicitQuotaClaim.test(feature.trim()) && !currencyPerPeriod.test(feature.trim()) && !periodCurrency.test(feature.trim()))
}
