<template>
  <section class="open-settings-section" data-testid="profile-qq-card" :aria-busy="busy || loading">
    <div class="min-w-0">
      <AppHeading as="h2" variant="section">{{ t('qq.title') }}</AppHeading>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('qq.description') }}</p>
    </div>
    <div class="space-y-5 py-5">
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-if="error" role="alert" class="space-y-2 text-sm text-red-600 dark:text-red-400">
        <p>{{ error }}</p>
        <button v-if="!status" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="refresh">{{ t('qq.retry') }}</button>
      </div>
      <template v-if="status">
        <p v-if="!status.enabled" class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.unavailable') }}</p>
        <template v-else>
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div class="min-w-0 space-y-1">
              <span :class="['badge', status.qq ? 'badge-success' : 'badge-gray']">{{ t(status.qq ? 'qq.bound' : 'qq.unbound') }}</span>
              <p v-if="status.qq" class="text-sm text-gray-700 dark:text-gray-200">QQ {{ status.qq }}</p>
              <p v-else-if="!challengeActive" class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.bindHint') }}</p>
            </div>
            <button v-if="status.qq" type="button" class="btn btn-secondary btn-sm self-start" :disabled="busy" @click="unlink">{{ t('qq.unlink') }}</button>
            <button v-else type="button" :class="['btn btn-sm self-start', challengeActive ? 'btn-secondary' : 'btn-primary']" :disabled="busy" @click="createBinding">{{ t(challengeActive ? 'qq.regenerate' : 'qq.createCode') }}</button>
          </div>
          <div v-if="!status.qq && challengeActive" class="space-y-3 border-l-2 border-primary-200 pl-4 dark:border-primary-800">
            <p class="text-sm text-gray-700 dark:text-gray-200">{{ t('qq.addFriend', { qq: status.bot_qq }) }}</p>
            <template v-if="code">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.sendCommand') }}</p>
              <div class="flex flex-wrap items-center gap-2">
                <code class="select-all break-all rounded bg-gray-100 px-3 py-2 text-base dark:bg-dark-700 dark:text-gray-100">/sub 绑定 {{ code }}</code>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="copyCommand">{{ t('qq.copy') }}</button>
              </div>
            </template>
            <p v-else-if="!status.pending_qq" class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.codeHidden') }}</p>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('qq.expires', { time: expiryLabel }) }}</p>
            <p v-if="status.pending_qq" class="text-sm text-gray-700 dark:text-gray-200">{{ t('qq.candidate', { qq: status.pending_qq }) }}</p>
            <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.waiting') }}</p>
            <div class="flex flex-wrap gap-2">
              <button v-if="status.pending_qq" type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="confirmBinding">{{ t('qq.confirm') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="refresh">{{ t('qq.refresh') }}</button>
            </div>
          </div>
          <p v-else-if="!status.qq && code" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.expired') }}</p>
          <form class="space-y-4" @submit.prevent="savePreferences">
            <div class="max-w-md">
              <label :for="channelId" class="input-label">{{ t('qq.channel') }}</label>
              <select :id="channelId" :aria-describedby="channelHintId" v-model="draft.channel" class="input" :disabled="busy || !status.qq">
                <option value="email">{{ t('qq.email') }}</option>
                <option value="qq">QQ</option>
                <option value="both">{{ t('qq.both') }}</option>
              </select>
              <p :id="channelHintId" class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('qq.channelHint') }}</p>
            </div>
            <fieldset class="space-y-3" :disabled="busy">
              <legend class="input-label">{{ t('qq.events') }}</legend>
              <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="draft.balance_low" type="checkbox" class="mt-1" />{{ t('qq.balanceLow') }}</label>
              <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200"><input v-model="draft.subscription_expiry" type="checkbox" class="mt-1" />{{ t('qq.subscriptionExpiry') }}</label>
            </fieldset>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('qq.balanceHint') }}</p>
            <button type="submit" class="btn btn-primary btn-sm" :disabled="busy || !dirty">{{ t(busy ? 'common.saving' : 'common.save') }}</button>
          </form>
          <p v-if="status.qq" class="text-sm text-gray-500 dark:text-gray-400">{{ t('qq.commands') }}</p>
        </template>
      </template>
    </div>
    <ConfirmationPrompt :confirmation="unlinkConfirmation" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppHeading from '@/components/common/AppHeading.vue'
