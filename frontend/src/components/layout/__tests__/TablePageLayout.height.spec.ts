import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import TablePageLayout from '../TablePageLayout.vue'
import { nextTick } from 'vue'

let wrapper: VueWrapper
afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks(); document.body.innerHTML = '' })
describe('table viewport allocation', () => {
  it('deducts the actual content offset and parent padding from the viewport', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 800 })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ top: 209, height: 0, left: 0, right: 0, bottom: 209, width: 0, x: 0, y: 209, toJSON: () => ({}) })
    const main = document.createElement('main'); main.style.paddingBottom = '32px'; document.body.append(main)
    wrapper = mount(TablePageLayout, { attachTo: main, props: { open: false } })
    await nextTick()
    expect(wrapper.attributes('style')).toContain('height: 559px')
  })
  it('uses natural document height on desktop by default', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1440 })
    wrapper = mount(TablePageLayout)
    await nextTick()
    expect(wrapper.attributes('style')).toContain('height: auto')
    expect(wrapper.find('.open-table-container').exists()).toBe(true)
    expect(wrapper.find('.card').exists()).toBe(false)
  })
  it('uses natural document height on phones', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    wrapper = mount(TablePageLayout)
    await nextTick()
    expect(wrapper.attributes('style')).toContain('height: auto')
    expect(wrapper.classes()).toContain('mobile-mode')
  })
})
