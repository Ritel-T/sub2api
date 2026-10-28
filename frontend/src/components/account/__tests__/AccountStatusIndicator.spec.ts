import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountStatusIndicator from '../AccountStatusIndicator.vue'
import type { Account } from '@/types'

const i18nLocale = vi.hoisted(() => ({ value: 'en' }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: i18nLocale
    })
  }
})

vi.mock('@/utils/format', async () => {
  const actual = await vi.importActual<typeof import('@/utils/format')>('@/utils/format')
  return {
    ...actual,
    formatCountdown: () => '1h'
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'antigravity',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-03-15T00:00:00Z',
    updated_at: '2026-03-15T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

describe('AccountStatusIndicator', () => {
  it.each([
    [{}, 'active'],
    [{ status: 'error', error_message: 'Upstream unavailable' }, 'error'],
    [{ rate_limit_reset_at: '2099-01-01T00:00:00Z' }, 'rateLimited'],
    [{ overload_until: '2099-01-01T00:00:00Z' }, 'overloaded'],
    [{ temp_unschedulable_until: '2099-01-01T00:00:00Z' }, 'tempUnschedulable'],
    [{ schedulable: false }, 'paused'],
  ] as [Partial<Account>, string][])('keeps the BPS badge above the %s status', (overrides, status) => {
    const wrapper = mount(AccountStatusIndicator, {
      props: { account: makeAccount({ platform: 'openai', extra: { openai_excel_bps: true }, ...overrides }) },
      global: { stubs: { Icon: true } },
    })

    const badge = wrapper.get('[data-testid="bps-status-badge"]')
    expect(badge.text()).toBe('bps')
    expect(badge.attributes('title')).toBe('admin.accounts.openai.excelBPS')
    expect(wrapper.element.firstElementChild).toBe(badge.element)
    expect(badge.element.nextElementSibling?.textContent).toContain(`admin.accounts.status.${status}`)
  })

  it.each([
    { extra: undefined },
    { extra: { openai_excel_bps: false } },
    { extra: { openai_excel_bps: 'true' } },
    { platform: 'anthropic' },
    { type: 'apikey' },
    { parent_account_id: 2 },
    { credentials: { plan_type: ' Free ' } },
    { credentials: { auth_mode: ' agentIdentity ' } },
    { credentials: { auth_mode: 'personalAccessToken' } },
    { credentials: { openai_auth_mode: ' PERSONAL_ACCESS_TOKEN ' } },
  ] as Partial<Account>[])('hides BPS for disabled or unsupported accounts: %j', overrides => {
    const wrapper = mount(AccountStatusIndicator, {
      props: { account: makeAccount({ platform: 'openai', extra: { openai_excel_bps: true }, ...overrides }) },
    })

    expect(wrapper.find('[data-testid="bps-status-badge"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.status.active')
  })

  it('updates the BPS badge when the account setting changes', async () => {
    const account = makeAccount({ platform: 'openai', extra: { openai_excel_bps: false } })
    const wrapper = mount(AccountStatusIndicator, { props: { account } })
    expect(wrapper.find('[data-testid="bps-status-badge"]').exists()).toBe(false)

    await wrapper.setProps({ account: { ...account, extra: { openai_excel_bps: true } } })
    expect(wrapper.find('[data-testid="bps-status-badge"]').exists()).toBe(true)

    await wrapper.setProps({ account })
    expect(wrapper.find('[data-testid="bps-status-badge"]').exists()).toBe(false)
  })

  it('hides the BPS badge when the global protocol switch is off', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({ platform: 'openai', extra: { openai_excel_bps: true } }),
        globalBpsEnabled: false,
      },
    })

    expect(wrapper.find('[data-testid="bps-status-badge"]').exists()).toBe(false)
  })

  it('shows the RPM pause reason and clears it when refreshed after reset', async () => {
    const account = makeAccount({ platform: 'openai', base_rpm: 10, current_rpm: 10, rpm_paused: true, rpm_reset_at: 1_900_000_020 })
    const wrapper = mount(AccountStatusIndicator, { props: { account }, global: { stubs: { Icon: true } } })
    expect(wrapper.text()).toContain('admin.accounts.status.rpmPaused')
    expect(wrapper.text()).toContain('admin.accounts.status.rpmPausedUntil')
    await wrapper.setProps({ account: { ...account, current_rpm: 0, rpm_paused: false, rpm_reset_at: undefined } })
    expect(wrapper.text()).not.toContain('admin.accounts.status.rpmPaused')
  })
  it('Claude 5 系列模型限流时显示 Opus 和 Sonnet 的短别名', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          extra: {
            model_rate_limits: {
              'claude-opus-5': {
                rate_limited_at: '2026-07-28T00:00:00Z',
                rate_limit_reset_at: '2099-07-28T00:00:00Z'
              },
              'claude-sonnet-5': {
                rate_limited_at: '2026-07-28T00:00:00Z',
                rate_limit_reset_at: '2099-07-28T00:00:00Z'
              },
              'claude-sonnet-5-5': {
                rate_limited_at: '2026-09-28T00:00:00Z',
                rate_limit_reset_at: '2099-09-28T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('COpus5')
    expect(wrapper.text()).toContain('CSon5')
    expect(wrapper.text()).toContain('CSon55')
    expect(wrapper.text()).not.toContain('claude-sonnet-5')
  })

  it('Grok 账号额度限流时显示自动恢复时间而非临时不可调度', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 5,
          name: 'grok-free-1',
          platform: 'grok',
          rate_limited_at: '2026-07-11T12:00:00Z',
          rate_limit_reset_at: '2099-07-11T13:00:00Z',
          temp_unschedulable_until: '2099-07-11T12:30:00Z',
          temp_unschedulable_reason: 'legacy grok rate limited'
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.find('.badge-warning').text()).toBe('admin.accounts.status.rateLimited')
    expect(wrapper.text()).toContain('admin.accounts.status.rateLimitedAutoResume')
    expect(wrapper.text()).not.toContain('admin.accounts.status.tempUnschedulable')
  })

  it('模型限流 + overages 启用 + 无 AICredits key → 显示 ⚡ (credits_active)', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 1,
          name: 'ag-1',
          extra: {
            allow_overages: true,
            model_rate_limits: {
              'claude-sonnet-4-5': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('⚡')
    expect(wrapper.text()).toContain('CSon45')
  })

  it('模型限流 + overages 未启用 → 普通限流样式（无 ⚡）', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 2,
          name: 'ag-2',
          extra: {
            model_rate_limits: {
              'claude-sonnet-4-5': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('CSon45')
    expect(wrapper.text()).not.toContain('⚡')
  })

  it('AICredits key 生效 → 显示积分已用尽 (credits_exhausted)', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 3,
          name: 'ag-3',
          extra: {
            allow_overages: true,
            model_rate_limits: {
              'AICredits': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.status.creditsExhausted')
  })

  it('模型限流 + overages 启用 + AICredits key 生效 → 普通限流样式（积分耗尽，无 ⚡）', () => {
    const wrapper = mount(AccountStatusIndicator, {
      props: {
        account: makeAccount({
          id: 4,
          name: 'ag-4',
          extra: {
            allow_overages: true,
            model_rate_limits: {
              'claude-sonnet-4-5': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              },
              'AICredits': {
                rate_limited_at: '2026-03-15T00:00:00Z',
                rate_limit_reset_at: '2099-03-15T00:00:00Z'
              }
            }
          }
        })
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    // 模型限流 + 积分耗尽 → 不应显示 ⚡
    expect(wrapper.text()).toContain('CSon45')
    expect(wrapper.text()).not.toContain('⚡')
    // AICredits 积分耗尽状态应显示
    expect(wrapper.text()).toContain('admin.accounts.status.creditsExhausted')
  })
})

describe('Borrowing quality requirement labels', () => {
  it('shows only models requiring borrowing and does not imply current route readiness', () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { openai_gateway_borrow_models: ['gpt-6.1-sol'], quality_candy_models: { 'gpt-6-astra': { state: 'healthy' }, 'gpt-6.1-sol': { state: 'degraded' } } } }) }, global: { stubs: { Icon: true } } })
    const badges = w.findAll('[data-testid="borrow-required-badge"]')
    expect(badges).toHaveLength(1)
    expect(badges[0].text()).toContain('6.1 Sol')
    expect(badges[0].attributes('title')).toBe('admin.astraGateway.borrowRequiredDegradedHint')
    w.unmount()
  })

  it('recognizes model-specific native degraded evidence and aliases, without guessing from inconclusive results', () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { openai_gateway_borrow_models: ['gpt-6-sol', 'gpt-6-astra'], quality_candy_models: { 'gpt-6-astra': { state: 'degraded' } } } }) }, global: { stubs: { Icon: true } } })
    expect(w.findAll('[data-testid="borrow-required-badge"]')).toHaveLength(2)
    w.unmount()
    const noRequirement = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { quality_candy_models: { 'gpt-6-astra': { state: 'inconclusive' }, 'gpt-6.1-sol': { state: 'healthy' } } } }) }, global: { stubs: { Icon: true } } })
    expect(noRequirement.find('[data-testid="borrow-required-badge"]').exists()).toBe(false)
    noRequirement.unmount()
  })

  it('does not show OAuth borrowing labels on other account types', () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', type: 'apikey', extra: { openai_gateway_borrow_models: ['gpt-6-astra'] } }) }, global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="borrow-required-badge"]').exists()).toBe(false)
    w.unmount()
  })
})

