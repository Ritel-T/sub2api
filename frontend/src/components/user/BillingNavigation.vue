<template>
  <header class="border-b border-gray-200 pb-5 dark:border-dark-700">
    <h1 class="text-3xl font-semibold" data-testid="billing-page-title">{{ title || bt(`billing.${current}`) }}</h1>
  </header>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PublicSettings } from '@/types'
import { billingMessages, type BillingDestination } from './billingNavigation'
// The console section tabs switch between billing destinations; the remaining
// props are still accepted so existing pages mount unchanged.
defineProps<{ current: BillingDestination; title?: string; settings?: Partial<PublicSettings> | null; simpleMode?: boolean; merchantTestAccess?: boolean }>()
const { t: bt } = useI18n({ useScope: 'local', messages: billingMessages })
</script>
