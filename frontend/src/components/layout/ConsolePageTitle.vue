<template>
  <AppHeading variant="page" as="h1" class="console-page-title">{{ pageTitle }}</AppHeading>
</template>
<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { useConsoleNav } from '@/navigation/useConsoleNavSource'
const { pageTitle } = useConsoleNav()
</script>
<style scoped>
.console-page-title { margin: 0 0 32px; color: var(--ritel-fg); }
@media (max-width: 767px) { .console-page-title { margin-bottom: 24px; } }
</style>
