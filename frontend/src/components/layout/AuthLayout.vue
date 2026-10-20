<template>
  <div class="ritel-auth">
    <header class="ritel-auth-header">
      <a href="https://ritel.si" class="ritel-brand" aria-label="Ritel.SI">
        <img class="ritel-logo" :src="BRAND_LOGO" alt="" width="64" height="50" />
        <span>{{ BRAND_NAME }}</span>
      </a>
      <div class="flex items-center gap-1">
        <ThemeSwitcher />
        <LocaleSwitcher />
      </div>
    </header>
    <main class="ritel-auth-main">
      <div class="ritel-auth-form">
        <slot />
        <div class="ritel-auth-footer"><slot name="footer" /></div>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import { BRAND_LOGO, BRAND_NAME } from '@/utils/branding'
const appStore = useAppStore()
onMounted(() => appStore.fetchPublicSettings())
</script>