describe('Independent borrowing policy and route readiness', () => {
  const expiry = '2099-01-01T00:00:00Z'
  const routes = [
    { account_id: 1, model: 'gpt-6-astra', state: 'ready', reason: 'target_probe_passed', expires_at: expiry, remaining_seconds: 100, active: true },
    { account_id: 1, model: 'gpt-6.1-sol', state: 'failed', reason: 'target_quality_failed', retry_at: '2099-01-01T00:00:30Z', remaining_seconds: 0, active: false }
  ]
  it('does not label legacy unclassified policy as confirmed native degradation', () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { openai_gateway_borrow_models: ['gpt-6.1-sol'] } }) }, global: { stubs: { Icon: true } } })
    expect(w.get('[data-testid="borrow-required-badge"]').attributes('title')).toBe('admin.astraGateway.borrowRequiredHint')
    expect(w.get('[data-testid="borrow-route-badge"]').text()).toContain('routeStates.unknown')
    w.unmount()
  })
  it('requires a policy marker instead of treating native quality evidence as configured borrowing', () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { quality_candy_models: { 'gpt-6-astra': { state: 'degraded' } } } }) }, global: { stubs: { Icon: true } } })
    expect(w.find('[data-testid="borrow-required-badge"]').exists()).toBe(false)
    w.unmount()
  })
  it('shows ready Astra and failed Sol separately and expires readiness without another request', async () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { openai_gateway_borrow_models: ['gpt-6-astra', 'gpt-6.1-sol'] } }), borrowRoutes: routes, borrowRuntimeLoaded: true, borrowNow: Date.parse('2098-12-31T23:59:59Z') }, global: { stubs: { Icon: true } } })
    const badges = w.findAll('[data-testid="borrow-route-badge"]')
    expect(badges[0].text()).toContain('routeStates.ready')
    expect(badges[1].text()).toContain('routeStates.unavailable')
    expect(badges[1].attributes('title')).toContain('testReasons.answerFailed')
    expect(w.get('[data-testid="borrow-retry-at"]').text()).toContain('retryAt')
    await w.setProps({ borrowNow: Date.parse(expiry) })
    expect(w.findAll('[data-testid="borrow-route-badge"]')[0].text()).toContain('routeStates.expired')
    await w.setProps({ borrowRuntimeError: true })
    expect(w.findAll('[data-testid="borrow-route-badge"]')[0].text()).toContain('routeStates.unknown')
    w.unmount()
  })
  it('does not borrow another account or model readiness', () => {
    const w = mount(AccountStatusIndicator, { props: { account: makeAccount({ platform: 'openai', extra: { openai_gateway_borrow_models: ['gpt-6.1-sol'] } }), borrowRoutes: [routes[0], { ...routes[0], account_id: 2, model: 'gpt-6.1-sol' }], borrowRuntimeLoaded: true, borrowNow: Date.parse('2098-12-31T23:59:59Z') }, global: { stubs: { Icon: true } } })
    expect(w.get('[data-testid="borrow-route-badge"]').text()).toContain('routeStates.waiting')
    w.unmount()
  })
})


