<template>
  <span ref="anchor" class="inline-flex">
    <button type="button" class="btn btn-secondary" :aria-label="t('consoleExperience.more')" :aria-expanded="open" aria-haspopup="true" @click.stop="toggle">•••</button>
    <Teleport to="body">
      <div v-if="open" ref="panel" class="more-actions-panel" :style="position" @click="open = false" @keydown.esc.stop="close">
        <slot />
      </div>
    </Teleport>
  </span>
</template>
<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { onClickOutside, onKeyStroke, useEventListener } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
const open = ref(false)
const anchor = ref<HTMLElement>()
const panel = ref<HTMLElement>()
const position = ref({ top: '0px', right: '0px' })
function close() { open.value = false; anchor.value?.querySelector('button')?.focus() }
async function toggle() {
  open.value = !open.value
  if (!open.value) return
  const rect = anchor.value!.getBoundingClientRect()
  position.value = { top: Math.min(rect.bottom + 6, window.innerHeight - 180) + 'px', right: Math.max(12, window.innerWidth - rect.right) + 'px' }
  await nextTick()
  panel.value?.querySelector<HTMLElement>('button, a, input')?.focus()
}
onClickOutside(panel, () => { open.value = false }, { ignore: [anchor] })
onKeyStroke('Escape', () => { if (open.value) close() })
useEventListener(window, 'resize', () => { open.value = false })
</script>
<style>
.more-actions-panel { position: fixed; z-index: 70; display: flex; flex-direction: column; gap: .5rem; min-width: 12rem; max-width: calc(100vw - 24px); padding: .75rem; border: 1px solid var(--ritel-border, #d9d5cc); border-radius: .75rem; background: var(--ritel-bg, #fffdf8); box-shadow: 0 8px 24px #0002; }
.more-actions-panel button { min-width: 44px; min-height: 44px; width: 100%; }
</style>
