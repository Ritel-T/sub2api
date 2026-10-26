<template>
  <div class="open-section overview-key-workspace">
  <section class="overview-create" aria-labelledby="quick-start-title">
    <AppHeading id="quick-start-title" as="h2" variant="section">{{ copy.quickStart }}</AppHeading>
    <p class="mt-2 text-gray-500 dark:text-gray-400">{{ copy.quickHint }}</p>
    <p v-if="groupsLoading" class="py-6" role="status">{{ copy.loading }}</p>
    <div v-else-if="groupsError" class="flex flex-wrap items-center gap-3 py-6" role="alert">
      <span>{{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="$emit('retryGroups')">{{ copy.retry }}</button>
    </div>
    <div v-else-if="platformGroups.length" class="overview-platforms">
      <button v-for="group in platformGroups" :key="platformBucket(group.platform)" type="button" class="overview-platform" :class="platformBadgeClass(group.platform)" :data-platform="platformBucket(group.platform)" :disabled="creating !== null || creationUnknown" @click="createKey(group)">
        <span class="inline-flex items-center gap-1" aria-hidden="true"><PlatformIcon :platform="platformIcon(group.platform)" size="lg" /></span>{{ creationLabel(group.platform) }}<span v-if="creating === group.platform" role="status"> · {{ copy.creating }}</span>
      </button>
    </div>
    <p v-else class="py-6 text-gray-500 dark:text-gray-400">{{ copy.noGroups }}</p>
    <p v-if="createError" class="pb-4" role="alert">{{ creationUnknown ? copy.createUnknown : copy.createFailed }} <router-link v-if="creationUnknown" to="/keys" class="inline-flex min-h-[44px] items-center gap-2 no-underline">{{ copy.manageKeys }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></router-link></p>
  </section>

  <section class="overview-recent" aria-labelledby="recent-keys-title">
    <header class="flex flex-wrap items-center justify-between gap-3 pb-2">
      <AppHeading id="recent-keys-title" as="h2" variant="section">{{ copy.recentKeys }}</AppHeading>
      <router-link to="/keys" class="inline-flex min-h-[44px] items-center gap-2 no-underline">{{ copy.manageKeys }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></router-link>
    </header>
    <p v-if="loading" class="py-6" role="status">{{ copy.loading }}</p>
    <div v-else-if="error" class="flex flex-wrap items-center gap-3 py-6" role="alert"><span>{{ copy.loadFailed }}</span><button type="button" class="btn btn-secondary" @click="$emit('retry')">{{ copy.retry }}</button></div>
    <ul v-else-if="recentKeys.length" class="divide-y divide-gray-200 dark:divide-dark-700">
      <li v-for="key in recentKeys" :key="key.id" class="overview-key-row" :data-key-id="key.id">
        <div class="min-w-0"><p class="break-words font-medium">{{ key.name }}</p><p class="text-sm text-gray-500 dark:text-gray-400">{{ platformLabel(key.group?.platform) }}</p></div>
        <button type="button" class="btn btn-secondary overview-key-use" @click="selectedKey = key">{{ copy.useKey }}</button>
      </li>
    </ul>
    <p v-else class="py-6 text-gray-500 dark:text-gray-400">{{ copy.noKeys }}</p>
  </section>

  </div>
  <UseKeyModal v-if="selectedKey" :show="true" :api-key="selectedKey.key" :key-record="selectedKey" :public-settings="appStore.cachedPublicSettings" :base-url="baseUrl" :platform="selectedKey.group?.platform ?? null" :allow-messages-dispatch="selectedKey.group?.allow_messages_dispatch" @close="selectedKey = null" />
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import AppHeading from '@/components/common/AppHeading.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { platformBadgeClass } from '@/utils/platformColors'
import { getKeyGroupProvider } from '@/utils/keyGroupProviders'
import UseKeyModal from '@/components/keys/UseKeyModal.vue'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import { keysAPI } from '@/api/keys'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { ApiKey, Group, GroupPlatform } from '@/types'
import { useOverviewCopy } from './overviewCopy'

const props = defineProps<{ recentKeys: ApiKey[]; groups: Group[]; loading: boolean; error: boolean; groupsLoading: boolean; groupsError: boolean }>()
const emit = defineEmits<{ retry: []; retryGroups: []; created: [key: ApiKey] }>()
const copy = useOverviewCopy()
const appStore = useAppStore()
const authStore = useAuthStore()
const selectedKey = ref<ApiKey | null>(null)
const creating = ref<string | null>(null)
const createError = ref(false)
const creationUnknown = ref(false)
const baseUrl = computed(() => appStore.cachedPublicSettings?.api_base_url || window.location.origin)
const platformBucket = (platform: GroupPlatform) => getKeyGroupProvider(platform) === 'domestic' ? 'domestic' : platform
const platformIcon = (platform: GroupPlatform): GroupPlatform => platformBucket(platform) === 'domestic' ? 'deepseek' : platform
const creationLabel = (platform: GroupPlatform) => platformBucket(platform) === 'domestic' ? copy.value.domestic : platformLabel(platform)
// Available groups are already permission-filtered and ordered by the server.
const platformGroups = computed(() => {
  const seen = new Set<string>()
  return props.groups.filter(group => {
    const bucket = platformBucket(group.platform)
    if (group.status !== 'active' || seen.has(bucket)) return false
    seen.add(bucket)
    return true
  })
})
const platformLabel = (platform?: string) => GROUP_PLATFORM_OPTIONS.find(option => option.value === platform)?.label ?? platform ?? '—'
let generation = 0
let alive = true
watch([() => authStore.user?.id, () => Boolean(authStore.token)], () => {
  generation++
  selectedKey.value = null
  creating.value = null
  createError.value = false
  creationUnknown.value = false
}, { flush: 'sync' })
onBeforeUnmount(() => { alive = false; generation++ })
async function createKey(group: Group) {
  if (creating.value || creationUnknown.value || !authStore.user || props.groupsLoading || props.groupsError) return
  const owner = generation
  creating.value = group.platform
  createError.value = false
  try {
    const key = await keysAPI.create(`${creationLabel(group.platform)} Key`, group.id)
    if (!alive || owner !== generation) return
    const hydrated = { ...key, group: key.group ?? group }
    selectedKey.value = hydrated
    emit('created', hydrated)
  } catch (error: unknown) {
    if (!alive || owner !== generation) return
    createError.value = true
    const failure = error as { status?: number; response?: { status?: number } }
    const status = failure?.status ?? failure?.response?.status
    creationUnknown.value = !status || status >= 500
  } finally {
    if (alive && owner === generation) creating.value = null
  }
}
</script>

<style scoped>
.overview-key-workspace{display:grid;grid-template-columns:minmax(0,1fr) minmax(17rem,.52fr);gap:3rem;align-items:start;padding-block:1.5rem}
.overview-create,.overview-recent{min-width:0}
.overview-platforms{display:flex;flex-wrap:wrap;gap:.75rem;padding-top:1.5rem}
.overview-platform{display:inline-flex;align-items:center;justify-content:center;gap:.625rem;min-height:48px;padding:.625rem 1rem;border-width:1px;border-style:solid;border-radius:999px;font-weight:500;transition:filter .15s ease,box-shadow .15s ease}
.overview-platform:hover:not(:disabled){filter:brightness(.92);box-shadow:0 0 0 1px currentColor}
.overview-platform:focus-visible{outline:2px solid var(--ritel-focus);outline-offset:3px}
.overview-platform:disabled{opacity:.5;cursor:wait}
.overview-key-row{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:.625rem 0}
.overview-key-use{flex-shrink:0;min-height:44px;padding:.5rem 1rem}
@media(max-width:767px){.overview-key-workspace{grid-template-columns:minmax(0,1fr);gap:1.5rem}.overview-recent{border-top:1px solid var(--ritel-border);padding-top:1.5rem}.overview-platforms{gap:.625rem}}
@media(prefers-reduced-motion:reduce){.overview-platform{transition:none}}
</style>
