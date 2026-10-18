<template>
  <header class="space-y-4 border-b border-gray-200 pb-5 dark:border-dark-700">
    <h1 class="text-3xl font-semibold" data-testid="billing-page-title">{{ title || bt(`billing.${current}`) }}</h1>
    <nav v-if="destinations.length > 1" class="flex flex-wrap gap-x-6 gap-y-3" :aria-label="bt('billing.title')" data-testid="billing-navigation">
      <router-link v-for="destination in destinations" :key="destination" :to="destination === 'purchase' ? '/purchase' : `/${destination}`" :aria-current="current === destination ? 'page' : undefined"
        class="border-b-2 pb-2 text-sm transition-colors" :class="current === destination ? 'border-current text-gray-900 dark:text-white' : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'">{{ bt(`billing.${destination}`) }}</router-link>
    </nav>
  </header>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useMerchantTestPaymentAccess } from '@/composables/useMerchantTestPaymentAccess'
import { useI18n } from 'vue-i18n'
import type { PublicSettings } from '@/types'
import { billingDestinations, billingMessages, type BillingDestination } from './billingNavigation'
const props = defineProps<{ current: BillingDestination; title?: string; settings?: Partial<PublicSettings> | null; simpleMode?: boolean; merchantTestAccess?: boolean }>()
const { t: bt } = useI18n({ useScope: 'local', messages: billingMessages })
const authStore = useAuthStore()
const { merchantTestPaymentAccess } = useMerchantTestPaymentAccess()
const destinations = computed(() => billingDestinations({ ...props, simpleMode: props.simpleMode ?? authStore.isSimpleMode, merchantTestAccess: props.merchantTestAccess || merchantTestPaymentAccess.value }))
</script>
