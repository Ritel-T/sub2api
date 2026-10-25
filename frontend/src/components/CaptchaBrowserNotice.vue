<template>
  <div
    v-if="showNotice"
    role="alert"
    class="mb-4 flex items-start gap-3 rounded-xl border border-amber-300 bg-amber-50 p-4 text-amber-950 dark:border-amber-600/60 dark:bg-amber-950/40 dark:text-amber-100"
  >
    <Icon name="exclamationCircle" size="md" class="mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" />
    <div class="min-w-0 space-y-1">
      <p class="text-base font-semibold leading-6">{{ t('auth.captchaBrowserNoticeTitle') }}</p>
      <p class="text-sm leading-6">{{ t('auth.captchaBrowserNoticeDescription') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { shouldRecommendCaptchaBrowser } from '@/utils/captchaBrowser'

const { t } = useI18n()
const showNotice = typeof navigator !== 'undefined' && shouldRecommendCaptchaBrowser(navigator.userAgent)
</script>