import ConfirmationPrompt from '@/components/common/ConfirmationPrompt.vue'
import { useConfirmedAction, useConfirmation, useConfirmationContext } from '@/composables/useConfirmation'
import { qqAPI, type QQPreferences, type QQStatus } from '@/api/qq'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t, locale } = useI18n()
const app = useAppStore()
const channelId = useId()
const channelHintId = useId()
const currentContext = useConfirmationContext()
const action = useConfirmedAction()
const status = ref<QQStatus | null>(null)
const initialDraft = (): QQPreferences => ({ channel: 'email', balance_low: true, subscription_expiry: true })
const draft = reactive<QQPreferences>(initialDraft())
const code = ref('')
const error = ref('')
const loading = ref(true)
const unlinkConfirmation = useConfirmation(() => ({
  active: !!status.value?.qq,
  identity: [status.value?.qq, status.value?.binding_id]
}))
const busy = computed(() => action.busy.value || unlinkConfirmation.busy.value)
const now = ref(Date.now())
let disposed = false
let refreshPending = false
let draftRevision = 0
let clock: ReturnType<typeof setInterval> | undefined
const challengeActive = computed(() => !!status.value?.expires_at && Date.parse(status.value.expires_at) > now.value)
const expiryLabel = computed(() => status.value?.expires_at ? new Date(status.value.expires_at).toLocaleTimeString(locale.value) : '')
const dirty = computed(() => !!status.value && (draft.channel !== status.value.channel || draft.balance_low !== status.value.balance_low || draft.subscription_expiry !== status.value.subscription_expiry))
watch(draft, () => { draftRevision++ }, { flush: 'sync' })

type IsCurrent = () => boolean
async function load(isCurrent: IsCurrent, replaceDraft = !dirty.value, expectedDraftRevision = draftRevision) {
  const next = await qqAPI.status()
  if (!isCurrent()) return
  status.value = next
  if (replaceDraft && expectedDraftRevision === draftRevision) {
    Object.assign(draft, { channel: next.channel, balance_low: next.balance_low, subscription_expiry: next.subscription_expiry })
  }
}
async function run(operation: (isCurrent: IsCurrent) => Promise<void>, stillCurrent: IsCurrent = () => true) {
  await action.run(async ownsRequest => {
    const isCurrent = () => ownsRequest() && stillCurrent()
    error.value = ''
    try { await operation(isCurrent) } catch (err) {
      if (isCurrent()) error.value = extractApiErrorMessage(err, t('common.error'))
    } finally {
      if (isCurrent()) loading.value = false
    }
  })
}
async function refresh() {
  if (busy.value) return
  await run(load)
}
async function createBinding() {
  if (busy.value) return
  await run(async isCurrent => {
    const result = await qqAPI.createBinding()
    if (!isCurrent() || !status.value) return
    code.value = result.code
    status.value = { ...status.value, pending_qq: null, expires_at: result.expires_at, bot_qq: result.bot_qq }
    now.value = Date.now()
  })
}
async function confirmBinding() {
  if (busy.value) return
  const candidate = status.value?.pending_qq
  const bindingId = status.value?.binding_id
  if (!candidate || !bindingId || !challengeActive.value) return
  await run(async isCurrent => {
    await qqAPI.confirm(candidate, bindingId)
    if (!isCurrent()) return
    code.value = ''
    await load(isCurrent)
  })
}
async function unlink() {
  if (busy.value || !status.value?.qq) return
  await unlinkConfirmation.run({
    title: t('qq.unlink'), message: t('qq.unlinkMessage'), confirmText: t('qq.unlink'), danger: true
  }, async isConfirmationCurrent => {
    await run(async isCurrent => {
      const expectedDraftRevision = draftRevision
      await qqAPI.unlink()
      if (!isCurrent()) return
      code.value = ''
      await load(isCurrent, true, expectedDraftRevision)
    }, isConfirmationCurrent)
  })
}
async function savePreferences() {
  if (busy.value || !dirty.value) return
  const preferences = { ...draft }
  const expectedDraftRevision = draftRevision
  await run(async isCurrent => {
    await qqAPI.preferences(preferences)
    if (!isCurrent()) return
    await load(isCurrent, true, expectedDraftRevision)
    if (isCurrent()) app.showSuccess(t('common.saved'))
  })
}
async function copyCommand() {
  if (busy.value) return
  const command = `/sub 绑定 ${code.value}`
  await run(async isCurrent => {
    try {
      await navigator.clipboard.writeText(command)
      if (isCurrent()) app.showSuccess(t('qq.copied'))
    } catch {
      if (isCurrent()) error.value = t('qq.copyFailed')
    }
  })
}
function refreshCurrentContext() {
  if (disposed || !refreshPending || busy.value || !currentContext().active) return
  refreshPending = false
  void refresh()
}
watch(currentContext, context => {
  status.value = null
  code.value = ''
  error.value = ''
  Object.assign(draft, initialDraft())
  loading.value = context.active
  refreshPending = context.active
  refreshCurrentContext()
}, { deep: true, flush: 'sync' })
watch(busy, refreshCurrentContext)
onMounted(() => {
  void refresh()
  clock = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => { disposed = true; if (clock) clearInterval(clock) })
</script>
