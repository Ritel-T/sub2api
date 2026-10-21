import { afterEach, describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'

import { findRowIndexByDomPosition, useSwipeSelect, type SwipeSelectVirtualContext } from '../useSwipeSelect'

/**
 * Build a fake scroll element whose `tbody tr[data-index]` rows expose stubbed
 * vertical rects, so we can exercise findRowIndexByDomPosition without a real DOM.
 * `index` is the value placed in the row's data-index attribute (its absolute
 * position in the sorted data), which need not equal the array position.
 */
function makeScrollEl(rows: Array<{ index: number; top: number; bottom: number }>): Element {
  const trs = rows.map((r) => ({
    getAttribute: (name: string) => (name === 'data-index' ? String(r.index) : null),
    getBoundingClientRect: () => ({
      top: r.top,
      bottom: r.bottom,
      left: 0,
      right: 0,
      width: 0,
      height: r.bottom - r.top,
      x: 0,
      y: r.top,
      toJSON: () => ({})
    })
  })) as unknown as HTMLElement[]

  return {
    querySelectorAll: (sel: string) =>
      (sel === 'tbody tr[data-index]' ? trs : []) as unknown as NodeListOf<Element>
  } as unknown as Element
}

describe('findRowIndexByDomPosition (swipe-select full-render fallback)', () => {
  // Variable row heights on purpose — the third row is taller.
  const rows = [
    { index: 0, top: 100, bottom: 200 },
    { index: 1, top: 200, bottom: 300 },
    { index: 2, top: 300, bottom: 450 }
  ]
  const el = makeScrollEl(rows)

  it('returns -1 when no rows are rendered', () => {
    expect(findRowIndexByDomPosition(makeScrollEl([]), 250)).toBe(-1)
  })

  it('locates the row whose rect contains the Y coordinate', () => {
    expect(findRowIndexByDomPosition(el, 150)).toBe(0)
    expect(findRowIndexByDomPosition(el, 250)).toBe(1)
    expect(findRowIndexByDomPosition(el, 400)).toBe(2) // inside the tall row
  })

  it('clamps to the first/last row when Y is outside the rendered range', () => {
    expect(findRowIndexByDomPosition(el, 50)).toBe(0) // above the first row
    expect(findRowIndexByDomPosition(el, 999)).toBe(2) // below the last row
  })

  it('picks the closer row when Y falls in a gap between rows', () => {
    const gapped = makeScrollEl([
      { index: 0, top: 100, bottom: 180 },
      { index: 1, top: 220, bottom: 300 }
    ])
    expect(findRowIndexByDomPosition(gapped, 190)).toBe(0) // 10px from row0.bottom vs 30px from row1.top
    expect(findRowIndexByDomPosition(gapped, 215)).toBe(1) // 35px from row0.bottom vs 5px from row1.top
  })

  it('returns the data-index attribute value, not the array position', () => {
    const remapped = makeScrollEl([
      { index: 5, top: 100, bottom: 200 },
      { index: 9, top: 200, bottom: 300 }
    ])
    expect(findRowIndexByDomPosition(remapped, 150)).toBe(5)
    expect(findRowIndexByDomPosition(remapped, 250)).toBe(9)
  })
})


describe('useSwipeSelect scroll ownership', () => {
  const wrappers: Array<ReturnType<typeof mount>> = []
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  function harness(documentMode: boolean, virtualized = false, contentOffset?: number, withContext = true, asyncScroll = false) {
    const frames = new Map<number, FrameRequestCallback>()
    let nextFrame = 1
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      const id = nextFrame++
      frames.set(id, callback)
      return id
    })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    Object.defineProperty(document.documentElement, 'clientWidth', { configurable: true, value: 1200 })
    Object.defineProperty(document.documentElement, 'clientHeight', { configurable: true, value: 800 })
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(800)
    let pageOffset = virtualized ? 1000 : 0
    vi.spyOn(window, 'scrollY', 'get').mockImplementation(() => pageOffset)
    let pendingScroll = 0
    const scrollBy = vi.spyOn(window, 'scrollBy').mockImplementation((_x, y) => {
      if (asyncScroll) pendingScroll += Number(y) || 0
      else pageOffset += Number(y) || 0
    })
    const selected = new Set<number>()
    const data = Array.from({ length: virtualized ? 150 : 3 }, (_, id) => ({ id }))
    const component = defineComponent({
      setup() {
        const container = ref<HTMLElement | null>(null)
        const context: SwipeSelectVirtualContext = {
          getVirtualizer: () => ({
            scrollElement,
            getVirtualItems: () => virtualized ? [
              { index: 10, start: 1100, end: 1180 },
              { index: 11, start: 1180, end: 1340 },
              { index: 12, start: 1340, end: 1420 }
            ] : [],
            options: { estimateSize: () => 80, scrollMargin: virtualized ? 300 : 0 }
          }) as unknown as ReturnType<SwipeSelectVirtualContext['getVirtualizer']>,
          getScrollElement: () => documentMode ? window : scrollElement,
          getContentOffset: () => contentOffset ?? (documentMode ? 0 : 40),
          getSortedData: () => data,
          getRowId: row => row.id
        }
        useSwipeSelect(container, {
          isSelected: id => selected.has(id),
          select: id => { selected.add(id) },
          deselect: id => { selected.delete(id) },
          batchUpdate: update => update(selected)
        }, withContext ? context : undefined)
        return () => h('div', { ref: container }, [h('div', { class: 'review-scroll' }, [h('table', [
          h('thead', [h('tr', [h('th', 'Name')])]),
          h('tbody', [
            ...data.slice(virtualized ? 10 : 0, virtualized ? 13 : 3).map(row => h('tr', { 'data-index': row.id, 'data-row-id': row.id }, [h('td', [h('button', 'Action'), h('span', { role: 'checkbox', class: 'review-checkbox' })])])),
            h('tr', { 'data-document-detail': '' }, [h('td', { class: 'review-detail' })])
          ])
        ])])])
      }
    })
    const wrapper = mount(component, { attachTo: document.body })
    wrappers.push(wrapper)
    const scrollElement = wrapper.get('.review-scroll').element as HTMLElement
    if (virtualized && !documentMode) scrollElement.scrollTop = 1000
    vi.spyOn(scrollElement, 'getBoundingClientRect').mockReturnValue({ top: 100, bottom: documentMode ? 2100 : 700, height: documentMode ? 2000 : 600, left: 0, right: 1000, width: 1000, x: 0, y: 100, toJSON() {} })
    vi.spyOn(wrapper.get('thead').element, 'getBoundingClientRect').mockReturnValue({ top: 100, bottom: 140, height: 40 } as DOMRect)
    wrapper.findAll('tr[data-index]').forEach((row, index) => {
      vi.spyOn(row.element, 'getBoundingClientRect').mockReturnValue({ top: 140 + index * 80, bottom: 220 + index * 80, height: 80 } as DOMRect)
    })
    const drag = (start: Element, startY: number, endY: number) => {
      start.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0, clientX: 100, clientY: startY }))
      document.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, clientX: 100, clientY: endY }))
    }
    const tick = () => {
      // Browser scrolling may commit after scrollBy returns, before the next animation frame.
      pageOffset += pendingScroll
      pendingScroll = 0
      const current = [...frames.entries()]
      frames.clear()
      current.forEach(([, callback]) => callback(0))
    }
    return { wrapper, selected, scrollBy, scrollElement, drag, tick, pendingFrames: () => frames.size }
  }

  it('scrolls the document at the viewport edge for an open table', () => {
    const test = harness(true)
    test.drag(test.wrapper.get('tr[data-index] td').element, 160, 790)
    test.tick()
    expect(test.scrollBy).toHaveBeenCalledWith(0, 8)
    expect(test.scrollElement.scrollTop).toBe(0)
  })

  it.each(['mouseup', 'blur'] as const)('extends selection when scrolling commits on the next frame and stops on %s', (stop) => {
    const test = harness(true, true, 0, true, true)
    test.drag(test.wrapper.get('tr[data-index="10"] td').element, 160, 790)
    const initialCount = test.selected.size
    for (let frame = 0; frame < 24; frame++) test.tick()
    expect(test.scrollBy).toHaveBeenCalled()
    expect(test.selected.size).toBeGreaterThan(initialCount)
    expect(test.selected.has(20)).toBe(true)
    if (stop === 'mouseup') document.dispatchEvent(new MouseEvent('mouseup'))
    else window.dispatchEvent(new Event('blur'))
    const finalSelection = [...test.selected]
    const scrollCalls = test.scrollBy.mock.calls.length
    test.tick()
    test.tick()
    expect([...test.selected]).toEqual(finalSelection)
    expect(test.scrollBy).toHaveBeenCalledTimes(scrollCalls)
    expect(test.pendingFrames()).toBe(0)
    expect(document.querySelector('[style*="pointer-events: none"]')).toBeNull()
  })

  it('uses document offsets and measured virtual ranges including expanded row height', () => {
    const test = harness(true, true)
    test.drag(test.wrapper.get('tr[data-index="11"] td').element, 200, 270)
    expect([...test.selected]).toEqual([11])
  })

  it('uses host-relative coordinates for open tables in a scrolling dialog', () => {
    const test = harness(false, true, 0)
    test.drag(test.wrapper.get('tr[data-index="11"] td').element, 280, 350)
    expect([...test.selected]).toEqual([11])
  })
  it('does not treat a horizontal-only wrapper as the fallback vertical scroll host', () => {
    const test = harness(true, false, 0, false)
    test.scrollElement.style.overflowX = 'auto'
    test.scrollElement.style.overflowY = 'hidden'
    Object.defineProperty(test.scrollElement, 'clientHeight', { configurable: true, value: 2000 })
    Object.defineProperty(test.scrollElement, 'scrollHeight', { configurable: true, value: 2000 })
    test.drag(test.wrapper.get('tr[data-index] td').element, 160, 790)
    test.tick()
    expect(test.scrollBy).toHaveBeenCalledWith(0, 8)
    expect(test.scrollElement.scrollTop).toBe(0)
  })
  it('retains element scrolling for the legacy internal-scroll table', () => {
    const test = harness(false)
    test.drag(test.wrapper.get('tr[data-index] td').element, 160, 690)
    test.tick()
    expect(test.scrollElement.scrollTop).toBe(8)
    expect(test.scrollBy).not.toHaveBeenCalled()
  })

  it('ignores drag starts in expanded details and action buttons', () => {
    const test = harness(true)
    test.drag(test.wrapper.get('.review-detail').element, 360, 790)
    test.tick()
    expect([...test.selected]).toEqual([])
    expect(test.scrollBy).not.toHaveBeenCalled()
    document.dispatchEvent(new MouseEvent('mouseup'))
    test.drag(test.wrapper.get('button').element, 160, 790)
    test.tick()
    expect([...test.selected]).toEqual([])
    expect(test.scrollBy).not.toHaveBeenCalled()
    document.dispatchEvent(new MouseEvent('mouseup'))
    test.drag(test.wrapper.get('.review-checkbox').element, 160, 790)
    test.tick()
    expect([...test.selected]).toEqual([])
    expect(test.scrollBy).not.toHaveBeenCalled()
  })
})
