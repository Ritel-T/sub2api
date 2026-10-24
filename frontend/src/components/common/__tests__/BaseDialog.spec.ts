import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

describe('BaseDialog', () => {
  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })

  describe('click outside', () => {
    const mountOpen = (props: Record<string, unknown> = {}) =>
      mount(BaseDialog, {
        attachTo: document.body,
        props: { show: true, title: 'Details', ...props },
        slots: { default: '<p class="selectable">request-id-to-copy</p>' },
        global: { stubs: { Icon: true } }
      })

    // 按浏览器的派发顺序模拟一次鼠标操作：按下和松开的目标不同时，click 落在两者的共同祖先上
    const pointer = (down: Element, up: Element, click: Element) => {
      down.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      up.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }))
      click.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    }

    const overlay = () => document.body.querySelector('.modal-overlay')!
    const panelText = () => document.body.querySelector('.selectable')!

    it('closes when the overlay is pressed and released', async () => {
      const wrapper = mountOpen({ closeOnClickOutside: true, placement: 'right' })
      await nextTick()

      pointer(overlay(), overlay(), overlay())

      expect(wrapper.emitted('close')).toHaveLength(1)
      wrapper.unmount()
    })

    it('stays open when a press and release do not both land on the overlay', async () => {
      const wrapper = mountOpen({ closeOnClickOutside: true, placement: 'right' })
      await nextTick()

      // 在面板里拖选文字，松手落在遮罩上
      pointer(panelText(), overlay(), overlay())
      // 在遮罩上按下，拖进面板里松手
      pointer(overlay(), panelText(), overlay())
      expect(wrapper.emitted('close')).toBeUndefined()

      pointer(overlay(), overlay(), overlay())
      expect(wrapper.emitted('close')).toHaveLength(1)
      wrapper.unmount()
    })

    it('ignores overlay clicks unless closeOnClickOutside is enabled', async () => {
      const wrapper = mountOpen()
      await nextTick()

      pointer(overlay(), overlay(), overlay())

      expect(wrapper.emitted('close')).toBeUndefined()
      wrapper.unmount()
    })
  })
})

const pressEscape = () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))

describe('stacked dialog Escape handling', () => {
  it('closes only the most recently opened dialog, then allows the parent to close', async () => {
    const parent = mount(BaseDialog, { props: { show: true, title: 'Parent' } })
    const child = mount(BaseDialog, { props: { show: true, title: 'Child' } })
    pressEscape()
    expect(child.emitted('close')).toHaveLength(1)
    expect(parent.emitted('close')).toBeUndefined()
    await child.setProps({ show: false })
    pressEscape()
    expect(parent.emitted('close')).toHaveLength(1)
  })

  it('does not dismiss a parent behind a child that disallows Escape', () => {
    const parent = mount(BaseDialog, { props: { show: true, title: 'Parent' } })
    const child = mount(BaseDialog, { props: { show: true, title: 'Child', closeOnEscape: false } })
    pressEscape()
    expect(child.emitted('close')).toBeUndefined()
    expect(parent.emitted('close')).toBeUndefined()
    child.unmount()
    pressEscape()
    expect(parent.emitted('close')).toHaveLength(1)
  })
})

function openDialog(title: string) {
  return mount(BaseDialog, { props: { show: true, title }, global: { stubs: { Icon: true } } })
}

describe('dialog accessible titles', () => {
  it('associates simultaneous dialogs with their own title', async () => {
    openDialog('Edit account')
    openDialog('Confirm deletion')
    await nextTick()
    const dialogs = Array.from(document.body.querySelectorAll('[role="dialog"]'))
    expect(dialogs).toHaveLength(2)
    const titles = dialogs.map(dialog => document.getElementById(dialog.getAttribute('aria-labelledby')!)?.textContent)
    expect(titles).toEqual(['Edit account', 'Confirm deletion'])
  })

  it('keeps the title association stable when a dialog is reopened', async () => {
    const wrapper = openDialog('Details')
    await nextTick()
    const id = document.body.querySelector('[role="dialog"]')!.getAttribute('aria-labelledby')!
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, title: 'Updated details' })
    expect(document.body.querySelector('[role="dialog"]')!.getAttribute('aria-labelledby')).toBe(id)
    expect(document.getElementById(id)?.textContent).toBe('Updated details')
  })
})

function press(key: string, shiftKey = false) {
  const event = new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })
  document.activeElement?.dispatchEvent(event)
  return event
}
describe('shared dialog keyboard ownership', () => {
  it('skips hidden and disabled controls, honors autofocus, and wraps the focus boundary', async () => {
    mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'OTP', showCloseButton: false }, slots: { default: '<input aria-hidden="true" tabindex="-1"><input disabled><input autofocus id="otp"><button id="cancel">Cancel</button>' } })
    await nextTick()
    const input = document.querySelector<HTMLInputElement>('#otp')!
    const cancel = document.querySelector<HTMLButtonElement>('#cancel')!
    expect(document.activeElement).toBe(input)
    press('Tab', true)
    expect(document.activeElement).toBe(cancel)
    press('Tab')
    expect(document.activeElement).toBe(input)
  })
  it('keeps focus on an empty busy dialog and restores the opener on unmount', async () => {
    const opener = document.createElement('button'); document.body.append(opener); opener.focus()
    const wrapper = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Busy', showCloseButton: false }, slots: { default: '<button disabled>Saving</button>' } })
    await nextTick()
    expect(document.activeElement?.classList.contains('modal-content')).toBe(true)
    expect(press('Tab').defaultPrevented).toBe(true)
    wrapper.unmount()
    expect(document.activeElement).toBe(opener)
  })
  it('lets only the top dialog wrap focus and respects a handled Escape event', async () => {
    const parent = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Parent', showCloseButton: false }, slots: { default: '<button id="parent">Parent</button>' } })
    await nextTick()
    const child = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Child', showCloseButton: false }, slots: { default: '<button id="child">Child</button>' } })
    await nextTick()
    press('Tab')
    expect(document.activeElement?.id).toBe('child')
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }); escape.preventDefault(); document.dispatchEvent(escape)
    expect(child.emitted('close')).toBeUndefined()
    expect(parent.emitted('close')).toBeUndefined()
    child.unmount()
    expect(document.activeElement?.id).toBe('parent')
  })
})

function dialog(show = true) {
  return mount(BaseDialog, { props: { show, title: 'Details' }, global: { stubs: { Icon: true } } })
}

const locked = () => document.body.classList.contains('modal-open')

describe('dialog body scroll lock', () => {
  it('keeps the body locked when a hidden sibling mounts or unmounts', () => {
    dialog()
    const hidden = dialog(false)
    expect(locked()).toBe(true)
    hidden.unmount()
    expect(locked()).toBe(true)
  })

  it('keeps the lock until the last visible dialog closes', async () => {
    const first = dialog()
    const second = dialog()
    await second.setProps({ show: false })
    expect(locked()).toBe(true)
    await first.setProps({ show: false })
    expect(locked()).toBe(false)
  })

  it('keeps the lock until the last visible dialog unmounts', () => {
    const first = dialog()
    const second = dialog()
    second.unmount()
    expect(locked()).toBe(true)
    first.unmount()
    expect(locked()).toBe(false)
  })

  it('releases the lock after repeated open and close cycles', async () => {
    const wrapper = dialog(false)
    expect(locked()).toBe(false)
    await wrapper.setProps({ show: true })
    expect(locked()).toBe(true)
    await wrapper.setProps({ show: false })
    expect(locked()).toBe(false)
    await wrapper.setProps({ show: true })
    wrapper.unmount()
    expect(locked()).toBe(false)
  })
})
