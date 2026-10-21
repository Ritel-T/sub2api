<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import type { CustomEndpoint } from '@/types'

const props = defineProps<{
  apiBaseUrl: string
  customEndpoints: CustomEndpoint[]
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copiedEndpoint = ref<string | null>(null)

let copiedResetTimer: number | undefined

const allEndpoints = computed(() => {
  const items: Array<{ name: string; endpoint: string; description: string; isDefault: boolean }> = []
  if (props.apiBaseUrl) {
    items.push({
      name: t('keys.endpoints.title'),
      endpoint: props.apiBaseUrl,
      description: '',
      isDefault: true,
    })
  }
  for (const ep of props.customEndpoints) {
    items.push({ ...ep, isDefault: false })
  }
  return items
})

async function copy(url: string) {
  const success = await copyToClipboard(url, t('keys.endpoints.copied'))
  if (!success) return

  copiedEndpoint.value = url
  if (copiedResetTimer !== undefined) {
    window.clearTimeout(copiedResetTimer)
  }
  copiedResetTimer = window.setTimeout(() => {
    if (copiedEndpoint.value === url) {
      copiedEndpoint.value = null
    }
  }, 1800)
}

function tooltipHint(endpoint: string): string {
  return copiedEndpoint.value === endpoint
    ? t('keys.endpoints.copiedHint')
    : t('keys.endpoints.clickToCopy')
}

function speedTestUrl(endpoint: string): string {
  return `https://www.tcptest.cn/http/${encodeURIComponent(endpoint)}`
}

onBeforeUnmount(() => {
  if (copiedResetTimer !== undefined) {
    window.clearTimeout(copiedResetTimer)
  }
})
</script>

<template>
  <div v-if="allEndpoints.length" class="endpoint-list">
    <div v-for="item in allEndpoints" :key="item.endpoint" class="endpoint-item">
      <span class="endpoint-label">{{ item.name }}</span>
      <button
        type="button"
        class="endpoint-copy"
        :title="[item.description, tooltipHint(item.endpoint)].filter(Boolean).join(' · ')"
        :aria-label="`${tooltipHint(item.endpoint)}: ${item.endpoint}`"
        @click="copy(item.endpoint)"
      >
        <code>{{ item.endpoint }}</code>
        <svg v-if="copiedEndpoint === item.endpoint" width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
        <svg v-else width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/></svg>
      </button>
      <a :href="speedTestUrl(item.endpoint)" target="_blank" rel="noopener noreferrer" class="endpoint-test" :title="t('keys.endpoints.speedTest')" :aria-label="t('keys.endpoints.speedTest')">
        <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>
      </a>
    </div>
  </div>
</template>

<style scoped>
.endpoint-list { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 32px; min-width: 0; }
.endpoint-item { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 12px; min-width: 0; font-size: 13px; }
.endpoint-label { color: var(--ritel-muted); }
.endpoint-copy { display: inline-flex; align-items: center; gap: 8px; min-width: 0; min-height: 32px; color: var(--ritel-fg); text-align: left; }
.endpoint-copy code { font-size: 13px; overflow-wrap: anywhere; }
.endpoint-copy svg { flex: none; color: var(--ritel-muted); }
.endpoint-copy:hover code { text-decoration: underline; text-underline-offset: 4px; }
.endpoint-test { display: inline-flex; align-items: center; justify-content: center; min-width: 32px; min-height: 32px; color: var(--ritel-muted); }
.endpoint-test:hover { color: var(--ritel-fg); }
.endpoint-copy:focus-visible, .endpoint-test:focus-visible { outline: 2px solid var(--ritel-focus); outline-offset: 3px; }
</style>
