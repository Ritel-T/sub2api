<template>
  <section class="card p-6" aria-labelledby="continue-title">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h2 id="continue-title" class="text-xl font-semibold">{{ copy.continue }}</h2>
        <p v-if="activeKey" class="mt-2 text-gray-500 dark:text-gray-400">{{ activeKey.name }}</p>
      </div>
      <div v-if="loading" role="status">{{ copy.loading }}</div>
      <div v-else-if="error" class="flex items-center gap-3" role="alert">
        <span>{{ copy.loadFailed }}</span>
        <button type="button" class="btn btn-secondary" @click="$emit('retry')">{{ copy.retry }}</button>
      </div>
      <router-link v-else-if="activeKey" :to="{ path: '/keys', query: { connect: '1', key_id: String(activeKey.id) } }" class="btn btn-primary">{{ copy.connect }}</router-link>
      <router-link v-else-if="totalKeys === 0" to="/keys?create=1" class="btn btn-primary">{{ copy.create }}</router-link>
      <router-link v-else to="/keys" class="btn btn-primary">{{ copy.manageKeys }}</router-link>
    </div>
  </section>
</template>
<script setup lang="ts">
import type { ApiKey } from '@/types'
import { useOverviewCopy } from './overviewCopy'
defineProps<{ activeKey: ApiKey | null; totalKeys: number | null; loading: boolean; error: boolean }>()
defineEmits<{ retry: [] }>()
const copy = useOverviewCopy()
</script>
