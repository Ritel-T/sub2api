import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: ReturnType<typeof mount>
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

async function press(key: string) {
  document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  await nextTick()
}

async function open(searchable: boolean | 'auto' = 'auto') {
  wrapper = mount(Select, {
    attachTo: document.body,
    props: { modelValue: 'alpha', searchable, options: [
      { value: 'alpha', label: 'Alpha' },
      { value: 'disabled', label: 'Disabled', disabled: true },
      { value: 'beta', label: 'Beta' },
    ] },
  })
  wrapper.get<HTMLButtonElement>('button').element.focus()
  await press('ArrowDown')
  await nextTick()
}

describe('Select keyboard focus', () => {
  it.each([false, 'auto'] as const)('selects an enabled option without a search input (%s)', async (searchable) => {
    await open(searchable)
    expect(document.activeElement).toBe(document.querySelector('[role="listbox"]'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

  it('allows Escape to close a non-searchable dropdown and restore trigger focus', async () => {
    await open(false)
    await press('Escape')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps focus in the search input when search is enabled', async () => {
    await open(true)
    expect(document.activeElement).toBe(document.querySelector('.select-search-input'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
  })
})

describe('Select IME keyboard input', () => {
  it.each(['Enter', 'Escape', 'ArrowDown', 'ArrowUp'])('leaves composing %s to the input method', async (key) => {
    wrapper = mount(Select, {
      attachTo: document.body,
      props: { modelValue: null, searchable: true, options: [
        { value: 'first', label: 'First' }, { value: 'second', label: 'Second' }
      ] }
    })
    await wrapper.get('button').trigger('click')
    await nextTick()
    const input = document.querySelector<HTMLInputElement>('.select-search-input')!
    expect(document.activeElement).toBe(input)
    const composing = new KeyboardEvent('keydown', { key, isComposing: true, bubbles: true, cancelable: true })
    input.dispatchEvent(composing)
    await nextTick()
    expect(composing.defaultPrevented).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([['first']])
  })
})
