import { redirectConsoleAlias, centralAccountEnabled } from '@/utils/accountConfig'
import { useAuthStore } from '@/stores/auth'
import { createApp, watch } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { getLocale, initI18n } from './i18n'
import { useAppStore } from '@/stores/app'
import { updateFavicon, BRAND_NAME, BRAND_FAVICON } from '@/utils/branding'
import { isIOSDevice } from '@/utils/device'
import { initializeTheme, useTheme } from '@/composables/useTheme'
import { getChartTheme } from '@/utils/chartTheme'
import './style.css'
import './styles/open-workspace.css'
import './styles/startup.css'
import { createStartupRecovery } from '@/utils/startupRecovery'
import { Chart } from 'chart.js'

const chartFont = getComputedStyle(document.documentElement).getPropertyValue('--ritel-ui').trim()
if (chartFont) Chart.defaults.font.family = chartFont
Chart.defaults.font.size = 12

function initIOSViewportZoomFix() {
  // iOS Safari 在输入框字号小于 16px 时聚焦会自动放大页面，且失焦后不会恢复。
  // 限制 maximum-scale 可阻止该行为；iOS 10+ 用户仍可双指手动缩放，不影响可访问性。
  // 仅在 iOS 设备上注入，避免影响 Android Chrome 的手动缩放能力。
  if (!isIOSDevice()) return

  const viewport = document.querySelector('meta[name="viewport"]')
  if (!viewport) return

  const content = viewport.getAttribute('content') || ''
  if (/maximum-scale/i.test(content)) return
  viewport.setAttribute('content', `${content}, maximum-scale=1.0`)
}

function setupApplication() {
  // Apply theme class globally before app mount to keep all routes consistent.
  initializeTheme()
  const { resolvedTheme } = useTheme()
  watch(resolvedTheme, theme => {
    const colors = getChartTheme(theme === 'dark')
    Chart.defaults.color = colors.text
    Chart.defaults.borderColor = colors.grid
  }, { immediate: true })
  initIOSViewportZoomFix()

  const app = createApp(App)
  const pinia = createPinia()
  app.use(pinia)

  // Initialize settings from injected config BEFORE mounting (prevents flash)
  // This must happen after pinia is installed but before router and i18n
  const appStore = useAppStore()
  appStore.initFromInjectedConfig()

  // Set document title immediately after config is loaded
  document.title = BRAND_NAME
  updateFavicon(BRAND_FAVICON)

  // Everything above is installed once. Only read-only initialization and the
  // initial navigation below can be resumed after a temporary connection failure.
  let localeReady = false
  let routerInstalled = false
  let routeReady = false
  let mounted = false

  return async (whenActive: () => Promise<void>) => {
    if (mounted) return
    await whenActive()
    if (!localeReady) {
      await initI18n()
      app.use(i18n)
      localeReady = true
    }

    await whenActive()
    // Central auth owns its bounded read-only retries and hydration single flight.
    // Do not wrap this in another automatic retry loop.
    if (centralAccountEnabled) await useAuthStore(pinia).initializeCentralAuth()

    await whenActive()
    if (!routerInstalled) {
      routerInstalled = true
      app.use(router)
      await router.isReady()
      routeReady = true
    } else if (!routeReady) {
      // isReady() alone would wait forever after a rejected first navigation.
      // Retry that navigation on the same URL, keeping the router installation.
      const failure = await router.replace(router.options.history.location)
      if (failure) throw failure
      await router.isReady()
      routeReady = true
    }

    await whenActive()
    app.mount('#app')
    mounted = true
  }
}

if (!redirectConsoleAlias()) {
  // Paint the neutral shell before any async session/locale initialization.
  // Defer setup to the first attempt so synchronous setup failures also reach it.
  let setup: Promise<ReturnType<typeof setupApplication>> | undefined
  const root = document.getElementById('app')
  if (root) {
    const recovery = createStartupRecovery({
      root,
      locale: getLocale(),
      finish: async whenActive => {
        setup ??= Promise.resolve().then(setupApplication)
        const finish = await setup
        await finish(whenActive)
      }
    })
    void recovery.retry()
  }
}
