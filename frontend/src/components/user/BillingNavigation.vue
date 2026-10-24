<template>
  <header class="border-b border-gray-200 pb-5 dark:border-dark-700">
    <AppHeading as="h1" variant="page"  data-testid="billing-page-title">{{ title || bt(`billing.${current}`) }}</AppHeading>
  </header>
</template>
<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { useI18n } from 'vue-i18n'
import type { PublicSettings } from '@/types'
import { billingMessages, type BillingDestination } from './billingNavigation'
// The console section tabs switch between billing destinations; the remaining
// props are still accepted so existing pages mount unchanged.
defineProps<{ current: BillingDestination; title?: string; settings?: Partial<PublicSettings> | null; simpleMode?: boolean; merchantTestAccess?: boolean }>()
const { t: bt } = useI18n({ useScope: 'local', messages: billingMessages })
</script>
