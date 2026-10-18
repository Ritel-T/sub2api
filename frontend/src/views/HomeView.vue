<template>
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe v-if="isHomeContentUrl" :src="homeContent.trim()" class="h-screen w-full border-0" allowfullscreen :title="BRAND_NAME"></iframe>
    <div v-else v-html="homeContent"></div>
  </div>
  <div v-else class="ritel-home" :class="{ 'ritel-home-compact': compactHomeEnabled }" :data-testid="compactHomeEnabled ? 'compact-home' : undefined">
    <header class="ritel-home-header">
      <nav class="ritel-home-nav">
        <a href="https://ritel.si" class="ritel-brand" aria-label="Ritel.SI">
          <img :src="BRAND_LOGO" alt="" width="64" height="50" />
          <span>{{ BRAND_NAME }}</span>
        </a>
        <div class="ritel-home-actions">
          <LocaleSwitcher />
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="ritel-nav-link">{{ t('nav.docs') }}</a>
          <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="ritel-nav-link">{{ t('nav.modelPlaza') }}</router-link>
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="btn btn-secondary">{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
        </div>
      </nav>
    </header>
    <main class="ritel-home-main">
      <section class="ritel-home-hero">
        <img :src="BRAND_LOGO" alt="" class="ritel-home-symbol" width="180" height="142" />
        <h1>{{ BRAND_NAME }}</h1>
        <p>{{ siteSubtitle }}</p>
        <div class="ritel-home-cta">
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="btn btn-primary">{{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}<Icon name="arrowRight" size="md" /></router-link>
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="btn btn-secondary">{{ t('home.viewDocs') }}</a>
        </div>
      </section>
      <section v-if="!compactHomeEnabled" class="terminal-container ritel-home-endpoint" aria-label="API endpoint">
        <span>API</span>
        <code>{{ apiBaseUrl }}</code>
        <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="ritel-nav-link">{{ t('nav.modelPlaza') }}<Icon name="arrowRight" size="sm" /></router-link>
      </section>
    </main>
    <footer class="ritel-home-footer">&copy; {{ currentYear }} {{ BRAND_NAME }}</footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import { BRAND_LOGO, BRAND_NAME } from '@/utils/branding'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const apiBaseUrl = computed(() => appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl || 'https://api.ritel.si/v1')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const isHomeContentUrl = computed(() => /^https?:\/\//.test(homeContent.value.trim()))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const showModelPlazaEntry = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza) && (isAuthenticated.value || appStore.cachedPublicSettings?.model_plaza_require_auth !== true))
const currentYear = new Date().getFullYear()

onMounted(() => {
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
})
</script>
