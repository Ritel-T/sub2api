<template>
  <div>
    <button type="button" @click="openModal" class="relative flex h-9 w-9 items-center justify-center rounded-lg text-gray-600 transition-colors hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-800" :aria-label="t('announcements.title')">
      <Icon name="bell" size="md" />
      <span v-if="unreadCount > 0" class="absolute right-1 top-1 h-2 w-2 rounded-full bg-red-500"></span>
    </button>
    <BaseDialog :show="isModalOpen" :title="t('announcements.title')" width="wide" :z-index="100" :close-on-click-outside="true" @close="closeModal">
      <div v-if="unreadCount > 0" class="mb-4 flex items-center justify-between gap-3 text-sm text-gray-500 dark:text-gray-400">
        <span>{{ unreadCount }} {{ t('announcements.unread') }}</span>
        <button type="button" @click="markAllAsRead" :disabled="loading" class="btn btn-secondary btn-sm">{{ t('announcements.markAllRead') }}</button>
      </div>
      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <div v-else-if="announcements.length > 0">
        <button v-for="item in announcements" :key="item.id" type="button" class="group relative flex w-full items-center gap-4 border-b border-gray-200 py-4 text-left transition-colors hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/30" @click="openDetail(item)">
          <span class="h-2 w-2 shrink-0 rounded-full" :class="item.read_at ? 'bg-gray-300 dark:bg-dark-600' : 'bg-primary-500'"></span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.title }}</span>
            <span class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400"><time>{{ formatRelativeTime(item.created_at) }}</time><span v-if="!item.read_at">{{ t('announcements.unread') }}</span></span>
          </span>
          <Icon name="chevronRight" size="sm" />
        </button>
      </div>
      <EmptyState v-else :title="t('announcements.empty')" :description="t('announcements.emptyDescription')" />
    </BaseDialog>
    <BaseDialog :show="detailModalOpen && !!selectedAnnouncement" :title="selectedAnnouncement?.title || t('announcements.title')" width="wide" :z-index="110" :close-on-click-outside="true" @close="closeDetail">
      <template v-if="selectedAnnouncement">
        <div class="mb-4 flex flex-wrap items-center gap-3 text-sm text-gray-500 dark:text-gray-400"><time>{{ formatRelativeWithDateTime(selectedAnnouncement.created_at) }}</time><span>{{ selectedAnnouncement.read_at ? t('announcements.read') : t('announcements.unread') }}</span></div>
        <div class="markdown-body prose prose-sm max-w-none dark:prose-invert" v-html="renderMarkdown(selectedAnnouncement.content)"></div>
      </template>
      <template #footer>
        <div v-if="selectedAnnouncement" class="flex w-full flex-wrap items-center justify-between gap-3">
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ selectedAnnouncement.read_at ? t('announcements.readStatus') : t('announcements.markReadHint') }}</span>
          <div class="flex flex-wrap gap-3"><button type="button" class="btn btn-secondary" @click="closeDetail">{{ t('common.close') }}</button><button v-if="!selectedAnnouncement.read_at" type="button" class="btn btn-primary" @click="markAsReadAndClose(selectedAnnouncement.id)">{{ t('announcements.markRead') }}</button></div>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import BaseDialog from './BaseDialog.vue'
import EmptyState from './EmptyState.vue'
import LoadingSpinner from './LoadingSpinner.vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAppStore } from '@/stores/app'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeTime, formatRelativeWithDateTime } from '@/utils/format'
import type { UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import '@/styles/announcement-markdown.css'

const { t } = useI18n()
const appStore = useAppStore()
const announcementStore = useAnnouncementStore()

// Configure marked
marked.setOptions({
  breaks: true,
  gfm: true,
})

// Use store state (storeToRefs for reactivity)
const { announcements, loading } = storeToRefs(announcementStore)
const unreadCount = computed(() => announcementStore.unreadCount)

// Local modal state
const isModalOpen = ref(false)
const detailModalOpen = ref(false)
const selectedAnnouncement = ref<UserAnnouncement | null>(null)

// Methods
function renderMarkdown(content: string): string {
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
}

function openModal() {
  isModalOpen.value = true
}

function closeModal() {
  isModalOpen.value = false
}

function openDetail(announcement: UserAnnouncement) {
  selectedAnnouncement.value = announcement
  detailModalOpen.value = true
  if (!announcement.read_at) {
    markAsRead(announcement.id)
  }
}

function closeDetail() {
  detailModalOpen.value = false
  selectedAnnouncement.value = null
}

async function markAsRead(id: number) {
  try {
    const marked = await announcementStore.markAsRead(id)
    if (!marked) appStore.showError(t('common.unknownError'))
    return marked
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
    return false
  }
}

async function markAsReadAndClose(id: number) {
  if (!await markAsRead(id)) return
  appStore.showSuccess(t('announcements.markedAsRead'))
  closeDetail()
}

async function markAllAsRead() {
  try {
    await announcementStore.markAllAsRead()
    appStore.showSuccess(t('announcements.allMarkedAsRead'))
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
  }
}

</script>
