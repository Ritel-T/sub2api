<template>
  <div ref="root" class="theme-switcher">
    <button ref="trigger" type="button" class="theme-switcher-trigger"
      :title="`${t('common.theme.label')}: ${t(`common.theme.${preference}`)}`"
      :aria-label="t('common.theme.label')" aria-haspopup="menu" :aria-expanded="open" :aria-controls="menuId"
      @click="toggleMenu" @keydown="triggerKeydown">
      <Icon :name="currentIcon" size="md" aria-hidden="true" />
    </button>
    <Teleport to="body">
      <div v-if="open" :id="menuId" ref="panel" role="menu" :aria-label="t('common.theme.label')"
        class="theme-switcher-menu" :style="position" @keydown="menuKeydown">
        <button v-for="(choice, index) in choices" :key="choice.value" type="button" role="menuitemradio"
          :aria-checked="preference === choice.value" :tabindex="index === activeIndex ? 0 : -1"
          class="theme-switcher-choice" :class="{ 'is-selected': preference === choice.value }"
          @focus="activeIndex = index" @click="selectTheme(choice.value)">
          <Icon :name="choice.icon" size="md" aria-hidden="true" />
          <span>{{ t(`common.theme.${choice.value}`) }}</span>
          <Icon v-if="preference === choice.value" name="check" size="sm" class="theme-switcher-check" aria-hidden="true" />
        </button>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useOwnedPopover } from '@/composables/overlayLifecycle'
import { useTheme, type ThemePreference } from '@/composables/useTheme'

const { t } = useI18n()
const { preference, setTheme } = useTheme()
const choices = [
  { value: 'system', icon: 'monitor' },
  { value: 'light', icon: 'sun' },
  { value: 'dark', icon: 'moon' }
] as const
const currentIcon = computed(() => choices.find(choice => choice.value === preference.value)?.icon ?? 'monitor')
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const open = ref(false)
const activeIndex = ref(0)
const menuId = `theme-menu-${useId()}`
const position = ref<Record<string, string>>({})
const close = () => { open.value = false }
const ownedPopover = useOwnedPopover(open, { root: () => panel.value, trigger: () => trigger.value, close })
onClickOutside(root, event => { if (!panel.value?.contains(event.target as Node)) close() })

function positionMenu() {
  const rect = trigger.value?.getBoundingClientRect()
  if (!rect) return
  const width = Math.min(176, window.innerWidth - 16)
  position.value = { top: `${rect.bottom + 8}px`, left: `${Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8))}px`, width: `${width}px` }
}
useEventListener(window, 'resize', () => { if (open.value) positionMenu() })
function focusChoice(index: number) {
  activeIndex.value = (index + choices.length) % choices.length
  void nextTick(() => { panel.value?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]')[activeIndex.value]?.focus() })
}
async function openMenu(index = choices.findIndex(choice => choice.value === preference.value)) {
  activeIndex.value = Math.max(0, index)
  positionMenu()
  open.value = true
  await nextTick()
  focusChoice(activeIndex.value)
}
function toggleMenu() { if (open.value) close(); else void openMenu() }
function triggerKeydown(event: KeyboardEvent) {
  if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
    event.preventDefault()
    void openMenu(event.key === 'Home' ? 0 : event.key === 'End' || event.key === 'ArrowUp' ? choices.length - 1 : undefined)
  }
}
function menuKeydown(event: KeyboardEvent) {
  if (event.key === 'Tab') { ownedPopover.tab(event); close(); return }
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); trigger.value?.focus(); return }
  const movement = event.key === 'ArrowDown' ? activeIndex.value + 1 : event.key === 'ArrowUp' ? activeIndex.value - 1
    : event.key === 'Home' ? 0 : event.key === 'End' ? choices.length - 1 : null
  if (movement !== null) { event.preventDefault(); focusChoice(movement) }
}
function selectTheme(value: ThemePreference) {
  if (preference.value !== value) setTheme(value)
  close()
  trigger.value?.focus()
}
</script>

<style scoped>
.theme-switcher{display:inline-flex;flex:0 0 36px;width:36px;height:36px}
.theme-switcher-trigger{display:inline-flex;align-items:center;justify-content:center;width:36px;height:36px;border:0;border-radius:var(--ritel-radius-control);background:transparent;color:var(--ritel-muted);cursor:pointer}
.theme-switcher-trigger:hover{color:var(--ritel-fg);background:var(--theme-hover)}
.theme-switcher-trigger:focus-visible{outline:2px solid var(--ritel-focus);outline-offset:2px}
.theme-switcher-menu{position:fixed;z-index:100000100;padding:4px;border:1px solid var(--ritel-border);border-radius:var(--ritel-radius-menu);background:var(--ritel-surface-raised);color:var(--ritel-fg);box-shadow:0 10px 30px rgba(var(--theme-shadow-rgb),.18)}
.theme-switcher-choice{display:flex;align-items:center;gap:10px;width:100%;min-height:40px;padding:8px 10px;border:0;border-radius:var(--ritel-radius-control);background:transparent;font:inherit;font-size:14px;text-align:left;color:var(--ritel-muted);cursor:pointer}
.theme-switcher-choice:hover,.theme-switcher-choice:focus-visible{background:var(--theme-hover);color:var(--ritel-fg)}
.theme-switcher-choice.is-selected{color:var(--ritel-fg);background:var(--theme-selected)}
.theme-switcher-check{margin-left:auto}
</style>
