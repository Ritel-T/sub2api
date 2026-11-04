<template>
  <span class="ccs-app-glyph" aria-hidden="true">
    <img v-if="brand === 'hermes'" :src="hermes" alt="" class="ccs-app-mark" />
    <span v-else class="ccs-app-mark" :class="{ 'ccs-app-mark-monochrome': brand === 'opencode' }" :style="{ color: colors[brand] }" v-html="svgIcons[brand]"></span>
    <span v-if="app === 'claude' || app === 'claude-desktop'" class="ccs-app-badge">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
        <template v-if="app === 'claude'">
          <path d="M12 19h8" /><path d="m4 17 6-6-6-6" />
        </template>
        <template v-else>
          <rect width="20" height="14" x="2" y="3" rx="2" />
          <path d="M8 21h8M12 17v4" />
        </template>
      </svg>
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import openai from '@/assets/ccs-apps/openai.svg?raw'
import claude from '@/assets/ccs-apps/claude.svg?raw'
import gemini from '@/assets/ccs-apps/gemini.svg?raw'
import opencode from '@/assets/ccs-apps/opencode.svg?raw'
import openclaw from '@/assets/ccs-apps/openclaw.svg?raw'
import grok from '@/assets/ccs-apps/grok.svg?raw'
import pi from '@/assets/ccs-apps/pi.svg?raw'
import deepseek from '@/assets/ccs-apps/deepseek.svg?raw'
import hermes from '@/assets/ccs-apps/hermes.png'

const props = defineProps<{ app: string }>()
const brands: Record<string, string> = { codex: 'openai', 'codex-ws': 'openai', 'claude-desktop': 'claude', dsh: 'deepseek' }
const brand = computed(() => brands[props.app] || props.app)
// Static, reviewed SVGs copied from CCS. No remote or user-supplied markup.
const svgIcons: Record<string, string> = { openai, claude, gemini, opencode, openclaw, grok, pi, deepseek }
const colors: Record<string, string> = { claude: '#D4915D', gemini: '#4285F4', openclaw: '#ff4f40', deepseek: '#1E88E5' }
</script>

<style scoped>
.ccs-app-glyph { position: relative; display: inline-flex; flex: none; align-items: center; justify-content: center; width: 18px; height: 18px; font-size: 16px; line-height: 1; }
.ccs-app-mark { display: inline-flex; align-items: center; justify-content: center; width: 16px; height: 16px; object-fit: contain; }
.ccs-app-badge { position: absolute; bottom: -4px; right: -4px; display: flex; align-items: center; justify-content: center; width: 11px; height: 11px; border-radius: 3px; background: var(--theme-surface); color: var(--ritel-muted); }
.ccs-app-badge svg { width: 9px; height: 9px; }
.dark .ccs-app-mark-monochrome { filter: invert(1); }
</style>