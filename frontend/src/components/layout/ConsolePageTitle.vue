<template>
  <h1
    v-if="visible"
    class="console-page-title"
  >{{ pageTitle }}</h1>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'

const { pageTitle } = useConsoleNav()

const visible = ref(true)
let observer: MutationObserver | null = null

function checkForH1(root: HTMLElement) {
  visible.value = !root.querySelector('h1:not(.console-page-title)')
}

onMounted(() => {
  const main = document.getElementById('console-main')
  if (!main) return
  checkForH1(main)
  observer = new MutationObserver(() => checkForH1(main))
  observer.observe(main, { childList: true, subtree: true })
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})
</script>

<style scoped>
.console-page-title {
  font-family: var(--ritel-display);
  font-size: 36px;
  font-weight: 600;
  line-height: 1.15;
  letter-spacing: -0.01em;
  margin: 0 0 32px;
  color: var(--ritel-fg);
  font-variant-numeric: lining-nums proportional-nums;
}
@media (max-width: 767px) {
  .console-page-title { font-size: 28px; margin-bottom: 24px; }
}
</style>
