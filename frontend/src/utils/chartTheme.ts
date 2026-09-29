import { computed, ref } from 'vue'
import { useMutationObserver } from '@vueuse/core'

/** Resolve theme tokens to real colors before passing them to the canvas renderer. */
export function getChartTheme(isDark: boolean) {
  const fallback = isDark
    ? { surface: '#2E2D29', foreground: '#F0EDE7', muted: '#ABA59B', border: '#393833' }
    : { surface: '#FAF8F5', foreground: '#1E1E1C', muted: '#6F6254', border: '#DDD8D0' }
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

  return computed(() => getChartTheme(isDark.value))
}
