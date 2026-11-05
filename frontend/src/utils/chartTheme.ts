import { computed, ref, watch } from 'vue'
import { Chart } from 'chart.js'
import { useMutationObserver } from '@vueuse/core'

/** Resolve theme tokens to real colors before passing them to the canvas renderer. */
export function getChartTheme(isDark: boolean) {
  const fallback = isDark
    ? { surface: '#2b2b27', foreground: '#eeece5', muted: '#b4b1a7', border: '#41413b' }
    : { surface: '#f0eee6', foreground: '#27251f', muted: '#69665c', border: '#d6d1c5' }
  const style = typeof document !== 'undefined' && typeof getComputedStyle !== 'undefined'
    ? getComputedStyle(document.documentElement)
    : null
  const color = (token: string, defaultColor: string): string => {
    const value = style?.getPropertyValue(token).trim()
    // Canvas does not resolve CSS variable references. Missing styles also occur in SSR/tests.
    return value && !value.includes('var(') ? value : defaultColor
  }
  const text = color('--theme-muted', fallback.muted)
  const grid = color('--theme-border', fallback.border)

  return {
    text,
    grid,
    tooltip: {
      backgroundColor: color('--theme-surface-raised', fallback.surface),
      titleColor: color('--theme-foreground', fallback.foreground),
      bodyColor: text,
      footerColor: text,
      borderColor: grid,
      borderWidth: 1
    }
  }
}

/** Keep chart options in sync with the same html class used by the application theme. */
export function useChartTheme() {
  const root = typeof document !== 'undefined' ? document.documentElement : null
  const isDark = ref(root?.classList.contains('dark') ?? false)

  if (root) {
    useMutationObserver(root, () => {
      isDark.value = root.classList.contains('dark')
    }, { attributes: true, attributeFilter: ['class'] })
  }

  const font = root && getComputedStyle(root).getPropertyValue('--ritel-ui').trim()
  if (font) Chart.defaults.font.family = font
  Chart.defaults.font.size = 12
  const theme = computed(() => getChartTheme(isDark.value))
  watch(theme, colors => {
    Chart.defaults.color = colors.text
    Chart.defaults.borderColor = colors.grid
  }, { immediate: true })
  return theme
}
