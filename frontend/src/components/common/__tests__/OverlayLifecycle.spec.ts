import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref, nextTick } from 'vue'
import { describe, it, expect, afterEach } from 'vitest'
import { useOwnedPopover } from '@/composables/overlayLifecycle'
import { createI18n } from 'vue-i18n'
import BaseDialog from '../BaseDialog.vue'
import AppSelect from '../Select.vue'
import DateRangePicker from '../DateRangePicker.vue'
import ColumnSettings from '../ColumnSettings.vue'
import HelpTooltip from '../HelpTooltip.vue'

const wrappers: ReturnType<typeof mount>[] = []
const i18n = () => createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })
function render(component: ReturnType<typeof defineComponent>) {
  const wrapper = mount(component, { attachTo: document.body, global: { plugins: [i18n()], stubs: { transition: true } } })
  wrappers.push(wrapper)
  return wrapper
}
function key(value: string, shiftKey = false) {
  document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: value, shiftKey, bubbles: true, cancelable: true }))
}
const button = (id: string) => document.querySelector<HTMLButtonElement>(`#${id}`)!
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })

describe('owned overlay lifecycle', () => {
  it('Tabs from searchable Select to the next field, and ShiftTabs to its trigger', async () => {
    render(defineComponent({ components: { BaseDialog, AppSelect }, template: `<BaseDialog :show="true" title="Form"><button id="before">Before</button><AppSelect id="choose" :model-value="null" :searchable="true" :options="[{value:1,label:'One'}]"/><button id="after">After</button></BaseDialog>` }))
    await flushPromises()
    button('choose').click(); await flushPromises()
    expect(document.activeElement?.classList.contains('select-search-input')).toBe(true)
    key('Tab'); await nextTick()
    expect(document.activeElement).toBe(button('after'))
    expect(document.querySelector('[role="listbox"]')).toBeNull()
    button('choose').click(); await flushPromises()
    key('Tab', true); await nextTick()
    expect(document.activeElement).toBe(button('choose'))
  })
  it('Escape closes only the Select, then closes its parent dialog', async () => {
    const close = ref(0)
    render(defineComponent({ components: { BaseDialog, AppSelect }, setup: () => ({ close }), template: `<BaseDialog :show="true" title="Form" @close="close++"><AppSelect id="choose" :model-value="null" :searchable="true" :options="[]"/></BaseDialog>` }))
    await flushPromises(); button('choose').click(); await flushPromises()
    key('Escape'); await nextTick()
    expect(close.value).toBe(0); expect(document.activeElement).toBe(button('choose'))
    key('Escape'); expect(close.value).toBe(1)
  })
  it('closing a lower dialog preserves top focus and the scroll lock', async () => {
    const lower = ref(true), upper = ref(false)
    render(defineComponent({ components: { BaseDialog }, setup: () => ({ lower, upper }), template: `<button id="opener">Open</button><BaseDialog :show="lower" title="Lower"><button id="lower">Lower</button></BaseDialog><BaseDialog :show="upper" title="Upper"><button id="upper">Upper</button></BaseDialog>` }))
    await flushPromises(); button('lower').focus(); upper.value = true; await flushPromises()
    button('upper').focus(); lower.value = false; await flushPromises()
    expect(document.activeElement).toBe(button('upper'))
    expect(document.body.classList.contains('modal-open')).toBe(true)
    upper.value = false; await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
  it('unmounting a lower dialog preserves top focus; closing top falls back when opener disconnected', async () => {
    const lower = ref(true), upper = ref(false)
    render(defineComponent({ components: { BaseDialog }, setup: () => ({ lower, upper }), template: `<BaseDialog v-if="lower" :show="true" title="Lower"><button id="lower">Lower</button></BaseDialog><BaseDialog :show="upper" title="Upper"><button id="upper">Upper</button></BaseDialog>` }))
    await flushPromises(); button('lower').focus(); upper.value = true; await flushPromises(); button('upper').focus()
    lower.value = false; await flushPromises()
    expect(document.activeElement).toBe(button('upper'))
    upper.value = false; await flushPromises()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
  it('owner hide dismisses its still-mounted portal and reopening starts closed', async () => {
    const show = ref(true)
    render(defineComponent({ components: { BaseDialog, AppSelect }, setup: () => ({ show }), template: `<BaseDialog :show="show" title="Form"><AppSelect id="choose" :model-value="null" :searchable="true" :options="[]"/></BaseDialog>` }))
    await flushPromises(); button('choose').click(); await flushPromises()
    show.value = false; await flushPromises()
    expect(document.querySelector('[role="listbox"]')).toBeNull()
    show.value = true; await flushPromises()
    expect(button('choose').getAttribute('aria-expanded')).toBe('false')
  })
  it('calendar Escape is consumed before parent modal Escape', async () => {
    const close = ref(0)
    render(defineComponent({ components: { BaseDialog, DateRangePicker }, setup: () => ({ close }), template: `<BaseDialog :show="true" title="Form" @close="close++"><DateRangePicker start-date="2026-10-01" end-date="2026-10-02"/></BaseDialog>` }))
    await flushPromises(); document.querySelector<HTMLButtonElement>('.date-picker-trigger')!.click(); await flushPromises()
    key('Escape'); await nextTick()
    expect(document.querySelector('.date-picker-dropdown')).toBeNull(); expect(close.value).toBe(0)
    key('Escape'); expect(close.value).toBe(1)
  })
  it('ColumnSettings portal participates in modal tab order and Escape returns to trigger', async () => {
    render(defineComponent({ components: { BaseDialog, ColumnSettings }, setup: () => ({ hidden: new Set() }), template: `<BaseDialog :show="true" title="Form"><ColumnSettings :columns="[{key:'name',label:'Name'}]" :hidden-columns="hidden"/><button id="after">After</button></BaseDialog>` }))
    await flushPromises()
    const trigger = document.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]')!
    trigger.click(); await flushPromises()
    key('Tab'); expect(document.activeElement?.textContent).toContain('resetColumns')
    key('Escape'); await nextTick(); expect(document.activeElement).toBe(trigger)
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
  })
  it('HelpTooltip owns Escape before its containing dialog', async () => {
    const close = ref(0)
    render(defineComponent({ components: { BaseDialog, HelpTooltip }, setup: () => ({ close }), template: `<BaseDialog :show="true" title="Form" @close="close++"><HelpTooltip trigger="click" content="Help"><template #trigger><button id="help">Help</button></template></HelpTooltip></BaseDialog>` }))
    await flushPromises(); button('help').click(); await flushPromises()
    key('Escape'); await nextTick()
    expect(close.value).toBe(0); expect(document.activeElement).toBe(button('help'))
    key('Escape'); expect(close.value).toBe(1)
  })
  it('closing the top dialog restores its lower-dialog opener', async () => {
    const upper = ref(false)
    render(defineComponent({ components: { BaseDialog }, setup: () => ({ upper }), template: `<BaseDialog :show="true" title="Lower"><button id="lower">Lower</button></BaseDialog><BaseDialog :show="upper" title="Upper"><button id="upper">Upper</button></BaseDialog>` }))
    await flushPromises(); button('lower').focus(); upper.value = true; await flushPromises()
    upper.value = false; await flushPromises()
    expect(document.activeElement).toBe(button('lower')); expect(document.body.classList.contains('modal-open')).toBe(true)
  })
  it('new modal dismisses lower owned popovers so they cannot cover the top dialog', async () => {
    const upper = ref(false)
    render(defineComponent({ components: { BaseDialog, AppSelect }, setup: () => ({ upper }), template: `<BaseDialog :show="true" title="Lower"><AppSelect id="choose" :model-value="null" :searchable="true" :options="[]"/></BaseDialog><BaseDialog :show="upper" title="Upper"><button id="upper">Upper</button></BaseDialog>` }))
    await flushPromises(); button('choose').click(); await flushPromises()
    upper.value = true; await flushPromises()
    expect(button('choose').getAttribute('aria-expanded')).toBe('false')
    expect(document.querySelector('[role="listbox"]')).toBeNull()
    upper.value = false; await flushPromises()
    expect(document.activeElement).toBe(button('choose'))
  })
  it('sibling portal resolves ownership from trigger DOM and closes with owner', async () => {
    const show = ref(true), open = ref(false), trigger = ref<HTMLElement | null>(null), panel = ref<HTMLElement | null>(null)
    render(defineComponent({ components: { BaseDialog }, setup() {
      useOwnedPopover(open, { root: () => panel.value, trigger: () => trigger.value, close: () => { open.value = false } })
      return { show, open, trigger, panel }
    }, template: `<BaseDialog :show="show" title="Form"><button ref="trigger" id="sibling-trigger" @click="open=true">Open</button><button id="after">After</button></BaseDialog><Teleport to="body"><div v-if="open" ref="panel"><button id="portal">Portal</button></div></Teleport>` }))
    await flushPromises(); button('sibling-trigger').click(); await flushPromises(); button('sibling-trigger').focus()
    key('Tab'); expect(document.activeElement).toBe(button('portal'))
    key('Tab'); expect(document.activeElement).toBe(button('after'))
    button('portal').focus(); key('Tab', true); expect(document.activeElement).toBe(button('sibling-trigger'))
    show.value = false; await flushPromises(); expect(open.value).toBe(false)
  })
  it('disconnected opener falls back to remaining dialog focus', async () => {
    const upper = ref(false), openerExists = ref(true)
    render(defineComponent({ components: { BaseDialog }, setup: () => ({ upper, openerExists }), template: `<BaseDialog :show="true" title="Lower"><button v-if="openerExists" id="lower">Lower</button></BaseDialog><BaseDialog :show="upper" title="Upper"/>` }))
    await flushPromises(); button('lower').focus(); upper.value = true; await flushPromises(); openerExists.value = false; await nextTick()
    upper.value = false; await flushPromises()
    expect(document.activeElement?.closest('.modal-overlay')?.getAttribute('aria-labelledby')).toBe(document.querySelector('.modal-overlay')?.getAttribute('aria-labelledby'))
  })
  it('standalone Select Tab moves to the following page field', async () => {
    render(defineComponent({ components: { AppSelect }, template: `<AppSelect id="choose" :model-value="null" :searchable="true" :options="[]"/><button id="after">After</button>` }))
    button('choose').click(); await flushPromises(); key('Tab'); await nextTick()
    expect(document.activeElement).toBe(button('after'))
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
  it('traps Tab both ways and restores a connected opener on unmount', async () => {
    const opener = document.createElement('button'); document.body.append(opener); opener.focus()
    const wrapper = render(defineComponent({ components: { BaseDialog }, template: `<BaseDialog :show="true" title="Form"><button id="last">Last</button></BaseDialog>` }))
    await flushPromises(); button('last').focus(); key('Tab')
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Close modal')
    key('Tab', true); expect(document.activeElement).toBe(button('last'))
    wrapper.unmount(); wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(document.activeElement).toBe(opener)
  })
})
