import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, onBeforeUpdate, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useDocumentRows } from '../useDocumentRows'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('useDocumentRows layout measurement', () => {
  it('invalidates offscreen heights on width changes, but retains them for row expansion', async () => {
    const initialWidth = window.innerWidth
    const callbacks: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callbacks.push(callback)
      return callbacks.length
    })
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    let rowResize: ResizeObserverCallback | undefined
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { rowResize = callback }
      observe() {}
      unobserve() {}
      disconnect() {}
    })
    let documentRows!: ReturnType<typeof useDocumentRows<{ id: number }>>
    const component = defineComponent({
      setup() {
        const body = ref<HTMLElement | null>(null)
        documentRows = useDocumentRows(ref([]), body, row => row.id, ref(true))
        return () => h('div', { ref: body })
      }
    })
    const wrapper = mount(component, { attachTo: document.body })
    await flushPromises()
    const flushFrames = () => {
      while (callbacks.length) callbacks.shift()!(0)
    }
    flushFrames()
    const measure = vi.spyOn(documentRows.virtualizer.value, 'measure')
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    window.dispatchEvent(new Event('resize'))
    flushFrames()
    expect(measure).toHaveBeenCalledOnce()
    rowResize?.([], {} as ResizeObserver)
    flushFrames()
    window.dispatchEvent(new Event('resize'))
    flushFrames()
    expect(measure).toHaveBeenCalledOnce()
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: initialWidth })
    window.dispatchEvent(new Event('resize'))
    flushFrames()
    expect(measure).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})


