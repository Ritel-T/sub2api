import { consoleUiMessages } from '@/i18n/consoleCopy'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

export function useOverviewCopy() {
  const { locale } = useI18n()
  return computed(() => consoleUiMessages(locale.value).dashboard)
}
