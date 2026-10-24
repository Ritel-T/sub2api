import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import Select from '@/components/common/Select.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'

import UserTokenRanking from '../UserTokenRanking.vue'

enableAutoUnmount(afterEach)
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

const getUserBreakdown = vi.fn()

vi.mock('@/api/admin/dashboard', () => ({
  getUserBreakdown: (...args: unknown[]) => getUserBreakdown(...args),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const item = (id: number, tokens: number) => ({
  user_id: id,
  email: `u${id}@test.com`,
  requests: 1,
  input_tokens: tokens,
  output_tokens: 0,
  cache_tokens: 0,
  total_tokens: tokens,
  actual_cost: 0.5,
})

const mountRanking = (props: Record<string, unknown> = {}, attachTo?: HTMLElement) =>
  mount(UserTokenRanking, {
    attachTo,
    props: {
      startDate: '2026-07-01',
      endDate: '2026-07-08',
      filters: {},
      ...props,
    },
    global: { stubs: { Select: true, LoadingSpinner: true } },
  })

describe('UserTokenRanking', () => {
  beforeEach(() => {
    getUserBreakdown.mockReset()
    getUserBreakdown.mockResolvedValue({ users: [item(1, 100), item(2, 50)] })
  })

  it('loads with shared filters, expands row details and selects a user through its name', async () => {
    const wrapper = mountRanking({ filters: { group_id: 3 }, model: 'claude-fable-5' })
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 3,
      model: 'claude-fable-5',
      start_date: '2026-07-01',
      end_date: '2026-07-08',
      sort_by: 'total_tokens',
      limit: 50,
    }))

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)

    await rows[0].trigger('click')
    expect(wrapper.emitted('select-user')).toBeUndefined()
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    expect(wrapper.text()).toContain('admin.usage.tokenRanking.columns.inputTokens')
    await rows[0].get('td:nth-child(2) button').trigger('click')
    expect(wrapper.emitted('select-user')![0]).toEqual([1, 'u1@test.com'])
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
  })

  it('reloads when shared filters change', async () => {
    const wrapper = mountRanking()
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ filters: { user_id: 9 } })
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledTimes(2)
    expect(getUserBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: 9 }))
  })

  it('windows all Top 200 users while preserving server ranks, offscreen selection, details and focus', async () => {
    const frames = new Map<number, FrameRequestCallback>()
    let nextFrame = 0
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    vi.stubGlobal('scrollTo', vi.fn())
    const host = document.createElement('div')
    host.dataset.viewport = ''
    host.style.overflowY = 'auto'
    host.style.height = '240px'
    Object.defineProperties(host, {
      offsetHeight: { get: () => 240 }, clientHeight: { get: () => 240 },
      offsetWidth: { get: () => 800 }, clientWidth: { get: () => 800 }
    })
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
    document.body.append(host)
    const rectangle = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 800, width: 800, x: 0, y: top, toJSON() {} })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      if (this === host) return rectangle(0, 240)
      const index = this.hasAttribute('data-document-row') ? Number(this.dataset.index) : 0
      return rectangle(index * 72 - host.scrollTop, this.hasAttribute('data-document-row') ? 72 : 14544)
    })
    const settle = async () => {
      for (let pass = 0; pass < 30; pass++) {
        await flushPromises()
        if (!frames.size) return
        const queued = [...frames.values()]; frames.clear(); queued.forEach(callback => callback(0))
      }
      throw new Error('ranking layout did not settle')
    }
    const wrapper = mountRanking({}, host)
    try {
      await settle()
      // Deliberately descending IDs and ascending values prove server order is retained.
      const users = Array.from({ length: 200 }, (_, index) => ({
        ...item(200 - index, index + 1), input_tokens: 123, output_tokens: 456, cache_tokens: 789
      }))
      getUserBreakdown.mockResolvedValueOnce({ users })
      const resultLimit = wrapper.findAllComponents(Select)[1]
      resultLimit.vm.$emit('update:modelValue', 200)
      await flushPromises()
      resultLimit.vm.$emit('change', 200)
      await settle()
      expect(getUserBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({ limit: 200, sort_by: 'total_tokens' }))
      expect(wrapper.getComponent(DocumentRows).props('sourceCount')).toBe(200)
      expect(wrapper.getComponent(DocumentRows).props('items')).toHaveLength(200)
      expect(wrapper.findAll('.ranking-main-row').length).toBeGreaterThan(0)
      expect(wrapper.findAll('.ranking-main-row').length).toBeLessThan(40)
      expect(wrapper.find('[data-user-id="50"]').exists()).toBe(false)
      const first = wrapper.get('[data-user-id="200"]')
      expect(first.get('td').text()).toBe('1')
      const firstName = first.get('td:nth-child(2) button').element as HTMLButtonElement
      firstName.focus()
      await first.trigger('click')
      await settle()
      expect(wrapper.getComponent(DocumentRows).props('items')).toHaveLength(201)
      host.scrollTop = 151 * 72
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(document.activeElement).toBe(firstName)
      const later = wrapper.get('[data-user-id="50"]')
      expect(later.get('td').text()).toBe('151')
      expect(later.findAll('td')).toHaveLength(6)
      expect(later.get('td:nth-child(3)').text()).toBe('1')
      expect(later.get('td:nth-child(4)').text()).toBe('151')
      expect(later.get('td:nth-child(5)').text()).toBe('$0.5000')
      await later.get('td:nth-child(2) button').trigger('click')
      expect(wrapper.emitted('select-user')![0]).toEqual([50, 'u50@test.com'])
      const expand = later.get('td:last-child button')
      ;(expand.element as HTMLButtonElement).focus()
      await expand.trigger('click')
      await settle()
      expect(expand.attributes('aria-expanded')).toBe('true')
      const detail = later.element.nextElementSibling!
      expect(detail.classList.contains('ranking-detail-row')).toBe(true)
      expect([...detail.querySelectorAll('dd')].map(element => element.textContent)).toEqual(['123', '456', '789'])
      expect([...detail.querySelectorAll('dt')].map(element => element.textContent)).toEqual([
        'admin.usage.tokenRanking.columns.inputTokens',
        'admin.usage.tokenRanking.columns.outputTokens',
        'admin.usage.tokenRanking.columns.cacheTokens'
      ])
      host.scrollTop = 199 * 72
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(document.activeElement).toBe(expand.element)
      expect(wrapper.findAll('.ranking-main-row').length).toBeLessThan(40)
      expect(wrapper.get('tbody').findAll(':scope > div')).toHaveLength(0)
      expect(wrapper.findAll('[data-document-spacer]').every(gap => gap.get('td').attributes('colspan') === '6')).toBe(true)
    } finally {
      wrapper.unmount()
      host.remove()
    }
  })

})
