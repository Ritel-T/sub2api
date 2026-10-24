<template>
  <div class="open-settings-section">
    <div class="flex items-start justify-between min-w-0">
      <div>
        <AppHeading as="h2" variant="section" class="text-gray-900 dark:text-white">
          {{ t('profile.passkey.title') }}
        </AppHeading>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('profile.passkey.description') }}
        </p>
      </div>
      <button
        v-if="enabled && supported && !showAddForm"
        type="button"
        class="btn btn-primary"
        :disabled="busy"
        @click="showAddForm = true"
      >
        {{ t('profile.passkey.add') }}
      </button>
    </div>

    <div class="min-w-0">
      <div v-if="!enabled" class="mb-5 text-sm text-gray-500 dark:text-gray-400">
        {{ t('profile.passkey.featureDisabled') }}
      </div>
      <div v-if="enabled && !supported" class="mb-5 text-sm text-amber-600 dark:text-amber-400">
        {{ t('profile.passkey.unsupported') }}
      </div>
      <div>
        <form
          v-if="enabled && supported && showAddForm"
          class="mb-5 flex flex-col gap-3 border-t border-gray-200 py-4 dark:border-dark-700"
          @submit.prevent="addPasskey"
        >
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <label for="passkey-name" class="input-label">{{ t('profile.passkey.name') }}</label>
              <input
                id="passkey-name"
                v-model="newName"
                class="input"
                maxlength="100"
                :placeholder="t('profile.passkey.namePlaceholder')"
                autofocus
              />
            </div>
            <div>
              <label for="passkey-add-password" class="input-label">{{
                t('profile.currentPassword')
              }}</label>
              <input
                id="passkey-add-password"
                v-model="newPassword"
                type="password"
                autocomplete="current-password"
                class="input"
                :placeholder="t('profile.passkey.passwordPlaceholder')"
              />
            </div>
          </div>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="cancelAdd">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary" :disabled="busy || newPassword.length === 0">
              {{ busy ? t('common.processing') : t('profile.passkey.continue') }}
            </button>
          </div>
        </form>

        <div v-if="loading" class="flex justify-center py-6">
          <div role="status" :aria-label="t('common.loading')" class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-500"></div>
        </div>

        <div
          v-else-if="credentials.length === 0"
          class="py-8 text-sm text-gray-500 dark:text-gray-400"
        >
          {{ t('profile.passkey.empty') }}
        </div>

        <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
          <div
            v-for="credential in credentials"
            :key="credential.id"
            class="flex items-center justify-between gap-4 py-4 first:pt-0 last:pb-0"
          >
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <Icon name="key" size="md" class="shrink-0 text-primary-500" />
                <p class="truncate font-medium text-gray-900 dark:text-white">
                  {{ credential.name }}
                </p>
                <span
                  v-if="credential.backup"
                  class="rounded-full bg-green-50 px-2 py-0.5 text-xs text-green-700 dark:bg-green-900/30 dark:text-green-300"
                >
                  {{ t('profile.passkey.synced') }}
                </span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('profile.passkey.createdAt', { date: formatDate(credential.created_at) }) }}
                <template v-if="credential.last_used_at">
                  · {{ t('profile.passkey.lastUsed', { date: formatDate(credential.last_used_at) }) }}
                </template>
              </p>
            </div>
            <div class="flex shrink-0 gap-2">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="busy"
                @click="renamePasskey(credential)"
              >
                {{ t('common.edit') }}
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm text-red-600 hover:bg-red-50 dark:text-red-300 dark:hover:bg-red-950/30"
                :disabled="busy"
                @click="deletePasskey(credential)"
              >
                {{ t('common.delete') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <BaseDialog
      :show="!!renameTarget"
      :title="t('profile.passkey.renamePrompt')"
      width="narrow"
      :show-close-button="!busy"
      :close-on-escape="!busy"
      :close-on-click-outside="!busy"
      @close="closeRenameDialog"
    >
      <form :id="renameFormId" @submit.prevent="confirmRename">
        <label :for="renameInputId" class="input-label">{{ t('profile.passkey.name') }}</label>
        <input
          :id="renameInputId"
          v-model="renameName"
          class="input"
          maxlength="100"
          :disabled="busy"
          :placeholder="t('profile.passkey.namePlaceholder')"
          autofocus
        />
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeRenameDialog">
          {{ t('common.cancel') }}
        </button>
        <button type="submit" :form="renameFormId" class="btn btn-primary" :disabled="busy || !canRename">
          {{ busy ? t('common.processing') : t('common.save') }}
        </button>
      </template>
    </BaseDialog>

    <!-- Revocation retains the current-password confirmation and original request flow. -->
    <BaseDialog :show="!!deleteTarget" :title="t('profile.passkey.deleteTitle')" width="narrow"
      :show-close-button="!busy" :close-on-escape="!busy" :close-on-click-outside="!busy" @close="requestDeleteClose">
      <p v-if="deleteTarget" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('profile.passkey.deleteConfirm', { name: deleteTarget.name }) }}
      </p>
      <form :id="deleteFormId" class="mt-4 space-y-4" @submit.prevent="confirmDelete">
        <label for="passkey-delete-password" class="input-label">{{ t('profile.currentPassword') }}</label>
        <input id="passkey-delete-password" v-model="deletePassword" type="password" autocomplete="current-password"
          class="input" :disabled="busy" :placeholder="t('profile.passkey.passwordPlaceholder')" autofocus />
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="requestDeleteClose">{{ t('common.cancel') }}</button>
        <button type="submit" :form="deleteFormId" class="btn btn-danger" :disabled="busy || deletePassword.length === 0">
          {{ busy ? t('common.processing') : t('common.delete') }}
        </button>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import AppHeading from '@/components/common/AppHeading.vue'
