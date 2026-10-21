import { computed, nextTick, onMounted, onUnmounted, ref, watch, type Ref } from 'vue'
import { defaultRangeExtractor, useWindowVirtualizer } from '@tanstack/vue-virtual'

/** Window-scrolled records, including an optional adjacent expanded table row. */
export function useDocumentRows<T>(
  rows: Ref<T[]>,
  body: Ref<HTMLElement | null>,
  keyOf: (row: T, index: number) => string | number,
  enabled: Ref<boolean>,
  estimateSize: () => number = () => 80
) {
  const margin = ref(0)
  const focusedIndex = ref<number | null>(null)
  const active = computed(() => enabled.value && rows.value.length > 100)
  const virtualizer = useWindowVirtualizer(computed(() => ({
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
  })))
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
  let frame = 0
  let measuredViewportWidth = typeof window === 'undefined' ? 0 : window.innerWidth

  function updateMargin() {
    if (body.value) margin.value = body.value.getBoundingClientRect().top + window.scrollY
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
    nextTick(() => updateSize(element))
  }
  function focused() {
    const element = document.activeElement?.closest<HTMLElement>('[data-index]')
    focusedIndex.value = element && body.value?.contains(element) ? Number(element.dataset.index) : null
  }
  function scheduleMeasure() {
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => {
      // Reflow can switch compact rows to mobile records. Offscreen desktop
      // measurements must not survive that layout change; expansion alone does.
      if (measuredViewportWidth !== window.innerWidth) {
        measuredViewportWidth = window.innerWidth
        virtualizer.value.measure()
      }
      updateMargin()
      for (const element of observed) {
        if (!element.isConnected) { observer?.unobserve(element); observed.delete(element) }
        else updateSize(element)
      }
    })
  }
  onMounted(() => {
    if (typeof ResizeObserver !== 'undefined') observer = new ResizeObserver(scheduleMeasure)
    for (const element of observed) observer?.observe(element)
    window.addEventListener('resize', scheduleMeasure)
    document.addEventListener('focusin', focused)
    document.addEventListener('focusout', focused)
    updateMargin()
  })
  watch([body, active, () => rows.value.length], async () => {
    await nextTick()
    updateMargin()
    scheduleMeasure()
  }, { flush: 'post' })
  onUnmounted(() => {
    observer?.disconnect()
    cancelAnimationFrame(frame)
    window.removeEventListener('resize', scheduleMeasure)
    document.removeEventListener('focusin', focused)
    document.removeEventListener('focusout', focused)
  })
  return { active, rendered, paddingTop, paddingBottom, measure, virtualizer }
}
