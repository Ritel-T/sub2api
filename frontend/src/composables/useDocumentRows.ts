import { computed, nextTick, onMounted, onUnmounted, onBeforeUpdate, onUpdated, ref, shallowRef, triggerRef, watch, type Ref } from 'vue'
import { defaultRangeExtractor, observeElementOffset, observeElementRect, observeWindowOffset, useVirtualizer, useWindowVirtualizer } from '@tanstack/vue-virtual'

// TanStack retains its last offset across a host replacement. Seed it from the
// new viewport before its attachment scroll, so reparenting cannot move that host.
const observeCurrentElementOffset: typeof observeElementOffset = (instance, callback) => {
  callback(instance.scrollElement?.scrollTop ?? 0, false)
  return observeElementOffset(instance, callback)
}
const observeCurrentWindowOffset: typeof observeWindowOffset = (instance, callback) => {
  callback(instance.scrollElement?.scrollY ?? 0, false)
  return observeWindowOffset(instance, callback)
}

// A max-height viewport can start at zero until its first virtual rows exist.
// Seed a usable rect, then keep the last valid size across hidden/reparented frames.
const observeSizedElementRect: typeof observeElementRect = (instance, callback) => {
  const element = instance.scrollElement
  if (element) {
    const box = element.getBoundingClientRect()
    const limit = getComputedStyle(element).maxHeight
    const maxHeight = limit.endsWith('px') ? Number.parseFloat(limit) : 0
    callback({
      width: Math.max(1, box.width || element.clientWidth || window.innerWidth),
      height: Math.max(1, box.height || element.clientHeight || (Number.isFinite(maxHeight) && maxHeight > 0 ? maxHeight : window.innerHeight))
    })
  }
  return observeElementRect(instance, rect => { if (rect.height > 0) callback(rect) })
}

