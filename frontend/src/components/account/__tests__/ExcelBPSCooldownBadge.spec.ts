import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ExcelBPSCooldownBadge from '../ExcelBPSCooldownBadge.vue'
import AccountStatusIndicator from '../AccountStatusIndicator.vue'
import en from '@/i18n/locales/en'
import { formatCountdown, formatDateTime } from '@/utils/format'
import type { Account, AccountListItem } from '@/types'

function translate(key: string, values: Record<string, string | number> = {}): string {
  const message = key.split('.').reduce<unknown>((value, name) =>
    (value as Record<string, unknown>)?.[name], en)
  return (typeof message === 'string' ? message : key)
    .replace(/\{(\w+)\}/g, (_, token) => String(values[token] ?? token))
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: translate })
  }
})

vi.mock('@/i18n', () => ({
  getLocale: () => 'en',
  i18n: { global: { t: translate } }
}))

vi.mock('@/utils/format', async () => {
  const actual = await vi.importActual<typeof import('@/utils/format')>('@/utils/format')
  return { ...actual, formatCountdown: vi.fn(actual.formatCountdown) }
})

type BadgeAccount = Pick<AccountListItem, 'platform' | 'type' | 'extra'>
const now = new Date('2026-09-27T03:00:00Z')
const reset = '2026-09-27T03:02:00Z'
const selector = '[data-test="excel-bps-cooldown-badge"]'

function account(extra: Record<string, unknown> = {}): BadgeAccount {
  return {
    platform: 'openai',
    type: 'oauth',
    extra: {
      openai_excel_bps: true,
      openai_excel_bps_rate_limit_reset_at: reset,
      openai_excel_bps_rate_limit_reason: 'quota_exhausted',
      ...extra
    }
  }
}

function mountBadge(value = account()) {
  return mount(ExcelBPSCooldownBadge, {
    props: { account: value },
    global: { stubs: { Icon: true } }
  })
}

enableAutoUnmount(afterEach)

describe('ExcelBPSCooldownBadge', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('shows the BPS scope, reason, full reset time and native-route caveat', () => {
    const wrapper = mountBadge()
    const badge = wrapper.get(selector)
    expect(badge.text()).toContain('BPS cooldown')
    expect(badge.text()).toContain(formatCountdown(reset))
    expect(badge.attributes('title')).toBe(
      `BPS quota exhausted. BPS automatically resumes at ${formatDateTime(reset)}. Native routes may remain available.`
    )
    expect(badge.attributes('aria-label')).toBe(badge.attributes('title'))
  })

  it.each(['rate_limited', undefined, 'unexpected upstream text'])(
    'uses a safe generic reason for %s', (reason) => {
      const wrapper = mountBadge(account({ openai_excel_bps_rate_limit_reason: reason }))
      expect(wrapper.get(selector).attributes('title')).toContain('BPS rate limited.')
      expect(wrapper.get(selector).attributes('title')).not.toContain('unexpected upstream text')
    }
  )

  it.each([null, undefined, '', 'invalid', 1790478120000, '2026-09-27T03:00:00Z', '2026-09-26T03:00:00Z'])(
    'hides missing, invalid or expired reset %s without starting a timer', (value) => {
      const wrapper = mountBadge(account({ openai_excel_bps_rate_limit_reset_at: value }))
      expect(wrapper.find(selector).exists()).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
    }
  )

  it.each([false, undefined, 'true', 1])(
    'hides the badge unless BPS is explicitly enabled (%s)', (value) => {
      const wrapper = mountBadge(account({ openai_excel_bps: value }))
      expect(wrapper.find(selector).exists()).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
    }
  )

  it('hides non-OpenAI and non-OAuth accounts', () => {
    expect(mountBadge({ ...account(), platform: 'anthropic' }).find(selector).exists()).toBe(false)
    expect(mountBadge({ ...account(), type: 'apikey' }).find(selector).exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('counts down through the last minute and disappears at expiry without a refresh', async () => {
    const wrapper = mountBadge(account({ openai_excel_bps_rate_limit_reset_at: '2026-09-27T03:00:02Z' }))
    expect(wrapper.text()).toContain('2s')
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.text()).toContain('1s')
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.find(selector).exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('updates a multi-minute countdown while the same account remains mounted', async () => {
    const wrapper = mountBadge()
    const initial = wrapper.get(selector).text()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(wrapper.get(selector).text()).not.toBe(initial)
    expect(wrapper.get(selector).text()).toContain(formatCountdown(reset))
  })

  it('accepts a timezone offset and fractional seconds', async () => {
    const wrapper = mountBadge(account({ openai_excel_bps_rate_limit_reset_at: '2026-09-27T12:00:01.123456789+09:00' }))
    expect(wrapper.get(selector).text()).toContain('2s')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.find(selector).exists()).toBe(false)
  })

  it('falls back to the full reset date when no countdown is available', () => {
    vi.mocked(formatCountdown).mockReturnValueOnce(null)
    const wrapper = mountBadge()
    expect(wrapper.get(selector).text()).toContain(formatDateTime(reset))
    expect(wrapper.get(selector).attributes('title')).toContain(formatDateTime(reset))
  })

  it('responds to reset extensions, cleared state and BPS being disabled', async () => {
    const wrapper = mountBadge()
    const extended = '2026-09-28T03:00:00Z'
    await wrapper.setProps({ account: account({ openai_excel_bps_rate_limit_reset_at: extended }) })
    expect(wrapper.get(selector).attributes('title')).toContain(formatDateTime(extended))
    await wrapper.setProps({ account: account({ openai_excel_bps_rate_limit_reset_at: undefined }) })
    expect(wrapper.find(selector).exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await wrapper.setProps({ account: account() })
    expect(wrapper.find(selector).exists()).toBe(true)
    await wrapper.setProps({ account: account({ openai_excel_bps: false }) })
    expect(wrapper.find(selector).exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('stops its timer when unmounted', async () => {
    const wrapper = mountBadge()
    expect(vi.getTimerCount()).toBe(1)
    wrapper.unmount()
    await nextTick()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps the native account status and scheduling flag intact beside the BPS badge', () => {
    const value = {
      ...account(), id: 1, name: 'BPS account', status: 'active', schedulable: true
    } as Account
    const before = JSON.stringify(value)
    const wrapper = mount(AccountStatusIndicator, {
      props: { account: value },
      global: { stubs: { Icon: true } }
    })
    expect(wrapper.get('.badge-success').text()).toBe('Active')
    expect(wrapper.get(selector).text()).toContain('BPS cooldown')
    expect(wrapper.text()).not.toContain('Paused')
    expect(wrapper.text()).not.toContain('Temp Unschedulable')
    expect(JSON.stringify(value)).toBe(before)
    expect(wrapper.emitted('show-temp-unsched')).toBeUndefined()
  })
})