import { computed, ref, watch, useId } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useConfirmedAction, useConfirmationContext } from '@/composables/useConfirmation'
import { useI18n } from 'vue-i18n'
import { passkeyAPI, type PasskeyCredentialSummary } from '@/api'
import { Icon } from '@/components/icons'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ enabled: boolean }>()

const { t } = useI18n()
const appStore = useAppStore()
const deleteFormId = useId()
const renameFormId = useId()
const renameInputId = useId()
const renameTarget = ref<PasskeyCredentialSummary | null>(null)
const renameName = ref('')
const canRename = computed(() => !!renameName.value.trim() && renameName.value.trim() !== renameTarget.value?.name)
const supported = passkeyAPI.isSupported()
const loading = ref(false)
const busy = ref(false)
const showAddForm = ref(false)
const newName = ref('')
const newPassword = ref('')
const deleteTarget = ref<PasskeyCredentialSummary | null>(null)
const deletePassword = ref('')
const credentials = ref<PasskeyCredentialSummary[]>([])
const deleteContext = useConfirmationContext(() => ({ active: props.enabled }))
const deleteAction = useConfirmedAction(deleteContext)
watch(deleteContext, () => { closeDeleteDialog() }, { deep: true, flush: 'sync' })

// apiClient 拦截器把错误规范化为 { code, reason, message }；
// 透出后端消息（如密码错误），否则回退到通用文案。
function extractErrorMessage(error: unknown, fallback: string): string {
  const message = (error as { message?: string }).message
  return typeof message === 'string' && message.length > 0 ? message : fallback
}

async function loadCredentials(): Promise<void> {
  if (!props.enabled) {
    credentials.value = []
    return
  }
  loading.value = true
  try {
    credentials.value = await passkeyAPI.list()
  } catch (error) {
    // 字符串错误码在 reason 字段（code 是数字状态码）；
    // 设置变更竞态下后端仍可能返回 PASSKEY_DISABLED，静默处理
    const reason = (error as { reason?: string }).reason
    if (reason !== 'PASSKEY_DISABLED') {
      appStore.showError(t('profile.passkey.loadFailed'))
    }
  } finally {
    loading.value = false
  }
}

async function addPasskey(): Promise<void> {
  if (busy.value || newPassword.value.length === 0) return
  busy.value = true
  try {
    await passkeyAPI.register(newName.value.trim(), newPassword.value)
    appStore.showSuccess(t('profile.passkey.added'))
    cancelAdd()
    await loadCredentials()
  } catch (error) {
    if (!(error instanceof DOMException && error.name === 'NotAllowedError')) {
      appStore.showError(extractErrorMessage(error, t('profile.passkey.addFailed')))
    }
  } finally {
    busy.value = false
  }
}

function cancelAdd(): void {
  showAddForm.value = false
  newName.value = ''
  newPassword.value = ''
}

function renamePasskey(credential: PasskeyCredentialSummary): void {
  if (busy.value) return
  renameTarget.value = credential
  renameName.value = credential.name
}

function closeRenameDialog(): void {
  if (busy.value) return
  renameTarget.value = null
  renameName.value = ''
}

async function confirmRename(): Promise<void> {
  const credential = renameTarget.value
  const name = renameName.value.trim()
  if (busy.value || !credential || !canRename.value) return
  busy.value = true
  try {
    await passkeyAPI.rename(credential.id, name)
    credential.name = name
    renameTarget.value = null
    renameName.value = ''
    appStore.showSuccess(t('profile.passkey.renamed'))
  } catch {
    appStore.showError(t('profile.passkey.renameFailed'))
  } finally {
    busy.value = false
  }
}

function deletePasskey(credential: PasskeyCredentialSummary): void {
  if (busy.value || deleteAction.busy.value || deleteTarget.value || !deleteContext().active) return
  deleteTarget.value = { ...credential }
  deletePassword.value = ''
}

function requestDeleteClose(): void {
  if (!busy.value) closeDeleteDialog()
}

function closeDeleteDialog(): void {
  deleteTarget.value = null
  deletePassword.value = ''
}

async function confirmDelete(): Promise<void> {
  const credential = deleteTarget.value
  const password = deletePassword.value
  if (!credential || busy.value || password.length === 0) return
  await deleteAction.run(async (isCurrent) => {
    busy.value = true
    try {
      await passkeyAPI.remove(credential.id, password)
      if (!isCurrent()) return
      credentials.value = credentials.value.filter((item) => item.id !== credential.id)
      appStore.showSuccess(t('profile.passkey.deleted'))
      closeDeleteDialog()
    } catch (error) {
      // Password failures remain editable in the same live session.
      if (isCurrent()) appStore.showError(extractErrorMessage(error, t('profile.passkey.deleteFailed')))
    } finally {
      busy.value = false
    }
  })
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric'
  }).format(new Date(value))
}

watch(
  () => props.enabled,
  () => {
    void loadCredentials()
  },
  { immediate: true }
)
</script>
