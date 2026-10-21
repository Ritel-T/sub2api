import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
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
