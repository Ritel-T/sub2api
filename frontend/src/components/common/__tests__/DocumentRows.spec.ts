import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DocumentRows from '../DocumentRows.vue'
import QualityAccountSelector from '@/components/admin/operations/QualityAccountSelector.vue'
import type { AccountListItem } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let frames: Map<number, FrameRequestCallback>
const disposals: Array<() => void> = []
const rectangle = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 400, width: 400, x: 0, y: top, toJSON() {} })
beforeEach(() => {
  frames = new Map(); let nextFrame = 0
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('scrollY', 0)
  vi.stubGlobal('innerHeight', 200)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
    const viewport = this.closest<HTMLElement>('[data-viewport]')
    if (this === viewport) return rectangle(0, 200)
    const index = this.hasAttribute('data-document-row') ? Number(this.dataset.index) : 0
    return rectangle(index * 20 - (viewport?.scrollTop ?? 0), this.hasAttribute('data-document-row') ? 20 : 4000)
  })
})
afterEach(() => {
  disposals.splice(0).reverse().forEach(dispose => dispose())
  vi.restoreAllMocks(); vi.unstubAllGlobals()
})
function viewport(element = document.createElement('div')) {
  element.dataset.viewport = ''
  element.style.overflowY = 'auto'
  Object.defineProperties(element, {
    offsetHeight: { configurable: true, get: () => 200 }, clientHeight: { configurable: true, get: () => 200 },
    offsetWidth: { configurable: true, get: () => 400 }, clientWidth: { configurable: true, get: () => 400 }
  })
  element.scrollTo = vi.fn((options?: ScrollToOptions | number) => { if (typeof options === 'object' && options.top !== undefined) element.scrollTop = options.top })
  return element
}
function createHost() {
  const host = viewport(); document.body.append(host)
  disposals.push(() => host.remove())
  return host
}
async function settle() {
  for (let pass = 0; pass < 30; pass++) {
    await flushPromises()
    if (!frames.size) return
    const queued = [...frames.values()]; frames.clear(); queued.forEach(callback => callback(0))
  }
  throw new Error('record layout did not settle')
}
function mountRows(count: number, as: 'div' | 'ul' | 'tbody' = 'div', host = createHost()) {
  const items = ref(Array.from({ length: count }, (_, id) => ({ id, name: `Record ${id}` })))
  const component = defineComponent({
    components: { DocumentRows },
    setup: () => ({ items, attrs: (item: { id: number }) => ({ id: `row-${item.id}`, 'data-index': 999 }) }),
    template: as === 'tbody'
      ? '<table><DocumentRows as="tbody" :items="items" item-key="id" :estimate-height="20" :colspan="2" :item-attrs="attrs"><template #default="{item}"><td>{{item.id}}</td><td><button :data-id="item.id">{{item.name}}</button></td></template></DocumentRows></table>'
      : `<DocumentRows as="${as}" :items="items" item-key="id" :estimate-height="20"><template #default="{item}"><button :data-id="item.id">{{item.name}}</button></template></DocumentRows>`
  })
  const wrapper = mount(component, { attachTo: host })
  disposals.push(() => wrapper.unmount())
  return { items, host, wrapper }
}

describe('DocumentRows actual record rendering', () => {
  it('renders all small records, virtualizes growth, and restores the whole filtered small set', async () => {
    const { items, wrapper } = mountRows(100)
    await settle()
    expect(wrapper.findAll('[data-document-row]')).toHaveLength(100)
    items.value.push({ id: 100, name: 'Record 100' })
    await settle()
    expect(wrapper.findAll('[data-document-row]').length).toBeLessThan(40)
    items.value = [{ id: 90, name: 'Filtered record 90' }, { id: 95, name: 'Filtered record 95' }]
    await settle()
    expect(wrapper.findAll('[data-document-row]')).toHaveLength(2)
    expect(wrapper.text()).toContain('Filtered record 95')
    expect(wrapper.findAll('[data-document-spacer]')).toHaveLength(0)
  })

  it.each(['div', 'ul', 'tbody'] as const)('retains a focused record with the missing gap inside a long %s list', async as => {
    const { wrapper, host } = mountRows(200, as)
    await settle()
    const focused = wrapper.get('[data-id="0"]').element as HTMLButtonElement
    focused.focus()
    host.scrollTop = 2800; host.dispatchEvent(new Event('scroll'))
    await settle()
    expect(wrapper.find('[data-id="140"]').exists()).toBe(true)
    expect(document.activeElement).toBe(focused)
    expect(wrapper.findAll('[data-document-row]').length).toBeLessThan(40)
    const gaps = wrapper.findAll('[data-document-spacer]')
    expect(gaps.length).toBeGreaterThan(0)
    expect(gaps.every(gap => gap.attributes('aria-hidden') === 'true')).toBe(true)
    const gapHeights = gaps.map(gap => parseFloat((as === 'tbody' ? gap.get('td') : gap).attributes('style')?.match(/height: ([\d.]+)px/)?.[1] ?? '0'))
    expect(Math.max(...gapHeights)).toBeGreaterThan(2000)
    if (as === 'ul') expect(wrapper.get('ul').findAll(':scope > div')).toHaveLength(0)
    if (as === 'tbody') {
      expect(gaps.every(gap => gap.element.tagName === 'TR' && gap.get('td').attributes('colspan') === '2')).toBe(true)
      expect(wrapper.get('[data-document-row]').attributes('data-index')).toBe('0')
      expect(wrapper.get('[data-document-row]').attributes('id')).toBe('row-0')
      expect(wrapper.get('tbody').findAll(':scope > div')).toHaveLength(0)
    }
    expect(window.scrollTo).not.toHaveBeenCalled()
  })

  it('keeps an edited focused record keyed by identity after reordering', async () => {
    const { items, wrapper } = mountRows(20)
    await settle()
    const button = wrapper.get('[data-id="0"]').element as HTMLButtonElement
    button.focus()
    items.value = [...items.value].reverse()
    await settle()
    expect(wrapper.get('[data-id="0"]').element).toBe(button)
    expect(document.activeElement).toBe(button)
  })
})

