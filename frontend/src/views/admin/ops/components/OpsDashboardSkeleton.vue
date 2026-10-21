<script setup lang="ts">
interface Props {
  fullscreen?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  fullscreen: false
})
</script>

<template>
  <div class="space-y-6">
    <!-- Header (matches OpsDashboardHeader + overview blocks) -->
    <div :class="['rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700', props.fullscreen ? 'p-8' : 'p-6']">
      <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 pb-4 dark:border-dark-700">
        <div class="space-y-2">
          <div class="h-6 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-3 w-80 max-w-full animate-pulse rounded bg-gray-100 dark:bg-dark-700/70"></div>
        </div>
        <div v-if="!props.fullscreen" class="flex flex-wrap items-center gap-3">
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-[140px] animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-[160px] animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-[150px] animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-9 animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-28 animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-28 animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-9 animate-pulse"></div>
        </div>
      </div>

      <div class="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-12">
        <div class="open-section lg:col-span-5">
          <div class="grid h-full grid-cols-1 gap-6 md:grid-cols-[200px_1fr] md:items-center">
            <div class="bg-gray-200 dark:bg-dark-700 h-28 animate-pulse"></div>
            <div class="space-y-4">
              <div class="h-4 w-32 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
              <div class="grid grid-cols-2 gap-3">
                <div v-for="i in 4" :key="i" class="bg-gray-200 dark:bg-dark-700 h-14 animate-pulse"></div>
              </div>
            </div>
          </div>
        </div>

        <div class="lg:col-span-7">
          <div class="grid h-full grid-cols-1 content-center gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="i in 7" :key="i" class="bg-gray-200 dark:bg-dark-700 h-20 animate-pulse"></div>
          </div>
        </div>
      </div>
    </div>

    <!-- Row: Concurrency + Throughput (matches OpsDashboard.vue) -->
    <div class="grid grid-cols-1 gap-6 lg:grid-cols-4">
      <div :class="['min-h-[360px] rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 lg:col-span-1', props.fullscreen ? 'p-8' : 'p-6']">
        <div class="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="bg-gray-200 dark:bg-dark-700 mt-6 h-72 animate-pulse"></div>
      </div>
      <div :class="['min-h-[360px] rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 lg:col-span-1', props.fullscreen ? 'p-8' : 'p-6']">
        <div class="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="bg-gray-200 dark:bg-dark-700 mt-6 h-72 animate-pulse"></div>
      </div>
      <div :class="['min-h-[360px] rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 lg:col-span-2', props.fullscreen ? 'p-8' : 'p-6']">
        <div class="h-4 w-56 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="bg-gray-200 dark:bg-dark-700 mt-6 h-72 animate-pulse"></div>
      </div>
    </div>

    <!-- Row: Visual Analysis (baseline 3-up grid) -->
    <div class="grid grid-cols-1 gap-6 md:grid-cols-3">
      <div
        v-for="i in 3"
        :key="i"
        :class="['rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700', props.fullscreen ? 'p-8' : 'p-6']"
      >
        <div class="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="bg-gray-200 dark:bg-dark-700 mt-6 h-56 animate-pulse"></div>
      </div>
    </div>

    <!-- Alert Events -->
    <div :class="['rounded-3xl bg-white shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700', props.fullscreen ? 'p-8' : 'p-6']">
      <div class="flex flex-wrap items-center justify-between gap-4">
        <div class="h-4 w-48 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div v-if="!props.fullscreen" class="flex flex-wrap items-center gap-2">
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-[140px] animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-[120px] animate-pulse"></div>
          <div class="bg-gray-200 dark:bg-dark-700 h-9 w-[120px] animate-pulse"></div>
        </div>
      </div>

      <div class="mt-6 space-y-3">
        <div v-for="i in 6" :key="i" class="open-section flex items-center justify-between gap-4">
          <div class="flex-1 space-y-2">
            <div class="h-3 w-56 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-80 max-w-full animate-pulse rounded bg-gray-100 dark:bg-dark-700/70"></div>
          </div>
          <div class="bg-gray-200 dark:bg-dark-700 h-7 w-20 animate-pulse"></div>
        </div>
      </div>
    </div>
  </div>
</template>
