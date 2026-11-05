<template>
  <section class="space-y-4 border-t border-gray-100 pt-5 dark:border-dark-700" aria-labelledby="quick-key-defaults-title">
    <div>
      <AppHeading id="quick-key-defaults-title" as="h3" variant="subsection">{{ copy.title }}</AppHeading>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ copy.hint }}</p>
    </div>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div v-else-if="failed" role="alert" class="flex items-center gap-3">
      <span>{{ copy.failed }}</span><button type="button" class="btn btn-secondary" @click="loadGroups">{{ t('common.retry') }}</button>
    </div>
    <div v-else class="grid gap-4 sm:grid-cols-2">
      <label v-for="bucket in buckets" :key="bucket.value" class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
        <span>{{ bucket.label }}</span>
        <select class="input" :data-bucket="bucket.value" :value="modelValue[bucket.value] ?? ''" @change="update(bucket.value, $event)">
          <option value="">{{ copy.automatic }}</option>
          <option v-if="missing(bucket.value)" :value="modelValue[bucket.value]" disabled>{{ copy.unavailable }} · #{{ modelValue[bucket.value] }}</option>
          <option v-for="group in eligible(bucket.value)" :key="group.id" :value="group.id">{{ group.name }} · #{{ group.id }}</option>
        </select>
      </label>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppHeading from '@/components/common/AppHeading.vue'
import { getAll } from '@/api/admin/groups'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import { quickKeyPlatformBucket } from '@/utils/quickKeyGroups'
import type { AdminGroup } from '@/types'

const props = defineProps<{ modelValue: Record<string, number> }>()
const emit = defineEmits<{ 'update:modelValue': [value: Record<string, number>] }>()
const { t, locale } = useI18n()
const copy = computed(() => locale.value.startsWith('zh') ? {
  title: '首页快捷创建 Key 默认分组', hint: '按平台保存分组 ID。国模共用一个默认组；仅在用户有权限且分组启用时使用，否则选择该平台首个可用组。留空使用相同回退规则，随本页设置一起保存。',
  automatic: '首个可用分组', unavailable: '原配置组已不可用', failed: '分组列表加载失败'
} : {
  title: 'Quick key default groups', hint: 'Save group IDs per platform. Domestic models share one default. Defaults apply only to active groups the user can access; otherwise the first available group is selected. Leave empty for the same fallback. Save with this settings page.',
  automatic: 'First available group', unavailable: 'Configured group unavailable', failed: 'Unable to load groups'
})
const groups = ref<AdminGroup[]>([])
const loading = ref(true)
const failed = ref(false)
const buckets = computed(() => {
  const result = new Map<string, string>()
  for (const platform of GROUP_PLATFORM_OPTIONS) {
    const value = quickKeyPlatformBucket(platform.value)
    if (!result.has(value)) result.set(value, value === 'domestic' ? (locale.value.startsWith('zh') ? '国模' : 'Domestic models') : platform.label)
  }
  return [...result].map(([value, label]) => ({ value, label }))
})
const eligible = (bucket: string) => groups.value.filter(group => group.status === 'active' && quickKeyPlatformBucket(group.platform) === bucket)
const missing = (bucket: string) => props.modelValue[bucket] !== undefined && !eligible(bucket).some(group => group.id === props.modelValue[bucket])
function update(bucket: string, event: Event) {
  const next = { ...props.modelValue }
  const value = (event.target as HTMLSelectElement).value
  if (value) next[bucket] = Number(value)
  else delete next[bucket]
  emit('update:modelValue', next)
}
async function loadGroups() {
  loading.value = true; failed.value = false
  try { groups.value = await getAll() }
  catch { failed.value = true }
  finally { loading.value = false }
}
onMounted(loadGroups)
</script>