/** Records in the page or a containing scroll viewport, including an adjacent detail row. */
export function useDocumentRows<T>(
  rows: Ref<T[]>,
  body: Ref<HTMLElement | null>,
  keyOf: (row: T, index: number) => string | number,
  enabled: Ref<boolean>,
  estimateSize: () => number = () => 80,
  // Grouped presentation rows still activate at the number of source records.
  sourceCount: Ref<number> = computed(() => rows.value.length)
) {
  const margin = ref(0)
  const focusedKey = ref<string | number | null>(null)
  const focusedIndex = computed(() => {
    if (focusedKey.value === null) return null
    const index = rows.value.findIndex((row, position) => keyOf(row, position) === focusedKey.value)
    return index >= 0 ? index : null
  })
  watch(focusedIndex, index => {
    if (index === null) focusedKey.value = null
  }, { flush: 'sync' })
  const scrollHost = shallowRef<HTMLElement | null>(null)
  const active = computed(() => enabled.value && sourceCount.value > 100)
  const options = computed(() => {
    // Pin identity across refreshed or reordered rows, including unchanged counts.
    const focused = focusedIndex.value
    return ({
    count: active.value ? rows.value.length : 0,
    enabled: active.value,
    getItemKey: (index: number) => keyOf(rows.value[index], index),
    estimateSize,
    overscan: 8,
    scrollMargin: margin.value,
    rangeExtractor: (range: Parameters<typeof defaultRangeExtractor>[0]) => {
      const indices = defaultRangeExtractor(range)
      if (focused !== null && focused < rows.value.length && !indices.includes(focused)) {
        indices.push(focused)
        indices.sort((a, b) => a - b)
      }
      return indices
    }
  })
  })
  const windowVirtualizer = useWindowVirtualizer<HTMLElement>(computed(() => ({
    ...options.value,
    observeElementOffset: observeCurrentWindowOffset,
    enabled: active.value && !!body.value && !scrollHost.value
  })))
  const elementVirtualizer = useVirtualizer<HTMLElement, HTMLElement>(computed(() => ({
    ...options.value,
    getScrollElement: () => scrollHost.value,
    observeElementOffset: observeCurrentElementOffset,
    observeElementRect: observeSizedElementRect,
    initialOffset: () => scrollHost.value?.scrollTop ?? 0,
    enabled: active.value && !!scrollHost.value
  })))
  const virtualizer = shallowRef<typeof windowVirtualizer.value | typeof elementVirtualizer.value>(windowVirtualizer.value)
  // The Vue adapter notifies by triggerRef on the same instance. Forward that
  // notification; a computed selector would suppress it when identity is stable.
  watch([scrollHost, windowVirtualizer, elementVirtualizer], () => {
    const current = scrollHost.value ? elementVirtualizer.value : windowVirtualizer.value
    if (virtualizer.value !== current) virtualizer.value = current
    else triggerRef(virtualizer)
  }, { flush: 'sync' })
  const scrollElement = computed(() => scrollHost.value ?? (typeof window === 'undefined' ? null : window))
  const virtualItems = computed(() => virtualizer.value.getVirtualItems())
  const rendered = computed(() => active.value
    ? virtualItems.value.map((item, position) => ({
      row: rows.value[item.index], index: item.index,
      gapBefore: position ? Math.max(0, item.start - virtualItems.value[position - 1].end) : 0
    }))
    : rows.value.map((row, index) => ({ row, index, gapBefore: 0 })))
  const paddingTop = computed(() => active.value && virtualItems.value.length
    ? Math.max(0, virtualItems.value[0].start - margin.value) : 0)
  const paddingBottom = computed(() => active.value && virtualItems.value.length
    ? Math.max(0, virtualizer.value.getTotalSize() -
      (virtualItems.value[virtualItems.value.length - 1].end - margin.value)) : 0)
  let movingFocus: HTMLElement | null = null
  onBeforeUpdate(() => {
    const element = document.activeElement
    movingFocus = element instanceof HTMLElement && body.value?.contains(element) && focusedIndex.value !== null
      ? element : null
  })
  onUpdated(() => {
    const element = movingFocus
    movingFocus = null
    // Moving a keyed DOM row can drop focus even when the node survives.
    // Restore that exact node, without scrolling or overriding another control's focus.
    if (element?.isConnected && body.value?.contains(element) && focusedIndex.value !== null &&
      (document.activeElement === document.body || document.activeElement === document.documentElement)) {
      element.focus({ preventScroll: true })
    }
  })
  const observed = new Set<HTMLElement>()
  let observer: ResizeObserver | undefined
  let mutations: MutationObserver | undefined
  let layoutElements: HTMLElement[] = []
  let disposed = false
  let measuredContentWidth: number | null = null
  let frame = 0
  let measuredViewportWidth = typeof window === 'undefined' ? 0 : window.innerWidth

  function updateMargin() {
    const element = body.value
    if (!element) { margin.value = 0; return }
    const host = scrollHost.value
    margin.value = host
      ? element.getBoundingClientRect().top - host.getBoundingClientRect().top - host.clientTop + host.scrollTop
      : element.getBoundingClientRect().top + window.scrollY
  }
  function refreshScrollHost() {
    const ancestors: HTMLElement[] = []
    let host: HTMLElement | null = null
    for (let element = body.value; element; element = element.parentElement) {
      ancestors.push(element)
      if (element !== body.value && element !== document.body && element !== document.documentElement && !host) {
        const style = getComputedStyle(element)
        // Bind before overflow exists: an empty virtual body can otherwise make
        // a dialog look non-scrollable and incorrectly fall back to the page.
        if (/^(auto|scroll|overlay)$/.test(style.overflowY || style.overflow)) host = element
      }
    }
    if (host !== scrollHost.value) {
      scrollHost.value = host
      measuredContentWidth = null
      virtualizer.value.measure()
    }
    if (ancestors.length !== layoutElements.length || ancestors.some((element, index) => element !== layoutElements[index])) {
      for (const element of layoutElements) if (!ancestors.includes(element)) observer?.unobserve(element)
      for (const element of ancestors) if (!layoutElements.includes(element)) observer?.observe(element)
      layoutElements = ancestors
      mutations?.disconnect()
      for (const element of ancestors) {
        mutations?.observe(element, {
          attributes: true, attributeFilter: ['class', 'style'],
          // Direct parent changes detect reparenting without observing every row mutation.
          childList: element !== body.value
        })
      }
    }
    updateMargin()
  }
  function updateSize(element: HTMLElement) {
    if (!active.value || !element.isConnected) return
    const main = element.hasAttribute('data-document-detail')
      ? element.previousElementSibling as HTMLElement | null : element
    if (!main) return
    const index = Number(main.dataset.index)
    if (!Number.isInteger(index) || index >= rows.value.length) return
    const detail = main.nextElementSibling as HTMLElement | null
    const height = main.getBoundingClientRect().height +
      (detail?.hasAttribute('data-document-detail') ? detail.getBoundingClientRect().height : 0)
    if (height > 0) virtualizer.value.resizeItem(index, height)
  }
  function measure(element: unknown) {
    if (!(element instanceof HTMLElement)) return
    if (!observed.has(element)) {
      observed.add(element)
      observer?.observe(element)
    }
    nextTick(() => { if (!disposed) updateSize(element) })
  }
  function focused() {
    // A nested list can have its own data-index; only this body's direct row owns focus.
    let element = document.activeElement as HTMLElement | null
    while (element && element !== body.value) {
      if (element.parentElement === body.value) {
        const row = element.hasAttribute('data-document-detail') ? element.previousElementSibling as HTMLElement | null : element
        const index = row?.hasAttribute('data-index') ? Number(row.dataset.index) : -1
        focusedKey.value = Number.isInteger(index) && index >= 0 && index < rows.value.length
          ? keyOf(rows.value[index], index) : null
        return
      }
      element = element.parentElement
    }
    focusedKey.value = null
  }
  function focusLeft() {
    // Chrome fires focusout during a keyed DOM move. Let the same update restore
    // its surviving node before deciding focus has actually left the records.
    void nextTick(() => { if (!disposed) focused() })
  }
  function scheduleMeasure() {
    if (disposed) return
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => {
      if (disposed) return
      refreshScrollHost()
      const contentWidth = body.value?.getBoundingClientRect().width || scrollHost.value?.clientWidth || window.innerWidth
      // Reflow can switch compact rows to mobile records. Offscreen desktop
      // measurements must not survive that layout change; expansion alone does.
      if (measuredViewportWidth !== window.innerWidth || (measuredContentWidth !== null && measuredContentWidth !== contentWidth)) {
        virtualizer.value.measure()
      }
      measuredViewportWidth = window.innerWidth
      measuredContentWidth = contentWidth
      for (const element of observed) {
        if (!element.isConnected) { observer?.unobserve(element); observed.delete(element) }
        else updateSize(element)
      }
    })
  }
  onMounted(() => {
    if (typeof ResizeObserver !== 'undefined') observer = new ResizeObserver(scheduleMeasure)
    if (typeof MutationObserver !== 'undefined') mutations = new MutationObserver(scheduleMeasure)
    for (const element of observed) observer?.observe(element)
    window.addEventListener('resize', scheduleMeasure)
    document.addEventListener('focusin', focused)
    document.addEventListener('focusout', focusLeft)
    refreshScrollHost()
    scheduleMeasure()
  })
  watch([body, active, () => rows.value.length], async () => {
    await nextTick()
    if (disposed) return
    refreshScrollHost()
    scheduleMeasure()
  }, { flush: 'post' })
  onUnmounted(() => {
    disposed = true
    mutations?.disconnect()
    observer?.disconnect()
    cancelAnimationFrame(frame)
    window.removeEventListener('resize', scheduleMeasure)
    document.removeEventListener('focusin', focused)
    document.removeEventListener('focusout', focusLeft)
  })
  return { active, rendered, paddingTop, paddingBottom, measure, virtualizer, scrollElement }
}
