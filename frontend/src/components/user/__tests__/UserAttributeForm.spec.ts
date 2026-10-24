import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserAttributeForm from '../UserAttributeForm.vue'

const mocks = vi.hoisted(() => ({ getUserAttributeValues: vi.fn(), listEnabledDefinitions: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { userAttributes: mocks } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key === 'common.loading' ? 'Loading' : key }) }))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  mocks.listEnabledDefinitions.mockResolvedValue([
    { id: 1, name: 'Count', type: 'number', validation: { min: -10, max: 100 } },
    { id: 2, name: 'Note', type: 'text' }
  ])
})

describe('UserAttributeForm numeric values', () => {
  it('announces pending attributes and removes the status once content is ready', async () => {
    let resolve!: (value: unknown[]) => void
    mocks.listEnabledDefinitions.mockReturnValueOnce(new Promise(res => { resolve = res }))
    const wrapper = mount(UserAttributeForm, { props: { modelValue: {} }, global: { stubs: { Select: true } } })
    await flushPromises()
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('Loading')
    resolve([{ id: 1, name: 'Note', type: 'text' }])
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('input[type="text"]').exists()).toBe(true)
  })
  it.each(['42', '0', '-3'])('emits %s as a string accepted by the values API', async (value) => {
    const wrapper = mount(UserAttributeForm, { props: { modelValue: {} }, global: { stubs: { Select: true } } })
    await flushPromises()
    await wrapper.get('input[type="text"]').setValue('keep me')
    await wrapper.get('input[type="number"]').setValue(value)
    const emitted = wrapper.emitted('update:modelValue')!.at(-1)![0]
    expect(JSON.parse(JSON.stringify(emitted))).toEqual({ 1: value, 2: 'keep me' })
  })

  it('emits an empty string when the number is cleared', async () => {
    const wrapper = mount(UserAttributeForm, { props: { modelValue: {} }, global: { stubs: { Select: true } } })
    await flushPromises()
    const input = wrapper.get('input[type="number"]')
    await input.setValue('42')
    await input.setValue('')
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ 1: '' }])
    expect(input.attributes()).toMatchObject({ min: '-10', max: '100', type: 'number' })
  })
})

function deferred() {
  let resolve!: (value: unknown) => void
  const promise = new Promise(res => { resolve = res })
  return { promise, resolve }
}
function open() {
  return mount(UserAttributeForm, { props: { userId: 1, modelValue: {} }, global: { stubs: { Select: true } } })
}
describe('user attribute request ownership', () => {
  beforeEach(() => {
    mocks.listEnabledDefinitions.mockResolvedValue([{ id: 1, name: 'Team', type: 'text' }])
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => vi.restoreAllMocks())
  it('does not emit previous user values after a newer user has loaded', async () => {
    const old = deferred()
    mocks.getUserAttributeValues.mockReturnValueOnce(old.promise).mockResolvedValueOnce([{ attribute_id: 1, value: 'current' }])
    const w = open(); await w.setProps({ userId: 2 }); await flushPromises()
    old.resolve([{ attribute_id: 1, value: 'old' }]); await flushPromises()
    expect(w.get('input').element.value).toBe('current')
    expect(w.emitted('update:modelValue')).toEqual([[{ 1: 'current' }]])
  })
  it('does not restore values after resetting to a new user', async () => {
    const old = deferred(); mocks.getUserAttributeValues.mockReturnValueOnce(old.promise)
    const w = open(); await flushPromises(); await w.setProps({ userId: undefined })
    old.resolve([{ attribute_id: 1, value: 'old' }]); await flushPromises()
    expect(w.get('input').element.value).toBe('')
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })
  it('clears the previous values before a new user request fails', async () => {
    mocks.getUserAttributeValues.mockResolvedValueOnce([{ attribute_id: 1, value: 'old' }]).mockRejectedValueOnce(new Error('unavailable'))
    const w = open(); await flushPromises(); await w.setProps({ userId: 2 }); await flushPromises()
    expect(w.get('input').element.value).toBe('')
  })
})
