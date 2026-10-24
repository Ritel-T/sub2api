import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/types'
import GroupRateMultipliersModal from '../GroupRateMultipliersModal.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'

const mocks = vi.hoisted(() => ({ getGroupRateMultipliers: vi.fn(), batchSetGroupRateMultipliers: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())
beforeEach(() => {
  vi.resetAllMocks()
  mocks.getGroupRateMultipliers.mockResolvedValue([
    { user_id: 7, user_email: 'user@example.com', user_name: '', user_status: 'active', rate_multiplier: 2, rpm_override: null }
  ])
})

async function openEditor() {
  const wrapper = mount(GroupRateMultipliersModal, {
    props: { show: false, group: { id: 1, name: 'Group', platform: 'openai', rate_multiplier: 1 } as AdminGroup },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      Icon: true, PlatformIcon: true, Pagination: true
    } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('group rate multiplier edits', () => {
  it('reveals further editor rows without discarding an unsaved override', async () => {
    mocks.getGroupRateMultipliers.mockResolvedValue(Array.from({ length: 65 }, (_, index) => ({
      user_id: index + 1, user_email: `user-${index + 1}@example.invalid`,
      user_name: '', user_status: 'active', rate_multiplier: 2, rpm_override: null,
    })))
    const wrapper = await openEditor()
    expect(wrapper.findAll('tbody tr')).toHaveLength(50)
    await wrapper.get('tbody input[type="number"]').setValue('3')
    await wrapper.get('.infinite-list-action').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(65)
    expect((wrapper.get('tbody input[type="number"]').element as HTMLInputElement).value).toBe('3')
    expect(mocks.getGroupRateMultipliers).toHaveBeenCalledTimes(1)
    expect(mocks.batchSetGroupRateMultipliers).not.toHaveBeenCalled()
  })
  it.each(['0', '-1'])('does not stage invalid custom rate %s', async (value) => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue(value)
    expect(wrapper.findAll('button').some(button => button.text() === 'common.save')).toBe(false)
    expect(mocks.batchSetGroupRateMultipliers).not.toHaveBeenCalled()
  })

  it.each([['0.5', 0.5], ['1e-3', 0.001]])('saves positive rate %s', async (value, expected) => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue(value)
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    expect(mocks.batchSetGroupRateMultipliers).toHaveBeenCalledWith(1, [{ user_id: 7, rate_multiplier: expected }])
  })

  it('still removes an override when its input is cleared', async () => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue('')
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    expect(mocks.batchSetGroupRateMultipliers).toHaveBeenCalledWith(1, [])
  })
})

it('windows loaded override rows while preserving the entire staged save payload', async () => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  mocks.getGroupRateMultipliers.mockResolvedValue(Array.from({ length: 150 }, (_, index) => ({
    user_id: index + 1, user_email: `user-${index + 1}@example.invalid`, user_name: '', user_status: 'active', rate_multiplier: 2, rpm_override: null
  })))
  const wrapper = await openEditor()
  await wrapper.get('tbody input[type="number"]').setValue('3')
  for (let page = 0; page < 2; page++) { await wrapper.get('.infinite-list-action').trigger('click'); await flushPromises() }
  expect(wrapper.findComponent(DocumentRows).props('items')).toHaveLength(150)
  expect(wrapper.findAll('tbody [data-document-row]').length).toBeGreaterThan(0)
  expect(wrapper.findAll('tbody [data-document-row]').length).toBeLessThan(100)
  expect(wrapper.get('tbody input[type="number"]').element).toHaveProperty('value', '3')
  await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
  await flushPromises()
  const saved = mocks.batchSetGroupRateMultipliers.mock.calls[0][1]
  expect(saved).toHaveLength(150)
  expect(saved[0]).toEqual({ user_id: 1, rate_multiplier: 3 })
  expect(saved[149]).toEqual({ user_id: 150, rate_multiplier: 2 })
})