describe('Compact account status summary', () => {
  const now = Date.parse('2026-10-28T12:00:00Z')
  const account = makeAccount({ platform: 'openai', extra: { openai_gateway_borrow_models: ['gpt-6-astra', 'gpt-6.1-sol'] } })
  const routes = [
    { account_id: 1, model: 'gpt-6-astra', state: 'ready', reason: 'target_probe_passed', expires_at: '2026-10-28T12:00:01Z', remaining_seconds: 1, active: true },
    { account_id: 1, model: 'gpt-6.1-sol', state: 'failed', reason: 'target_quality_failed', retry_at: '2026-10-28T12:05:00Z', remaining_seconds: 0, active: false },
  ]
  it.each(['zh', 'zh-CN'])('uses Simplified Chinese compact labels for actual locale %s', currentLocale => {
    i18nLocale.value = currentLocale
    const wrapper = mount(AccountStatusIndicator, { props: { compact: true, account, borrowNow: now }, global: { stubs: { Icon: true } } })
    try {
      const summary = wrapper.get('[data-testid="compact-account-status"]')
      expect(summary.find('.account-status-primary').exists()).toBe(false)
      expect(summary.findAll('[data-testid="compact-borrow-route"]').map(item => item.text())).toEqual(['Astra · 未核验', 'Sol · 未核验'])
      expect(summary.text()).not.toContain('未核驗')
    } finally {
      wrapper.unmount()
      i18nLocale.value = 'en'
    }
  })

  it('keeps borrowing and per-model readiness concise without showing an unverified account as active', async () => {
    const wrapper = mount(AccountStatusIndicator, { props: { compact: true, account, borrowNow: now }, global: { stubs: { Icon: true } } })
    const summary = wrapper.get('[data-testid="compact-account-status"]')
    expect(summary.text()).not.toContain('Borrowing required')
    expect(summary.find('.account-status-primary').exists()).toBe(false)
    expect(summary.findAll('[data-testid="compact-borrow-route"]').map(item => item.text())).toEqual(['Astra · Unverified', 'Sol · Unverified'])
    expect(summary.text()).not.toContain('admin.accounts.status.active')
    expect(summary.text()).not.toContain('retryAt')
    expect(summary.find('.badge').exists()).toBe(false)
    wrapper.unmount()
  })
  it('keeps per-model lines separate from BPS and the details control without a repeated borrowing heading', () => {
    const wrapper = mount(AccountStatusIndicator, { props: {
      compact: true, borrowNow: now, account: { ...account, extra: { ...account.extra, openai_excel_bps: true } }
    }, global: { stubs: { Icon: true } } })
    const summary = wrapper.get('[data-testid="compact-account-status"]')
    expect(summary.get('.account-status-lines').findAll('[data-testid="compact-borrow-route"]').map(item => item.text())).toEqual(['Astra · Unverified', 'Sol · Unverified'])
    expect(summary.get('.account-status-controls').text()).toBe('bps')
    expect(summary.get('.account-status-lines').text()).not.toContain('bps')
    expect(summary.find('.account-status-primary').exists()).toBe(false)
    expect(summary.text()).not.toContain('admin.accounts.status.active')
    wrapper.unmount()
  })

  it('keeps failed Sol separate from ready Astra, then expires route readiness and retains details in the popover', async () => {
    const wrapper = mount(AccountStatusIndicator, { attachTo: document.body, props: { compact: true, account, borrowNow: now, borrowRuntimeLoaded: true, borrowRoutes: routes }, global: { stubs: { Icon: true } } })
    const summary = wrapper.get('[data-testid="compact-account-status"]')
    expect(summary.findAll('[data-testid="compact-borrow-route"]').map(item => item.text())).toEqual(['Astra · Ready', 'Sol · Unavailable'])
    await summary.trigger('click')
    const detail = document.body.querySelector<HTMLElement>('[data-testid="account-status-details"]')!
    expect(detail.closest<HTMLElement>('[role="tooltip"]')!.style.display).not.toBe('none')
    expect(detail.textContent).toContain('admin.astraGateway.retryAt')
    expect(detail.querySelectorAll('[data-testid="borrow-required-badge"]')).toHaveLength(2)
    expect(detail.querySelectorAll('[data-testid="borrow-route-badge"]')[1].getAttribute('title')).toContain('testReasons.answerFailed')
    await wrapper.setProps({ borrowNow: now + 1000 })
    expect(summary.findAll('[data-testid="compact-borrow-route"]')[0].text()).toContain('Expired')
    await wrapper.setProps({ borrowRuntimeError: true })
    expect(summary.findAll('[data-testid="compact-borrow-route"]')[0].text()).toContain('Unverified')
    wrapper.unmount()
  })
  it('does not repeat rate-limit prose in the main summary and recovers at the exact reset boundary', async () => {
    const wrapper = mount(AccountStatusIndicator, { props: { compact: true, account: makeAccount({ rate_limit_reset_at: '2026-10-28T12:00:01Z' }), borrowNow: now }, global: { stubs: { Icon: true } } })
    const summary = wrapper.get('[data-testid="compact-account-status"]')
    expect(summary.text()).toBe('429 · admin.accounts.status.rateLimited')
    expect(summary.text()).not.toContain('rateLimitedAutoResume')
    await wrapper.setProps({ borrowNow: now + 1000 })
    expect(summary.text()).toBe('admin.accounts.status.active')
    wrapper.unmount()
  })
  it('keeps errors and inactive states ahead of borrowed readiness', () => {
    for (const state of ['error', 'inactive'] as const) {
      const wrapper = mount(AccountStatusIndicator, { props: { compact: true, account: { ...account, status: state }, borrowRuntimeLoaded: true, borrowRoutes: routes, borrowNow: now }, global: { stubs: { Icon: true } } })
      expect(wrapper.get('.account-status-primary').text()).toBe(`admin.accounts.status.${state}`)
      expect(wrapper.get('.account-status-primary').classes().join(' ')).not.toContain('text-emerald')
      wrapper.unmount()
    }
  })
  it('counts model limits once in the main row while retaining exact models in details', () => {
    const wrapper = mount(AccountStatusIndicator, { props: { compact: true, account: makeAccount({ extra: { model_rate_limits: {
      'claude-opus-5': { rate_limited_at: '2026-10-28T11:00:00Z', rate_limit_reset_at: '2026-10-28T13:00:00Z' },
      'claude-sonnet-5': { rate_limited_at: '2026-10-28T11:00:00Z', rate_limit_reset_at: '2026-10-28T13:00:00Z' },
    } } }), borrowNow: now }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="compact-model-limits"]').text()).toBe('2 model limits')
    expect(wrapper.get('[data-testid="compact-account-status"]').text()).not.toContain('COpus5')
    wrapper.unmount()
  })
})