describe('account selector record grid integration', () => {
  it('starts windowing at 101 source accounts while retaining two-column pairs and offscreen selection', async () => {
    const accounts = ref(Array.from({ length: 100 }, (_, index) => ({ id: index + 1, name: `Account ${index + 1}` })) as AccountListItem[])
    const selected = ref([150])
    const selectPage = vi.fn()
    const component = defineComponent({
      components: { QualityAccountSelector },
      setup: () => ({ accounts, selected, selectPage, search: ref(''), group: ref(''), type: ref(''), statuses: ref([]), scope: ref('manual'), disabledReason: (account: AccountListItem) => account.id === 2 ? 'Unavailable' : '' }),
      template: '<QualityAccountSelector v-model="selected" v-model:search="search" v-model:group="group" v-model:type="type" v-model:statuses="statuses" v-model:scope="scope" :accounts="accounts" :groups="[]" :accounts-loading="false" :selecting-accounts="false" accounts-error="" :has-more="false" :accounts-total="accounts.length" :bulk="false" :disabled-reason="disabledReason" @select-page="selectPage" />'
    })
    const wrapper = mount(component, { attachTo: createHost() }); disposals.push(() => wrapper.unmount())
    const host = viewport(wrapper.get('.max-h-52').element as HTMLElement)
    await settle()
    expect(wrapper.findAll('.max-h-52 input[type="checkbox"]')).toHaveLength(100)
    accounts.value.push({ id: 101, name: 'Account 101' } as AccountListItem)
    await settle()
    expect(wrapper.findAll('.max-h-52 input[type="checkbox"]').length).toBeLessThan(80)
    expect(wrapper.get('[data-document-row]').findAll('label')).toHaveLength(2)
    expect(wrapper.get('input[value="2"]').attributes('disabled')).toBeDefined()
    await wrapper.get('input[value="1"]').setValue(true)
    expect(selected.value).toEqual([150, 1])
    await wrapper.get('[data-testid="quality-select-page"]').trigger('click')
    expect(selectPage).toHaveBeenCalledOnce()
    host.scrollTop = 900; host.dispatchEvent(new Event('scroll')); await settle()
    expect(wrapper.find('input[value="101"]').exists()).toBe(true)
    expect(selected.value).toEqual([150, 1])
  })
})


describe('DocumentRows initially empty viewport bootstrap', () => {
  it('fills an auto-height max-height viewport instead of remaining in a zero-height cycle', async () => {
    const host = createHost()
    host.style.maxHeight = '280px'
    const contentHeight = () => {
      const rows = host.querySelectorAll('[data-document-row]').length * 20
      const gaps = Array.from(host.querySelectorAll<HTMLElement>('[data-document-spacer]')).reduce((total, gap) => total + (parseFloat(gap.style.height) || 0), 0)
      return Math.min(280, rows + gaps)
    }
    Object.defineProperties(host, {
      offsetHeight: { configurable: true, get: contentHeight },
      clientHeight: { configurable: true, get: contentHeight }
    })
    host.getBoundingClientRect = () => rectangle(0, contentHeight())
    expect(host.offsetHeight).toBe(0)
    const { wrapper } = mountRows(600, 'div', host)
    await settle()
    expect(wrapper.findAll('[data-document-row]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('[data-document-row]').length).toBeLessThan(60)
    expect(host.offsetHeight).toBe(280)
    host.scrollTop = 6000; host.dispatchEvent(new Event('scroll')); await settle()
    expect(wrapper.find('[data-id="300"]').exists()).toBe(true)
  })

  it('keeps records through a zero resize and measures a visible positive viewport again', async () => {
    const observers: Array<{ targets: Set<Element>; callback: ResizeObserverCallback }> = []
    vi.stubGlobal('ResizeObserver', class {
      targets = new Set<Element>()
      constructor(public callback: ResizeObserverCallback) { observers.push(this) }
      observe(element: Element) { this.targets.add(element) }
      unobserve(element: Element) { this.targets.delete(element) }
      disconnect() { this.targets.clear() }
    })
    const host = createHost()
    host.style.maxHeight = '280px'
    let height = 200
    Object.defineProperties(host, {
      offsetHeight: { configurable: true, get: () => height },
      clientHeight: { configurable: true, get: () => height }
    })
    host.getBoundingClientRect = () => rectangle(0, height)
    const resized = () => {
      for (const observer of observers.filter(observer => observer.targets.has(host))) {
        observer.callback([{ target: host, borderBoxSize: [{ inlineSize: 400, blockSize: height }] }] as unknown as ResizeObserverEntry[], observer as unknown as ResizeObserver)
      }
    }
    const { wrapper } = mountRows(600, 'div', host)
    await settle()
    expect(wrapper.findAll('[data-document-row]').length).toBeGreaterThan(0)
    height = 0; host.style.display = 'none'; resized(); await settle()
    expect(wrapper.findAll('[data-document-row]').length).toBeGreaterThan(0)
    height = 260; host.style.display = 'block'; resized(); await settle()
    host.scrollTop = 6000; host.dispatchEvent(new Event('scroll')); await settle()
    expect(wrapper.find('[data-id="300"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-document-row]').length).toBeLessThan(60)
  })
})
