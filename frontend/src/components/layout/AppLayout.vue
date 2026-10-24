<template>
  <div class="ritel-app" :class="{ 'has-tabs': hasTabs, 'wide-layout': wide }">
    <a href="#console-main" class="console-skip" :inert="appStore.mobileOpen ? true : undefined" @click.prevent="focusMain">{{ skipLabel }}</a>
    <AppSidebar />
    <ConsoleTopBar />
    <ConsoleSectionTabs :inert="appStore.mobileOpen ? true : undefined" />
    <main id="console-main" ref="mainRef" class="ritel-main" :inert="appStore.mobileOpen ? true : undefined" tabindex="-1">
      <ConsolePageTitle v-if="showPageTitle" />
      <slot />
    </main>
    <ConsoleBottomNav :inert="appStore.mobileOpen ? true : undefined" />
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { provideConsoleNav } from '@/navigation/useConsoleNavSource'
import { consoleShellCopy } from '@/utils/consoleShellUi'
import AppSidebar from './AppSidebar.vue'
import ConsoleTopBar from './ConsoleTopBar.vue'
import ConsoleSectionTabs from './ConsoleSectionTabs.vue'
import ConsolePageTitle from './ConsolePageTitle.vue'
import ConsoleBottomNav from './ConsoleBottomNav.vue'

withDefaults(defineProps<{ wide?: boolean; showPageTitle?: boolean }>(), { showPageTitle: true })

const route = useRoute()
const { locale } = useI18n()
const mainRef = ref<HTMLElement | null>(null)
const authStore = useAuthStore()
const appStore = useAppStore()
const onboardingStore = useOnboardingStore()

const consoleNav = provideConsoleNav()
const hasTabs = computed(() => (consoleNav.activeLocation.value?.section.tabs.length ?? 0) > 1)

const skipLabel = computed(() => consoleShellCopy(locale.value).skip)
const isAdmin = computed(() => authStore.user?.role === 'admin')

function focusMain() { mainRef.value?.focus({ preventScroll: true }) }
watch(() => route.path, async () => { await nextTick(); focusMain() })

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: false,
  prepareElement: consoleNav.prepareTourTarget,
  onEnd: consoleNav.finishTour
})

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

</script>

<style scoped>
.wide-layout { --console-content-width: 100%; }
</style>
