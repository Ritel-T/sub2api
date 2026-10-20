<template>
  <header class="ritel-home-header">
    <nav class="ritel-home-nav">
      <a href="https://ritel.si" class="ritel-brand" aria-label="Ritel.SI">
        <img class="ritel-logo" :src="BRAND_LOGO" alt="" width="64" height="50" />
        <span>{{ BRAND_NAME }}</span>
      </a>
      <div class="ritel-home-actions">
        <ThemeSwitcher />
        <LocaleSwitcher />
        <RouterLink v-if="isAuthenticated" :to="backTarget" class="btn btn-secondary">
          {{ t('modelPlaza.nav.backToDashboard') }}
        </RouterLink>
        <RouterLink v-else :to="{ path: '/login', query: { redirect: '/model-plaza' } }" class="btn btn-secondary">
          {{ t('modelPlaza.nav.login') }}
        </RouterLink>
      </div>
    </nav>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import { useAuthStore } from '@/stores/auth'
import { BRAND_LOGO, BRAND_NAME } from '@/utils/branding'

const { t } = useI18n()
const authStore = useAuthStore()

const isAuthenticated = computed(() => authStore.isAuthenticated)
const backTarget = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
</script>
