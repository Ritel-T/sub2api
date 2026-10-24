import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async () => {
  const wrapper = mount(Select, {
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await nextTick()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('preserves the existing 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('shrinks the minimum width to fit near the right viewport edge', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('220px')
    expect(dropdown?.style.minWidth).toBe('92px')
    expect(dropdown?.style.maxWidth).toBe('92px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })

  it('clamps an offscreen-right trigger position to the viewport boundary', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('312px')
    expect(dropdown?.style.minWidth).toBe('0px')
    expect(dropdown?.style.maxWidth).toBe('0px')
  })
})

describe('Select remote search', () => {
  const mountRemoteSelect = (props: Record<string, unknown> = {}) => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        remote: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
        ...props,
      },
    })
    unmountWrapper = () => wrapper.unmount()
    return wrapper
  }

  const openDropdown = async () => {
    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    return dropdown as HTMLElement
  }

  const typeSearchQuery = async (query: string) => {
    const dropdown = await openDropdown()
    const input = dropdown.querySelector<HTMLInputElement>('.select-search-input')
    expect(input).not.toBeNull()
    input!.value = query
    input!.dispatchEvent(new Event('input'))
    await nextTick()
  }

  it('emits debounced search events and skips local filtering in remote mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('zzz')

    // 防抖窗口内不触发。
    expect(wrapper.emitted('search')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toEqual([['zzz']])
    // 远程模式不做本地过滤：无命中的 query 下选项仍完整展示（由父组件更新 options）。
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toContain('Alpha account')
    expect(labels).toContain('Beta account')
  })

  it('does not emit search when the dropdown closes and the query resets', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('hidden')

    // 关闭下拉：排队中的防抖定时器应被取消，也不应因 query 重置而尾随 emit。
    await wrapper.get('button').trigger('click')
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('shows the loading text instead of empty text while loading with no options', async () => {
    const wrapper = mountRemoteSelect({ options: [], loading: true })
    await wrapper.get('button').trigger('click')
    await nextTick()

    const dropdown = await openDropdown()
    expect(dropdown.querySelector('.select-empty')?.textContent).toContain('common.loading')
  })

  it('keeps local filtering and emits nothing when remote is not set', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        searchable: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('alpha')
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toEqual(['Alpha account'])
  })
})

describe('Select multiple mode', () => {
  it('toggles options without closing the dropdown', async () => {
    const wrapper = mount(Select, {
      props: {
        modelValue: [1],
        multiple: true,
        options: [
          { value: 1, label: 'Group one' },
          { value: 2, label: 'Group two' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()

    await wrapper.get('button').trigger('click')
    await nextTick()
    const options = document.querySelectorAll<HTMLElement>('.select-option')
    await options[1].click()
    await nextTick()

    expect(wrapper.emitted('update:modelValue')).toEqual([[[1, 2]]])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
    await wrapper.setProps({ modelValue: [1, 2] })
    expect(wrapper.get('button').text()).toContain('common.selectedCount')
  })

  it('clears all values in multiple mode', async () => {
    const wrapper = mount(Select, {
      props: {
        modelValue: [1, 2],
        multiple: true,
        clearable: true,
        options: [
          { value: 1, label: 'Group one' },
          { value: 2, label: 'Group two' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()

    await wrapper.get('.select-clear').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
  })
})

describe('Select field semantics', () => {
  it('keeps native label associations and forwards field attributes to the trigger', async () => {
    const wrapper = mount({
      components: { Select },
      template: '<div><label for="category">Category</label><p id="category-hint">Required for routing</p><Select id="category" model-value="one" :options="[{value: &quot;one&quot;, label: &quot;One&quot;}]" class="layout-width" data-field-role="filter" data-field-density="compact" aria-describedby="category-hint" aria-required="true" :error="true" /></div>',
    })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get('.select-trigger')
    expect((trigger.element as HTMLButtonElement).labels?.[0].textContent).toBe('Category')
    expect(trigger.attributes('aria-label')).toBeUndefined()
    expect(trigger.attributes('aria-describedby')).toBe('category-hint')
    expect(trigger.attributes('aria-invalid')).toBe('true')
    expect(trigger.attributes('aria-required')).toBe('true')
    expect(wrapper.get('.layout-width').element.tagName).toBe('DIV')
    expect(wrapper.get('.layout-width').attributes('aria-describedby')).toBeUndefined()
    expect(wrapper.get('.layout-width').attributes('data-field-role')).toBe('filter')
    expect(wrapper.get('.layout-width').attributes('data-field-density')).toBe('compact')
    expect(trigger.attributes('data-field-role')).toBeUndefined()
  })

  it('preserves labelledby and description props while updating error and disabled states', async () => {
    const wrapper = mount(Select, {
      props: { modelValue: null, options: [], ariaDescribedby: 'hint', error: true },
      attrs: { 'aria-labelledby': 'business-label', 'aria-invalid': 'grammar' },
    })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get('.select-trigger')
    expect(trigger.attributes('aria-labelledby')).toBe('business-label')
    expect(trigger.attributes('aria-describedby')).toBe('hint')
    expect(trigger.attributes('aria-invalid')).toBe('true')
    await wrapper.setProps({ error: false, disabled: true })
    expect(trigger.attributes('aria-invalid')).toBe('grammar')
    expect(trigger.attributes('disabled')).toBeDefined()
    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('false')
  })

  it('renders a localized, separately focusable clear button and labels the popup', async () => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: { modelValue: 'one', options: [{ value: 'one', label: 'One' }], clearable: true, ariaLabel: 'Category' },
    })
    unmountWrapper = () => wrapper.unmount()
    const trigger = wrapper.get('.select-trigger')
    const clear = wrapper.get('.select-clear')
    expect(clear.element.tagName).toBe('BUTTON')
    expect(clear.attributes('tabindex')).toBeUndefined()
    expect(clear.attributes('aria-label')).toBe('common.clear')
    expect(trigger.find('button, [role="button"]').exists()).toBe(false)
    expect(clear.attributes('aria-describedby')).toBe(trigger.attributes('id'))
    await trigger.trigger('click')
    await nextTick()
    const popup = document.querySelector('[role="listbox"]')!
    expect(popup.getAttribute('aria-labelledby')).toBe(trigger.attributes('id'))
    expect(trigger.attributes('aria-controls')).toBe(popup.id)
    expect(trigger.attributes('aria-haspopup')).toBe('listbox')
    ;(clear.element as HTMLButtonElement).focus()
    await clear.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[null]])
    await wrapper.setProps({ modelValue: null })
    expect(wrapper.find('.select-clear').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    await wrapper.setProps({ disabled: true })
    expect(wrapper.find('.select-clear').exists()).toBe(false)
  })
})
