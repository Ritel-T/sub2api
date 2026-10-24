import { createApp, watch } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import i18n, { initI18n } from './i18n'
import { useAppStore } from '@/stores/app'
import { updateFavicon, BRAND_NAME, BRAND_FAVICON } from '@/utils/branding'
import { isIOSDevice } from '@/utils/device'
import { initializeTheme, useTheme } from '@/composables/useTheme'
import { getChartTheme } from '@/utils/chartTheme'
import './style.css'
import './styles/open-workspace.css'
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

async function bootstrap() {
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

  await initI18n()

  app.use(router)
  app.use(i18n)

  // 等待路由器完成初始导航后再挂载，避免竞态条件导致的空白渲染
  await router.isReady()
  app.mount('#app')
}

bootstrap()
