<template>
  <div class="relative" ref="containerRef" v-bind="wrapperAttrs()">
    <button
      ref="triggerRef"
      v-bind="fieldAttrs()"
      :id="id"
      :disabled="disabled"
      :aria-label="ariaLabel || undefined"
      :aria-invalid="ariaInvalid()"
      type="button"
      @click="toggle"
      :aria-expanded="isOpen"
      aria-haspopup="dialog"
      :aria-controls="`${inputId}-panel`"
      :class="['date-picker-trigger', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Transition name="date-picker-dropdown">
      <div v-if="isOpen" ref="panelRef" :id="`${inputId}-panel`" role="dialog" :aria-label="displayValue" class="date-picker-dropdown" :style="dropdownStyle">
        <!-- Quick presets -->
        <div class="date-picker-presets">
          <button type="button"
            v-for="preset in presets"
            :key="preset.value"
            @click="selectPreset(preset)"
            :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
          >
            {{ t(preset.labelKey) }}
          </button>
        </div>

        <div class="date-picker-divider"></div>

        <!-- Custom date range inputs -->
        <div class="date-picker-custom">
          <div class="date-picker-field">
            <label :for="`${inputId}-start`" class="date-picker-label">{{ t(enableTime ? 'dates.startTime' : 'dates.startDate') }}</label>
            <input
              :id="`${inputId}-start`"
              :type="enableTime ? 'datetime-local' : 'date'"
              :step="enableTime ? 60 : undefined"
              v-model="startInput"
              :max="endInput || (!enableTime ? tomorrow() : undefined)"
              :aria-invalid="!validRange"
              class="input date-picker-input"
              :aria-describedby="!validRange ? `${inputId}-error` : undefined"
            />
          </div>
          <div class="date-picker-separator">
            <Icon name="arrowRight" size="sm" class="text-gray-400" />
          </div>
          <div class="date-picker-field">
            <label :for="`${inputId}-end`" class="date-picker-label">{{ t(enableTime ? 'dates.endTime' : 'dates.endDate') }}</label>
            <input
              :id="`${inputId}-end`"
              :type="enableTime ? 'datetime-local' : 'date'"
              :step="enableTime ? 60 : undefined"
              v-model="endInput"
              :min="startInput || undefined"
              :max="!enableTime ? tomorrow() : undefined"
              :aria-invalid="!validRange"
              class="input date-picker-input"
              :aria-describedby="!validRange ? `${inputId}-error` : undefined"
            />
          </div>
        </div>

        <p v-if="!validRange" :id="`${inputId}-error`" role="alert" class="px-3 pb-2 text-xs text-red-600">{{ t('dates.invalidRange') }}</p>

        <!-- Apply button -->
        <div class="date-picker-actions">
          <button type="button" @click="apply" :disabled="!validRange" class="date-picker-apply">
            {{ t('dates.apply') }}
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, useId, useAttrs } from 'vue'
import type { CSSProperties, HTMLAttributes } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useOwnedPopover } from '@/composables/overlayLifecycle'
import { formatLocalDate, formatLocalMinute, getDatePresetRange, parseDateBoundary, parseLocalMinute } from '@/utils/dateRange'

defineOptions({ inheritAttrs: false })

interface Props {
  id?: string
  disabled?: boolean
  error?: boolean
  ariaLabel?: string
  startDate: string
  endDate: string
  enableTime?: boolean
  preset?: string | null
  /** Opt in to exact 24-hour windows for date-only consumers. */
  rolling24Hours?: boolean
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (e: 'change', range: { startDate: string; endDate: string; preset: string | null }): void
}

const props = defineProps<Props>()
const attrs = useAttrs()
const ariaInvalid = () => props.error ? true : attrs['aria-invalid'] as HTMLAttributes['aria-invalid']
const wrapperAttrs = () => ({ class: attrs.class, style: attrs.style, 'data-field-role': attrs['data-field-role'], 'data-field-density': attrs['data-field-density'] })
const fieldAttrs = () => Object.fromEntries(Object.entries(attrs).filter(([key]) => !['class', 'style', 'data-field-role', 'data-field-density'].includes(key)))
const emit = defineEmits<Emits>()
const { t, locale } = useI18n()
const inputId = useId()
const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
useOwnedPopover(isOpen, { root: () => panelRef.value, trigger: () => triggerRef.value, close: () => { isOpen.value = false } })
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
const activePreset = ref<string | null>(null)
const dropdownStyle = ref<CSSProperties>({})