describe('useDocumentRows scroll host', () => {
  let frames: Map<number, FrameRequestCallback>
  let resizeObservers: Array<{ targets: Set<Element>; callback: ResizeObserverCallback }>
  let hostStates: WeakMap<Element, { top: number; width: number; height: number; border: number }>

  beforeEach(() => {
    frames = new Map()
    resizeObservers = []
    hostStates = new WeakMap()
    let nextFrame = 0
    vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
      frames.set(++nextFrame, callback)
      return nextFrame
    }))
    vi.stubGlobal('cancelAnimationFrame', vi.fn((id: number) => frames.delete(id)))
    vi.stubGlobal('scrollTo', vi.fn())
    vi.stubGlobal('scrollY', 0)
    vi.stubGlobal('innerHeight', 400)
    vi.stubGlobal('ResizeObserver', class {
      targets = new Set<Element>()
      constructor(public callback: ResizeObserverCallback) { resizeObservers.push(this) }
      observe(element: Element) { this.targets.add(element) }
      unobserve(element: Element) { this.targets.delete(element) }
      disconnect() { this.targets.clear() }
    })
  })

  const rectangle = (top: number, width: number, height: number) =>
    ({ top, left: 0, width, height, bottom: top + height, right: width, x: 0, y: top, toJSON() {} })
  const settle = async () => {
    for (let pass = 0; pass < 20; pass++) {
      await flushPromises()
      if (!frames.size) return
      const queued = [...frames.values()]
      frames.clear()
      queued.forEach(callback => callback(0))
    }
    throw new Error('layout measurement did not settle')
  }
  const resize = (element: HTMLElement) => {
    for (const observer of resizeObservers) {
      if (observer.targets.has(element)) {
        observer.callback([{ target: element, borderBoxSize: [{ inlineSize: element.offsetWidth, blockSize: element.offsetHeight }] }] as unknown as ResizeObserverEntry[], {} as ResizeObserver)
      }
    }
  }
  const createHost = (parent: HTMLElement = document.body) => {
    const host = document.createElement('div')
    host.dataset.scrollHost = ''
    host.style.overflowY = 'auto'
    const state = { top: 200, width: 400, height: 100, border: 2 }
    hostStates.set(host, state)
    Object.defineProperties(host, {
      offsetHeight: { configurable: true, get: () => state.height },
      offsetWidth: { configurable: true, get: () => state.width },
      clientHeight: { configurable: true, get: () => state.height - state.border * 2 },
      clientWidth: { configurable: true, get: () => state.width - state.border * 2 },
      clientTop: { configurable: true, get: () => state.border }
    })
    host.getBoundingClientRect = () => rectangle(state.top, state.width, state.height)
    host.scrollTo = vi.fn((options?: ScrollToOptions | number) => {
      if (typeof options === 'object' && options.top !== undefined) host.scrollTop = options.top
    })
    parent.append(host)
    return { host, state }
  }
  const mountRows = (parent: HTMLElement, count = 400, focusable = false, details = false, beforeUpdate?: () => void) => {
    const rows = ref(Array.from({ length: count }, (_, id) => ({ id })))
    let documentRows!: ReturnType<typeof useDocumentRows<{ id: number }>>
    const component = defineComponent({
      setup() {
        const body = ref<HTMLElement | null>(null)
        documentRows = useDocumentRows(rows, body, row => row.id, ref(true), () => 20)
        // Registered after the hook captures focus, matching Chrome blur during a keyed move.
        if (beforeUpdate) onBeforeUpdate(beforeUpdate)
        return () => h('div', { ref: body }, documentRows.rendered.value.flatMap(item => {
          const main = h('div', { key: item.row.id, 'data-index': item.index, 'data-row-index': item.index }, focusable ? h('button', { 'data-row-key': item.row.id }, String(item.row.id)) : String(item.row.id))
          return details ? [main, h('div', { key: `detail-${item.row.id}`, 'data-document-detail': '' }, h('button', { 'data-detail-key': item.row.id }, String(item.row.id)))] : [main]
        }))
      }
    })
    const wrapper = mount(component, { attachTo: parent })
    const body = wrapper.element as HTMLElement
    body.getBoundingClientRect = () => {
      const host = body.closest<HTMLElement>('[data-scroll-host]')
      const state = host && hostStates.get(host)
      return state && host
        ? rectangle(state.top + state.border + 50 - host.scrollTop, state.width, rows.value.length * 20)
        : rectangle(1000 - window.scrollY, 800, rows.value.length * 20)
    }
    return { wrapper, body, rows, documentRows }
  }

  it('virtualizes long dialog lists against the internal viewport without scrolling the locked page', async () => {
    const { host } = createHost()
    const { wrapper, documentRows } = mountRows(host)
    try {
      await settle()
      expect(documentRows.virtualizer.value.scrollElement).toBe(host)
      expect(documentRows.scrollElement.value).toBe(host)
      expect(documentRows.virtualizer.value.options.scrollMargin).toBe(50)
      expect(documentRows.rendered.value.every(item => item.index < 100)).toBe(true)
      host.scrollTop = 4050
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(documentRows.rendered.value.some(item => item.index === 200)).toBe(true)
      expect(wrapper.find('[data-row-index="200"]').exists()).toBe(true)
      expect(documentRows.rendered.value.length).toBeLessThan(100)
      expect(documentRows.paddingTop.value).toBeGreaterThan(3000)
      expect(documentRows.paddingBottom.value).toBeGreaterThan(0)
      expect(window.scrollTo).not.toHaveBeenCalled()
    } finally { wrapper.unmount(); host.remove() }
  })

  it('renders the initially empty window virtualizer after attachment and reacts to later page scrolling', async () => {
    const { wrapper, documentRows } = mountRows(document.body)
    try {
      await settle()
      expect(documentRows.scrollElement.value).toBe(window)
      expect(documentRows.virtualizer.value.scrollElement).toBe(window)
      expect(documentRows.virtualizer.value.options.scrollMargin).toBe(1000)
      expect(wrapper.findAll('[data-row-index]').length).toBeGreaterThan(0)
      expect(wrapper.findAll('[data-row-index]').length).toBeLessThan(100)
      vi.stubGlobal('scrollY', 3020)
      window.dispatchEvent(new Event('scroll'))
      await settle()
      expect(documentRows.rendered.value.some(item => item.index === 101)).toBe(true)
      expect(wrapper.find('[data-row-index="101"]').exists()).toBe(true)
      expect(documentRows.rendered.value.length).toBeLessThan(100)
    } finally { wrapper.unmount() }
  })

  it('finds the nearest viewport and switches when ancestor overflow changes', async () => {
    const outer = createHost()
    const inner = createHost(outer.host)
    const { wrapper, documentRows } = mountRows(inner.host)
    try {
      await settle()
      expect(documentRows.scrollElement.value).toBe(inner.host)
      inner.host.style.overflowY = 'hidden'
      await settle()
      expect(documentRows.scrollElement.value).toBe(outer.host)
      expect(documentRows.virtualizer.value.scrollElement).toBe(outer.host)
      outer.host.style.overflowY = 'visible'
      await settle()
      expect(documentRows.scrollElement.value).toBe(window)
      expect(documentRows.virtualizer.value.scrollElement).toBe(window)
    } finally { wrapper.unmount(); outer.host.remove() }
  })

  it('rebinds a moved list without carrying the previous viewport scroll offset', async () => {
    const first = createHost()
    const second = createHost()
    second.state.top = 300
    const { wrapper, body, documentRows } = mountRows(first.host)
    const removeOldListener = vi.spyOn(first.host, 'removeEventListener')
    try {
      await settle()
      first.host.scrollTop = 4050
      first.host.dispatchEvent(new Event('scroll'))
      await settle()
      second.host.scrollTop = 650
      second.host.append(body)
      await settle()
      expect(documentRows.scrollElement.value).toBe(second.host)
      expect(documentRows.virtualizer.value.scrollElement).toBe(second.host)
      expect(documentRows.virtualizer.value.options.scrollMargin).toBe(50)
      expect(second.host.scrollTop).toBe(650)
      expect(documentRows.rendered.value.some(item => item.index === 30)).toBe(true)
      expect(removeOldListener.mock.calls.some(([type]) => type === 'scroll')).toBe(true)
    } finally { wrapper.unmount(); first.host.remove(); second.host.remove() }
  })

  it('reports the viewport below the threshold and starts virtualizing when rows grow', async () => {
    const { host } = createHost()
    const { wrapper, rows, documentRows } = mountRows(host, 25)
    try {
      await settle()
      expect(documentRows.active.value).toBe(false)
      expect(documentRows.scrollElement.value).toBe(host)
      expect(documentRows.rendered.value).toHaveLength(25)
      rows.value = Array.from({ length: 400 }, (_, id) => ({ id }))
      await settle()
      expect(documentRows.active.value).toBe(true)
      expect(documentRows.virtualizer.value.scrollElement).toBe(host)
      expect(documentRows.rendered.value.length).toBeLessThan(100)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('updates viewport height and invalidates offscreen row heights when only container width changes', async () => {
    const { host, state } = createHost()
    const { wrapper, documentRows } = mountRows(host)
    try {
      await settle()
      const oldCount = documentRows.rendered.value.length
      const measure = vi.spyOn(documentRows.virtualizer.value, 'measure')
      state.height = 300
      resize(host)
      await settle()
      expect(documentRows.rendered.value.length).toBeGreaterThan(oldCount)
      expect(measure).not.toHaveBeenCalled()
      state.width = 320
      resize(host)
      await settle()
      expect(measure).toHaveBeenCalledOnce()
    } finally { wrapper.unmount(); host.remove() }
  })

  it('preserves the focused stable record when refresh prepends a newer row', async () => {
    const { host } = createHost()
    const { wrapper, rows, documentRows } = mountRows(host, 200, true)
    try {
      await settle()
      const button = wrapper.get('[data-row-key="0"]').element as HTMLButtonElement
      button.focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      rows.value = [{ id: 999 }, ...rows.value]
      await settle()
      expect(button.isConnected).toBe(true)
      expect(document.activeElement).toBe(button)
      expect(documentRows.rendered.value.some(item => item.row.id === 0 && item.index === 1)).toBe(true)
      expect(documentRows.rendered.value.length).toBeLessThan(40)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('tracks the focused record through an equal-length reorder', async () => {
    const { host } = createHost()
    const { wrapper, rows, documentRows } = mountRows(host, 200, true)
    try {
      await settle()
      const button = wrapper.get('[data-row-key="0"]').element as HTMLButtonElement
      button.focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      rows.value = [...rows.value.slice(1), rows.value[0]]
      await settle()
      expect(document.activeElement).toBe(button)
      expect(documentRows.rendered.value.some(item => item.row.id === 0 && item.index === 199)).toBe(true)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('retains the focused node when Chrome emits focusout during its keyed reorder', async () => {
    const { host } = createHost()
    let simulateMove = false
    let button: HTMLButtonElement | undefined
    let focusouts = 0
    const { wrapper, rows, documentRows } = mountRows(host, 200, true, false, () => {
      if (simulateMove) {
        simulateMove = false
        button?.blur()
      }
    })
    try {
      await settle()
      button = wrapper.get('[data-row-key="0"]').element as HTMLButtonElement
      button.addEventListener('focusout', () => { focusouts++ })
      button.focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(document.activeElement).toBe(button)
      simulateMove = true
      rows.value = [...rows.value.slice(1), rows.value[0]]
      await settle()
      expect(focusouts).toBeGreaterThan(0)
      expect(button.isConnected).toBe(true)
      expect(wrapper.get('[data-row-key="0"]').element).toBe(button)
      expect(document.activeElement).toBe(button)
      expect(documentRows.rendered.value.some(item => item.row.id === 0 && item.index === 199)).toBe(true)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('does not pin the replacement index after the focused record is deleted', async () => {
    const { host } = createHost()
    const { wrapper, rows, documentRows } = mountRows(host, 200, true)
    try {
      await settle()
      const button = wrapper.get('[data-row-key="0"]').element as HTMLButtonElement
      button.focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      rows.value = rows.value.filter(row => row.id !== 0)
      await settle()
      expect(button.isConnected).toBe(false)
      expect(documentRows.rendered.value.every(item => item.index > 100)).toBe(true)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('releases its retained row when focus moves outside the list, even to another indexed row', async () => {
    const { host } = createHost()
    const { wrapper, documentRows } = mountRows(host, 200, true)
    const outside = document.createElement('button')
    outside.dataset.index = '0'
    document.body.append(outside)
    try {
      await settle()
      ;(wrapper.get('[data-row-key="0"]').element as HTMLButtonElement).focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(documentRows.rendered.value.some(item => item.row.id === 0)).toBe(true)
      outside.focus()
      await settle()
      expect(document.activeElement).toBe(outside)
      expect(documentRows.rendered.value.every(item => item.index > 100)).toBe(true)
    } finally { wrapper.unmount(); host.remove(); outside.remove() }
  })

  it('uses the owning record index instead of a nested child list index', async () => {
    const { host } = createHost()
    const { wrapper, documentRows } = mountRows(host, 200, true)
    try {
      await settle()
      const button = wrapper.get('[data-row-key="0"]').element as HTMLButtonElement
      button.dataset.index = '150'
      button.focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(document.activeElement).toBe(button)
      expect(documentRows.rendered.value.some(item => item.row.id === 0)).toBe(true)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('pins the owning main row when focus is inside its adjacent detail row', async () => {
    const { host } = createHost()
    const { wrapper, rows, documentRows } = mountRows(host, 200, false, true)
    try {
      await settle()
      const button = wrapper.get('[data-detail-key="0"]').element as HTMLButtonElement
      button.focus()
      host.scrollTop = 3070
      host.dispatchEvent(new Event('scroll'))
      await settle()
      expect(document.activeElement).toBe(button)
      expect(documentRows.rendered.value.some(item => item.row.id === 0)).toBe(true)
      rows.value = [{ id: 999 }, ...rows.value]
      await settle()
      expect(document.activeElement).toBe(button)
      expect(documentRows.rendered.value.some(item => item.row.id === 0 && item.index === 1)).toBe(true)
      expect(documentRows.rendered.value.length).toBeLessThan(40)
    } finally { wrapper.unmount(); host.remove() }
  })

  it('removes scroll and layout observers and pending frames on unmount', async () => {
    const { host } = createHost()
    const add = vi.spyOn(host, 'addEventListener')
    const remove = vi.spyOn(host, 'removeEventListener')
    const { wrapper } = mountRows(host)
    await settle()
    window.dispatchEvent(new Event('resize'))
    wrapper.unmount()
    const callsAfterUnmount = vi.mocked(requestAnimationFrame).mock.calls.length
    host.style.overflowY = 'visible'
    host.dispatchEvent(new Event('scroll'))
    window.dispatchEvent(new Event('resize'))
    await settle()
    expect(vi.mocked(requestAnimationFrame).mock.calls.length).toBe(callsAfterUnmount)
    expect(frames.size).toBe(0)
    expect(resizeObservers.every(observer => observer.targets.size === 0)).toBe(true)
    for (const [type, listener] of add.mock.calls.filter(([type]) => type === 'scroll')) {
      expect(remove.mock.calls.some(([removedType, removedListener]) => removedType === type && removedListener === listener)).toBe(true)
    }
    host.remove()
  })
})
