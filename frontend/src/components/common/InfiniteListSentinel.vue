<template>
  <div ref="sentinel" class="infinite-list-sentinel" :aria-busy="loading || undefined">
    <button v-if="error" type="button" class="infinite-list-action" @click="$emit('retry')">
      {{ retryLabel }}
    </button>
    <span v-else-if="loading" role="status">{{ t('common.loading') }}</span>
    <button v-else-if="hasMore" type="button" class="infinite-list-action" @click="$emit('load-more')">
      {{ copy }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ loading?: boolean; error?: unknown; hasMore: boolean; progress?: number }>()
const emit = defineEmits<{ 'load-more': []; retry: [] }>()
const { t, locale } = useI18n()
const sentinel = ref<HTMLElement | null>(null)
const visible = ref(false)
const copy = computed(() => (locale?.value ?? 'en').startsWith('zh')
  ? locale.value.includes('HK') ? '載入更多' : '加载更多'
  : 'Load more')
const retryLabel = computed(() => (locale?.value ?? 'en').startsWith('zh')
  ? locale.value.includes('HK') ? '重試' : '重试'
  : 'Retry')
let observer: IntersectionObserver | undefined

function maybeLoad() {
  const rect = sentinel.value?.getBoundingClientRect()
  const nearViewport = rect && rect.top <= window.innerHeight + 480 && rect.bottom >= 0
  if (visible.value && nearViewport && props.hasMore && !props.loading && !props.error) emit('load-more')
}

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined' || !sentinel.value) return
  observer = new IntersectionObserver(entries => {
    visible.value = entries[0]?.isIntersecting ?? false
    maybeLoad()
  }, { rootMargin: '0px 0px 480px 0px' })
  observer.observe(sentinel.value)
})
watch(() => [props.loading, props.hasMore, props.error, props.progress], async () => {
  await nextTick()
  maybeLoad()
})
onUnmounted(() => observer?.disconnect())
</script>

<style scoped>
.infinite-list-sentinel { min-height: 1px; display: flex; justify-content: center; color: var(--theme-text-secondary); font-size: .875rem; }
.infinite-list-sentinel:has(button), .infinite-list-sentinel:has([role="status"]) { padding: 1.25rem 0; }
.infinite-list-action { padding: .5rem 1rem; text-decoration: underline; text-underline-offset: .25em; }
.infinite-list-action:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; }
</style>
