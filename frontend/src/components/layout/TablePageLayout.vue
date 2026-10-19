<template>
  <div ref="layoutRef" class="table-page-layout" :class="{ 'mobile-mode': isMobile }" :style="layoutStyle">
    <!-- 固定区域：操作按钮 -->
    <div v-if="$slots.actions" class="layout-section-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区域：搜索和过滤器 -->
    <div v-if="$slots.filters" class="layout-section-fixed">
      <slot name="filters" />
    </div>

    <!-- 滚动区域：表格 -->
    <div class="layout-section-scrollable">
      <div class="card table-scroll-container">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区域：分页器 -->
    <div v-if="$slots.pagination" class="layout-section-fixed">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'

const isMobile = ref(false)
const layoutRef = ref<HTMLElement | null>(null)
const availableHeight = ref<number>()
const layoutStyle = computed(() => isMobile.value ? { height: 'auto', minHeight: '0' }
  : availableHeight.value !== undefined ? { height: `${availableHeight.value}px`, minHeight: '0' } : undefined)
let observer: ResizeObserver | undefined
let frame = 0
function measure() {
  isMobile.value = window.innerWidth < 1024
  const element = layoutRef.value
  const main = element?.closest('main')
  if (!element || !main || isMobile.value) return
  const padding = parseFloat(getComputedStyle(main).paddingBottom) || 0
  const remaining = window.innerHeight - (element.getBoundingClientRect().top + window.scrollY) - padding
  // On short viewports controls may need normal page scrolling.
  availableHeight.value = remaining >= 240 ? Math.floor(remaining) : undefined
}
function scheduleMeasure() {
  cancelAnimationFrame(frame)
  frame = requestAnimationFrame(measure)
}
onMounted(() => {
  measure()
  window.addEventListener('resize', scheduleMeasure)
  observer = new ResizeObserver(scheduleMeasure)
  const main = layoutRef.value?.closest('main')
  if (main) observer.observe(main)
  void document.fonts?.ready.then(scheduleMeasure)
  scheduleMeasure()
})
onUnmounted(() => {
  window.removeEventListener('resize', scheduleMeasure)
  observer?.disconnect()
  cancelAnimationFrame(frame)
})
</script>

<style scoped>
/* 桌面端：Flexbox 布局 */
.table-page-layout {
  @apply flex flex-col gap-6;
  height: calc(100dvh - var(--console-header-height, 88px) - 6rem);
  min-height: 420px; /* 减去 header + lg:p-8 的上下padding */
}

.layout-section-fixed {
  @apply flex-shrink-0;
}

.layout-section-scrollable {
  @apply flex-1 min-h-0 flex flex-col;
}

/* 表格滚动容器 - 增强版表体滚动方案 */
.table-scroll-container {
  @apply flex flex-col overflow-hidden h-full bg-white dark:bg-dark-800 rounded-2xl border border-gray-200 dark:border-dark-700 shadow-sm;
}

.table-scroll-container :deep(.table-wrapper) {
  @apply flex-1 overflow-x-auto overflow-y-auto;
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(thead) {
  @apply bg-gray-50/80 dark:bg-dark-800/80 backdrop-blur-sm;
}

.table-scroll-container :deep(th) {
  @apply px-5 py-4 text-left text-sm font-medium text-gray-600 dark:text-dark-300 border-b border-gray-200 dark:border-dark-700;
}

.table-scroll-container :deep(td) {
  @apply px-5 py-4 text-sm text-gray-700 dark:text-gray-300 border-b border-gray-100 dark:border-dark-800;
}

/* 移动端：恢复正常滚动 */
.table-page-layout.mobile-mode .table-scroll-container {
  @apply h-auto overflow-visible border-none shadow-none bg-transparent;
}

.table-page-layout.mobile-mode .layout-section-fixed > :deep(.flex) {
  flex-wrap: wrap;
  justify-content: flex-start;
}

.table-page-layout.mobile-mode .layout-section-scrollable {
  @apply flex-none min-h-fit;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  @apply flex-none;
  display: table;
  min-width: 100%;
}
</style>
