import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, triggerRef, watch, type Ref } from 'vue'
import { defaultRangeExtractor, observeElementOffset, observeWindowOffset, useVirtualizer, useWindowVirtualizer } from '@tanstack/vue-virtual'

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

/** Records in the page or a containing scroll viewport, including an adjacent detail row. */
export function useDocumentRows<T>(
  rows: Ref<T[]>,
  body: Ref<HTMLElement | null>,
  keyOf: (row: T, index: number) => string | number,
  enabled: Ref<boolean>,
  estimateSize: () => number = () => 80
) {
  const margin = ref(0)
  const focusedIndex = ref<number | null>(null)
  const scrollHost = shallowRef<HTMLElement | null>(null)
  const active = computed(() => enabled.value && rows.value.length > 100)
  const options = computed(() => ({
    count: active.value ? rows.value.length : 0,
    enabled: active.value,
    getItemKey: (index: number) => keyOf(rows.value[index], index),
    estimateSize,
    overscan: 8,
    scrollMargin: margin.value,
    rangeExtractor: (range: Parameters<typeof defaultRangeExtractor>[0]) => {
      const indices = defaultRangeExtractor(range)
      const focused = focusedIndex.value
      if (focused !== null && focused < rows.value.length && !indices.includes(focused)) {
        indices.push(focused)
        indices.sort((a, b) => a - b)
      }
      return indices
    }
  }))
  const windowVirtualizer = useWindowVirtualizer<HTMLElement>(computed(() => ({
    ...options.value,
    observeElementOffset: observeCurrentWindowOffset,
    enabled: active.value && !!body.value && !scrollHost.value
  })))
  const elementVirtualizer = useVirtualizer<HTMLElement, HTMLElement>(computed(() => ({
    ...options.value,
    getScrollElement: () => scrollHost.value,
    observeElementOffset: observeCurrentElementOffset,
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
    const element = document.activeElement?.closest<HTMLElement>('[data-index]')
    focusedIndex.value = element && body.value?.contains(element) ? Number(element.dataset.index) : null
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
    document.addEventListener('focusout', focused)
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
    document.removeEventListener('focusout', focused)
  })
  return { active, rendered, paddingTop, paddingBottom, measure, virtualizer, scrollElement }
}
