<template>
  <nav class="flex flex-wrap items-center gap-2" :aria-label="`${kind === 'user' ? t('admin.users.columns.user') : t('priorityScheduling.account')} #${id}`">
    <span v-if="context" class="text-sm font-medium">{{ kind === 'user' ? t('admin.users.columns.user') : t('priorityScheduling.account') }} #{{ id }}</span>
    <RouterLink v-for="link in links" :key="link.path" :to="{ path: link.path, query: { [kind === 'user' ? 'user_id' : 'account_id']: id } }" class="btn btn-secondary text-sm" @click="$emit('navigate')">{{ t(link.label) }}</RouterLink>
    <RouterLink v-if="auth.isAdmin && kind === 'account' && proxyId" :to="{ path: '/admin/proxies', query: { proxy_id: proxyId } }" class="btn btn-secondary text-sm" @click="$emit('navigate')">{{ t('nav.proxies') }}</RouterLink>
    <button v-if="context" type="button" class="btn btn-secondary text-sm" @click="clearContext">{{ t('common.clear') }}</button>
  </nav>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useMerchantTestPaymentAccess } from '@/composables/useMerchantTestPaymentAccess'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
const props = defineProps<{ kind: 'user' | 'account'; id: number; proxyId?: number | null; context?: boolean }>()
defineEmits<{ navigate: [] }>()
const { t } = useI18n(), route = useRoute(), router = useRouter(), auth = useAuthStore(), app = useAppStore(), settings = useAdminSettingsStore()
const { merchantTestPaymentAccess } = useMerchantTestPaymentAccess()
const paymentAvailable = computed(() => app.cachedPublicSettings?.payment_enabled === false ? merchantTestPaymentAccess.value : settings.paymentEnabled || merchantTestPaymentAccess.value)
const links = computed(() => {
  if (!auth.isAdmin) return []
  const items = props.kind === 'user'
    ? [{ path: '/admin/usage', label: 'nav.usage' }, ...(!auth.isSimpleMode && isFeatureFlagEnabled(FeatureFlags.subscription) ? [{ path: '/admin/subscriptions', label: 'nav.subscriptions' }] : []), ...(paymentAvailable.value ? [{ path: '/admin/orders', label: 'payment.admin.orders' }] : [])]
    : [{ path: '/admin/usage', label: 'nav.usage' }, { path: '/admin/account-quality', label: 'qualityOps.title' }, { path: '/admin/priority-scheduling', label: 'priorityScheduling.title' }, { path: '/admin/account-ops', label: 'accountOps.title' }]
  return items.filter(item => item.path !== route?.path)
})
function clearContext() {
  const query = { ...route.query }
  delete query[props.kind === 'user' ? 'user_id' : 'account_id']
  void router.replace({ path: route.path, query, hash: route.hash })
}
</script>
