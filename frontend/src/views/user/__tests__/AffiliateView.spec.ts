import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AffiliateView from '../AffiliateView.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'

const { copyToClipboard, getAffiliateDetail } = vi.hoisted(() => ({
  copyToClipboard: vi.fn(),
  getAffiliateDetail: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  default: {
    getAffiliateDetail,
    transferAffiliateQuota: vi.fn(),
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser: vi.fn(),
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, unknown>) => `${key}${values ? ' ' + Object.values(values).join(' ') : ''}`,
    }),
  }
})

describe('AffiliateView', () => {
  const affiliateCode = 'affiliate-code-that-is-long-enough-to-overflow-a-mobile-viewport'

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    copyToClipboard.mockResolvedValue(true)
    getAffiliateDetail.mockResolvedValue({
      user_id: 1,
      aff_code: affiliateCode,
      inviter_id: null,
      aff_count: 0,
      aff_quota: 0,
      aff_frozen_quota: 0,
      aff_history_quota: 0,
      effective_rebate_rate_percent: 10,
      invitees: [],
    })
  })

  it.each([10, 0])('shows the configured extra gift alongside regular rebates for minimum %s', async (minimum) => {
    const current = await getAffiliateDetail();
    getAffiliateDetail.mockResolvedValueOnce({ ...current, first_recharge_bonus: 5, first_recharge_min_amount: minimum });
    const wrapper = mount(AffiliateView, {
      global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } },
    });
    await flushPromises();
    const gift = wrapper.find('[data-testid="first-recharge-bonus"]');
    expect(gift.exists()).toBe(true);
    expect(gift.text()).toContain('$5.00');
    expect(gift.text()).toContain('10%');
    expect(gift.text()).toContain(minimum > 0 ? 'affiliate.firstRecharge.withMinimum' : 'affiliate.firstRecharge.withoutMinimum');
    if (minimum > 0) expect(gift.text()).toContain('$10.00');
    expect(gift.text()).toContain('affiliate.firstRecharge.eligibility');
  });

  it('does not advertise a first-recharge gift when disabled or unavailable', async () => {
    const wrapper = mount(AffiliateView, {
      global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } },
    });
    await flushPromises();
    expect(wrapper.find('[data-testid="first-recharge-bonus"]').exists()).toBe(false);
    expect(wrapper.text()).toContain('10');
  });

  it('stacks long values and copy controls on mobile while retaining desktop rows', async () => {
    const wrapper = mount(AffiliateView, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          Icon: true,
        },
      },
    })

    await flushPromises()

    const values = wrapper.findAll('code')
    expect(values).toHaveLength(2)
    for (const value of values) {
      expect(value.classes()).toEqual(expect.arrayContaining([
        'min-w-0',
        'break-all',
        'sm:flex-1',
        'sm:truncate',
      ]))
      expect(Array.from(value.element.parentElement?.classList ?? [])).toEqual(expect.arrayContaining([
        'flex-col',
        'items-stretch',
        'sm:flex-row',
        'sm:items-center',
      ]))
    }

    const copyButtons = wrapper.findAll('button').filter((button) =>
      ['affiliate.copyCode', 'affiliate.copyLink'].includes(button.text()),
    )
    expect(copyButtons).toHaveLength(2)
    for (const button of copyButtons) {
      expect(button.classes()).toEqual(expect.arrayContaining([
        'w-full',
        'sm:w-auto',
        'sm:shrink-0',
      ]))
    }

    await copyButtons[0].trigger('click')
    await copyButtons[1].trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenNthCalledWith(1, affiliateCode, 'affiliate.codeCopied')
    expect(copyToClipboard).toHaveBeenNthCalledWith(
      2,
      `${window.location.origin}/register?aff=${encodeURIComponent(affiliateCode)}`,
      'affiliate.linkCopied',
    )
  })
})

describe('affiliate invitee records', () => {
  it('persists main-row preferences without losing username, zero rebate or invitee identity', async () => {
    localStorage.clear()
    const data = await getAffiliateDetail()
    getAffiliateDetail.mockResolvedValue({ ...data, invitees: [{ user_id: 17, email: 'invited@example.test', username: 'Original invited name', total_rebate: 0, created_at: '2026-10-24T00:00:00Z' }] })
    const create = () => mount(AffiliateView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } } })
    let view = create(); await flushPromises()
    let table = view.findComponent(DataTable)
    expect(table.props('rowKey')).toBe('user_id')
    expect(table.props('data')[0].user_id).toBe(17)
    expect(table.text()).toContain('$0.00')
    table.findComponent(ColumnSettings).vm.$emit('toggle', 'username')
    await flushPromises()
    view.unmount()
    view = create(); await flushPromises(); table = view.findComponent(DataTable)
    expect(table.findComponent(ColumnSettings).props('hiddenColumns')).toContain('username')
    await table.get('[data-test="toggle-details"]').trigger('click')
    expect(table.text()).toContain('Original invited name')
    expect(table.text()).toContain('invited@example.test')
    expect(table.text()).toContain('$0.00')
    view.unmount()
  })
})
