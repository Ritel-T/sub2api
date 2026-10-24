import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/types'
import GroupRPMOverridesModal from '../GroupRPMOverridesModal.vue'
import DocumentRows from '@/components/common/DocumentRows.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), getGroupRPMOverrides: vi.fn(), batchSetGroupRPMOverrides: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { users: { list: mocks.list }, groups: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())
beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  mocks.getGroupRPMOverrides.mockResolvedValue([])
  mocks.list.mockResolvedValue({ items: [{ id: 7, email: 'user@example.com', status: 'active' }] })
  mocks.batchSetGroupRPMOverrides.mockResolvedValue(undefined)
})

async function openEditor(attach = false) {
  const wrapper = mount(GroupRPMOverridesModal, {
    attachTo: attach ? document.body : undefined,
    props: { show: false, group: { id: 1, name: 'Group', platform: 'openai' } as AdminGroup },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      Icon: true, PlatformIcon: true, Pagination: true
    } }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

async function selectUser() {
  const wrapper = await openEditor()
  await wrapper.get('input[type="text"]').setValue('user')
  await vi.advanceTimersByTimeAsync(300)
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text().includes('user@example.com'))!.trigger('click')
  return wrapper
}

describe('GroupRPMOverridesModal new override validation', () => {
  it.each(['', '1.5', '-1'])('does not add invalid RPM %j', async (value) => {
    const wrapper = await selectUser()
    const input = wrapper.get('input[placeholder="100"]')
    await input.setValue('100')
    await input.setValue(value)
    const add = wrapper.findAll('button').find(b => b.text() === 'common.add')!
    expect(add.attributes('disabled')).toBeDefined()
    await add.trigger('click')
    expect(wrapper.find('tbody tr').exists()).toBe(false)
    expect(mocks.batchSetGroupRPMOverrides).not.toHaveBeenCalled()
  })

  it.each([0, 100])('saves integer RPM %i including unlimited zero', async (value) => {
    const wrapper = await selectUser()
    await wrapper.get('input[placeholder="100"]').setValue(String(value))
    await wrapper.findAll('button').find(b => b.text() === 'common.add')!.trigger('click')
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(mocks.batchSetGroupRPMOverrides).toHaveBeenCalledWith(1, [{ user_id: 7, rpm_override: value }])
  })
})

describe('existing RPM override edits', () => {
  beforeEach(() => {
    mocks.getGroupRPMOverrides.mockResolvedValue([
      { user_id: 7, user_email: 'user@example.com', user_name: '', user_status: 'active', rpm_override: 100 }
    ])
  })
  it.each(['', '1.5', '-1'])('does not save invalid RPM %j', async (value) => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue(value)
    expect(wrapper.findAll('button').some(button => button.text() === 'common.save')).toBe(false)
    expect(mocks.batchSetGroupRPMOverrides).not.toHaveBeenCalled()
  })

  it.each([['0', 0], ['200', 200], ['1e3', 1000]])('saves %s as %i', async (input, expected) => {
    const wrapper = await openEditor()
    await wrapper.get('tbody input[type="number"]').setValue(input)
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(mocks.batchSetGroupRPMOverrides).toHaveBeenCalledWith(1, [{ user_id: 7, rpm_override: expected }])
  })
})

it('keeps the focused override and sparse gap while saving all loaded entries', async () => {
  const frames = new Map<number, FrameRequestCallback>()
  let serial = 0
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++serial, callback); return serial })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  const entries = Array.from({ length: 150 }, (_, index) => ({
    user_id: index + 1, user_email: `user-${index + 1}@example.invalid`, user_name: '', user_status: 'active', rpm_override: 100
  }))
  mocks.getGroupRPMOverrides.mockResolvedValue(entries)
  const wrapper = await openEditor(true)
  try {
    const host = wrapper.findAll('div').find(div => div.classes().includes('max-h-[420px]'))!.element as HTMLElement
    host.style.overflowY = 'auto'
    const rect = (top: number, height: number) => ({ top, bottom: top + height, left: 0, right: 800, width: 800, height, x: 0, y: top, toJSON() {} })
    Object.defineProperties(host, { clientHeight: { configurable: true, value: 200 }, offsetHeight: { configurable: true, value: 200 }, clientWidth: { configurable: true, value: 800 }, offsetWidth: { configurable: true, value: 800 } })
    host.getBoundingClientRect = () => rect(0, 200)
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top })
    const body = wrapper.get('tbody').element as HTMLElement
    body.getBoundingClientRect = () => rect(40 - host.scrollTop, entries.length * 56)
    const settle = async () => {
      for (let pass = 0; pass < 30; pass++) {
        await flushPromises()
        if (!frames.size) return
        const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(callback => callback(0))
      }
      throw new Error('editor layout did not settle')
    }
    window.dispatchEvent(new Event('resize'))
    await settle()
    const firstInput = wrapper.get('tbody input[type="number"]')
    await firstInput.setValue('275')
    ;(firstInput.element as HTMLInputElement).focus()
    for (let page = 0; page < 2; page++) { await wrapper.get('.infinite-list-action').trigger('click'); await settle() }
    expect(wrapper.findComponent(DocumentRows).props('items')).toHaveLength(150)
    host.scrollTop = 6000; host.dispatchEvent(new Event('scroll')); await settle()
    expect(wrapper.findAll('tbody [data-document-row]').length).toBeLessThan(50)
    expect(wrapper.findAll('tbody [data-document-row]').some(row => Number(row.attributes('data-index')) > 80)).toBe(true)
    expect(document.activeElement).toBe(firstInput.element)
    expect((firstInput.element as HTMLInputElement).value).toBe('275')
    expect(wrapper.findAll('tbody [data-document-spacer] td').some(cell => cell.attributes('colspan') === '7' && parseFloat((cell.element as HTMLElement).style.height) > 4000)).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    const saved = mocks.batchSetGroupRPMOverrides.mock.calls[0][1]
    expect(saved).toHaveLength(150)
    expect(saved[0]).toEqual({ user_id: 1, rpm_override: 275 })
    expect(saved[149]).toEqual({ user_id: 150, rpm_override: 100 })
  } finally {
    wrapper.unmount()
    vi.unstubAllGlobals()
  }
})