const presets = [
  { value: 'today', labelKey: 'dates.today' },
  { value: 'yesterday', labelKey: 'dates.yesterday' },
  { value: 'last24Hours', labelKey: 'dates.last24Hours' },
  { value: '7days', labelKey: 'dates.last7Days' },
  { value: '14days', labelKey: 'dates.last14Days' },
  { value: '30days', labelKey: 'dates.last30Days' },
  { value: 'thisMonth', labelKey: 'dates.thisMonth' },
  { value: 'lastMonth', labelKey: 'dates.lastMonth' }
]

const tomorrow = () => {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return formatLocalDate(d)
}

const presetRange = (preset: string) => {
  if (props.rolling24Hours && preset === 'last24Hours') {
    const end = new Date()
    return { start: new Date(end.getTime() - 86_400_000).toISOString(), end: end.toISOString() }
  }
  const range = getDatePresetRange(preset)!
  // Date-only consumers retain their original API contract.
  if (!props.enableTime && !props.rolling24Hours && preset === 'last24Hours') {
    const now = new Date()
    return { start: formatLocalDate(new Date(now.getTime() - 86400000)), end: formatLocalDate(now) }
  }
  return range
}

const detectPreset = () => {
  if (props.preset !== undefined) return props.preset
  if (props.rolling24Hours && props.startDate.includes('T') && props.endDate.includes('T') && Date.parse(props.endDate) - Date.parse(props.startDate) === 86_400_000) return 'last24Hours'
  // Explicit timestamps are fixed ranges unless the parent identifies a preset.
  if (props.startDate.includes('T') || props.endDate.includes('T')) return null
  return presets.find((p) => {
    if (props.rolling24Hours && p.value === 'last24Hours') return false
    const range = presetRange(p.value)
    return range.start === props.startDate && range.end === props.endDate
  })?.value ?? null
}

const syncDraft = () => {
  localStartDate.value = props.startDate
  localEndDate.value = props.endDate
  activePreset.value = detectPreset()
}

const inputValue = (value: string, end: boolean): string => {
  if (!value) return ''
  if (!props.enableTime) return value.includes('T') ? formatLocalDate(new Date(value)) : value
  // An unfinished/invalid local input must remain invalid rather than normalize.
  if (value.includes('T') && !/(?:[zZ]|[+-]\d{2}:\d{2})$/.test(value)) return value
  const date = parseDateBoundary(value, end)
  return Number.isFinite(date.getTime()) ? formatLocalMinute(date) : ''
}

const editBoundary = (value: string, end: boolean) => {
  const other = end ? localStartDate : localEndDate
  if (!props.enableTime && other.value.includes('T')) other.value = formatLocalDate(new Date(other.value))
  if (props.enableTime && other.value && !other.value.includes('T')) {
    other.value = parseDateBoundary(other.value, !end).toISOString()
  }
  const date = props.enableTime ? parseLocalMinute(value) : null
  ;(end ? localEndDate : localStartDate).value = date ? date.toISOString() : value
  activePreset.value = null
}
const startInput = computed({ get: () => inputValue(localStartDate.value, false), set: (v: string) => editBoundary(v, false) })
const endInput = computed({ get: () => inputValue(localEndDate.value, true), set: (v: string) => editBoundary(v, true) })

const validRange = computed(() => {
  if (!localStartDate.value || !localEndDate.value) return false
  if (props.enableTime && (!parseLocalMinute(startInput.value) || !parseLocalMinute(endInput.value))) return false
  const start = parseDateBoundary(localStartDate.value).getTime()
  const end = parseDateBoundary(localEndDate.value, true).getTime()
  return Number.isFinite(start) && Number.isFinite(end) && start < end
})

const formatDate = (value: string) => {
  const date = parseDateBoundary(value)
  return date.toLocaleString(locale.value === 'zh-Hant-HK' ? 'zh-Hant-HK' : locale.value === 'zh' ? 'zh-CN' : 'en-US', {
    year: 'numeric', month: 'short', day: 'numeric',
    ...(props.enableTime && value.includes('T') ? { hour: '2-digit', minute: '2-digit', hour12: false } : {})
  })
}
const displayValue = computed(() => {
  const preset = presets.find((p) => p.value === detectPreset())
  if (preset) return t(preset.labelKey)
  if (!props.startDate || !props.endDate) return t('dates.selectDateRange')
  return props.startDate === props.endDate ? formatDate(props.startDate) : `${formatDate(props.startDate)} - ${formatDate(props.endDate)}`
})

