import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ColumnSettings from '../ColumnSettings.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers.length = 0; document.body.innerHTML = '' })
function create() {
  const wrapper = mount(ColumnSettings, { attachTo: document.body, props: {
    columns: [{ key: 'select', label: '' }, { key: 'name', label: 'Name' }, { key: 'id', label: 'ID' }, { key: 'actions', label: 'Actions' }],
    hiddenColumns: new Set(['id']), requiredColumns: ['name', 'actions']
  } })
  wrappers.push(wrapper)
  return wrapper
}
describe('ColumnSettings', () => {
  it('opens a viewport-positioned dialog with column controls, including required movable columns', async () => {
    const wrapper = create()
    await wrapper.find('button').trigger('click')
    const dialog = document.querySelector('[role="dialog"]')!
    expect(dialog).not.toBeNull()
    const inputs = Array.from(dialog.querySelectorAll('input'))
    expect(inputs).toHaveLength(3)
    expect(inputs.map(input => input.checked)).toEqual([true, false, true])
    expect(inputs.map(input => input.disabled)).toEqual([true, false, true])
    expect(dialog.querySelectorAll('[data-column-key]')).toHaveLength(3)
    expect(document.activeElement).toBe(dialog)
    inputs[1]!.dispatchEvent(new Event('change', { bubbles: true }))
    expect(wrapper.emitted('toggle')?.[0]).toEqual(['id'])
  })
  it('supports keyboard reordering, reset and Escape with focus return', async () => {
    const wrapper = create()
    await wrapper.find('button').trigger('click')
    const dialog = document.querySelector('[role="dialog"]')!
    const handle = dialog.querySelector('[data-column-key="actions"]')!
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }))
    expect(wrapper.emitted('reorder')?.[0]).toEqual([['actions', 'name', 'id']])
    ;(dialog.querySelector('button') as HTMLButtonElement).click()
    expect(wrapper.emitted('reset')).toHaveLength(1)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(wrapper.find('button').element)
  })
})
