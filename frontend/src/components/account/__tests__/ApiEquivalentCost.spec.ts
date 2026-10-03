import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import { mount } from '@vue/test-utils'
import ApiEquivalentCost from '../ApiEquivalentCost.vue'
import AccountTodayStatsCell from '../AccountTodayStatsCell.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

// The app uses the CSP-safe runtime build. Supply precompiled message functions
// here so the component test verifies actual Chinese/English text and interpolation.
function compiledMessages(messages: typeof en) {
  const compile = (text: string) => new Function(`return ${baseCompile(text).code}`)()
  return {
    usage: Object.fromEntries(Object.entries(messages.usage).map(([key, value]) => [key, compile(value)])),
    admin: { accounts: { stats: Object.fromEntries(Object.entries(messages.admin.accounts.stats).map(([key, value]) => [key, compile(value)])) } }
  }
}

function i18n(locale = 'zh') {
  return createI18n({ legacy: false, locale, messages: { en: compiledMessages(en), zh: compiledMessages(zh) } })
}

describe('API list-price equivalent display', () => {
  it.each(['zh', 'en'])('shows official equivalent and its meaning in %s', (locale) => {
    const wrapper = mount(ApiEquivalentCost, {
      props: { cost: 1421.375, unpricedRequests: 0 },
      global: { plugins: [i18n(locale)] }
    })
    expect(wrapper.text()).toBe('A $1421.38')
    const title = wrapper.get('span').attributes('title')
    expect(title).toContain(locale === 'zh' ? 'API 原价等值' : 'API list-price equivalent')
    expect(title).toContain(locale === 'zh' ? '长上下文' : 'long-context')
    expect(title).toContain(locale === 'zh' ? '不代表 Pro 订阅实际扣费' : 'does not represent actual Pro subscription charges')
  })

  it.each(['zh', 'en'])('reports internal review pricing honestly in %s', (locale) => {
    const wrapper = mount(ApiEquivalentCost, {
      props: { cost: 1421.37971388, unpricedRequests: 0, internalPricedRequests: 2 },
      global: { plugins: [i18n(locale)] }
    })
    expect(wrapper.text()).toBe('A $1421.38')
    const title = wrapper.attributes('title')
    expect(title).toContain(locale === 'zh'
      ? '其中 2 条 codex-auto-review 请求按 Sub2API 内部设定的原价计入'
      : 'Includes 2 codex-auto-review requests at the recorded Sub2API internal base price')
    expect(title).not.toContain(locale === 'zh' ? '未计价请求' : 'Unpriced requests')
  })

  it('does not describe ordinary official-price rows as internally priced', () => {
    const wrapper = mount(ApiEquivalentCost, {
      props: { cost: .01, internalPricedRequests: 0 },
      global: { plugins: [i18n()] }
    })
    expect(wrapper.attributes('title')).not.toContain('codex-auto-review')
  })

  it('marks a known subtotal as partial and reports unpriced requests', () => {
    const wrapper = mount(ApiEquivalentCost, {
      props: { cost: 1421.375, unpricedRequests: 2 },
      global: { plugins: [i18n()] }
    })
    expect(wrapper.text()).toBe('A ≥$1421.38')
    expect(wrapper.attributes('title')).toContain('未计价请求：2')
    expect(wrapper.attributes('title')).toContain('仅包含已计价请求')
  })

  it.each([null, undefined, NaN, Infinity, -1])('never invents an equivalent for unavailable cost %s', (cost) => {
    const wrapper = mount(ApiEquivalentCost, {
      props: { cost, unpricedRequests: 4 },
      global: { plugins: [i18n()] }
    })
    expect(wrapper.text()).toBe('A —')
    expect(wrapper.attributes('title')).toContain('暂无可用的 API 原价等值')
    expect(wrapper.attributes('title')).toContain('未计价请求：4')
  })

  it('distinguishes recorded zero cost from unavailable cost', () => {
    const wrapper = mount(ApiEquivalentCost, {
      props: { cost: 0, unpricedRequests: 0 },
      global: { plugins: [i18n()] }
    })
    expect(wrapper.text()).toBe('A $0.00')
  })

  it('uses independent daily and lifetime equivalents without falling back to account costs', () => {
    const wrapper = mount(AccountTodayStatsCell, {
      props: {
        stats: {
          requests: 1700, tokens: 558800000,
          cost: 836, lifetime_cost: 940,
          api_equivalent_cost: 1421.375, api_equivalent_unpriced_requests: 2, api_equivalent_internal_priced_requests: 4,
          lifetime_api_equivalent_cost: 2421.375, lifetime_api_equivalent_unpriced_requests: 3, lifetime_api_equivalent_internal_priced_requests: 5
        }
      },
      global: { plugins: [i18n()] }
    })
    expect(wrapper.text()).toContain('今日 API 原价等值')
    expect(wrapper.text()).toContain('累计 API 原价等值')
    const costs = wrapper.findAll('[data-test="api-equivalent-cost"]')
    expect(costs.map(cost => cost.text())).toEqual(['≥$1421.38', '≥$2421.38'])
    expect(costs[0].attributes('title')).toContain('未计价请求：2')
    expect(costs[1].attributes('title')).toContain('未计价请求：3')
    expect(costs[0].attributes('title')).toContain('其中 4 条 codex-auto-review')
    expect(costs[1].attributes('title')).toContain('其中 5 条 codex-auto-review')
    expect(wrapper.text()).not.toContain('$836')
    expect(wrapper.text()).not.toContain('$940')
  })
})