const isPresetActive = (preset: { value: string }) => activePreset.value === preset.value
const selectPreset = (preset: { value: string }) => {
  const range = presetRange(preset.value)
  localStartDate.value = range.start
  localEndDate.value = range.end
  activePreset.value = preset.value
}
const positionDropdown = () => {
  const rect = containerRef.value?.getBoundingClientRect()
  if (!rect) return
  const width = Math.min(props.enableTime ? 520 : 360, window.innerWidth - 24)
  const below = window.innerHeight - rect.bottom - 16
  const above = rect.top - 16
  const showAbove = below < 320 && above > below
  dropdownStyle.value = {
    width: `${width}px`,
    left: `${Math.max(12, Math.min(rect.left, window.innerWidth - width - 12))}px`,
    ...(showAbove ? { bottom: `${window.innerHeight - rect.top + 8}px` } : { top: `${rect.bottom + 8}px` }),
    maxHeight: `${Math.max(120, showAbove ? above : below)}px`
  }
}
const toggle = () => {
  if (props.disabled) return
  if (!isOpen.value) { syncDraft(); positionDropdown() }
  isOpen.value = !isOpen.value
}
const apply = () => {
  if (!validRange.value) return
  if (activePreset.value) selectPreset({ value: activePreset.value })
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', { startDate: localStartDate.value, endDate: localEndDate.value, preset: activePreset.value })
  isOpen.value = false
}
const handleClickOutside = (event: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) isOpen.value = false
}
watch(() => props.disabled, disabled => { if (disabled) isOpen.value = false })
watch(() => [props.startDate, props.endDate, props.preset], () => { if (!isOpen.value) syncDraft() })
onMounted(() => {
  syncDraft()
  document.addEventListener('click', handleClickOutside)
  window.addEventListener('resize', positionDropdown)
  window.addEventListener('scroll', positionDropdown, true)
})
onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  window.removeEventListener('resize', positionDropdown)
  window.removeEventListener('scroll', positionDropdown, true)
})
</script>

<style scoped>
.date-picker-trigger {
  max-width: 100%;
  @apply flex cursor-pointer items-center gap-2;
}

.date-picker-icon {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-value {
  @apply min-w-0 break-words font-medium;
}

.date-picker-chevron {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-dropdown {
  @apply fixed z-[100];
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
  @apply overflow-y-auto;
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply rounded-md px-3 py-1.5 text-xs font-medium;
  @apply text-gray-600 dark:text-gray-400;
  @apply hover:bg-gray-100 dark:hover:bg-dark-700;
  @apply transition-colors duration-150;
}

.date-picker-preset-active {
  @apply bg-primary-100 dark:bg-primary-900/30;
  @apply text-primary-700 dark:text-primary-300;
}

.date-picker-divider {
  @apply border-t border-gray-100 dark:border-dark-700;
}

.date-picker-custom {
  @apply flex flex-col items-stretch gap-2 p-3 sm:flex-row sm:items-end;
}

.date-picker-field {
  @apply min-w-0 flex-1;
}

.date-picker-label {
  @apply mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400;
}

.date-picker-input { width: 100%; }

.date-picker-input::-webkit-calendar-picker-indicator {
  @apply cursor-pointer opacity-60 hover:opacity-100;
  filter: invert(0.5);
}

.dark .date-picker-input::-webkit-calendar-picker-indicator {
  filter: none;
}

.date-picker-separator {
  @apply hidden items-center justify-center pb-1 sm:flex;
}

.date-picker-actions {
  @apply flex justify-end p-2 pt-0;
}

.date-picker-apply:disabled { opacity: 0.5; cursor: not-allowed; }

.date-picker-apply {
  @apply rounded-lg px-4 py-1.5 text-sm font-medium;
  @apply bg-primary-600 text-white;
  @apply hover:bg-primary-700;
  @apply transition-colors duration-150;
}

/* Dropdown animation */
.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition: all 0.2s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
