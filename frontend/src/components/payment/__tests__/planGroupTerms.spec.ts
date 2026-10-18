import { describe, expect, it } from 'vitest'
import { hasPositivePlanLimit, planCustomFeatures } from '../planGroupTerms'

describe('plan group terms', () => {
  it('uses the same positive-only quota semantics as the gateway', () => {
    for (const value of [undefined, null, -1, 0, NaN, Infinity]) expect(hasPositivePlanLimit(value)).toBe(false)
    expect(hasPositivePlanLimit(0.01)).toBe(true)
  })

  it('removes explicit stale quota claims but preserves custom benefits and daily service promises', () => {
    const stale = ['日额度：$10', '周限额 70 美元', '月配额: 280', '每日 $10', '每月 280 USD 额度', '$280/month', 'Daily quota: $10', 'Weekly limit 70 USD', 'Monthly allowance: Unlimited']
    const custom = ['Priority support', '每日客服支持', '每周一次咨询', 'Daily model updates', '30 days of service', '每月 20 元返现', 'Daily support for 100 models']
    expect(planCustomFeatures([...stale, ...custom])).toEqual(custom)
  })
})
