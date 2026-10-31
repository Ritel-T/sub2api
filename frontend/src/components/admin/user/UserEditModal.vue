<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.editUser')"
    width="normal"
    @close="$emit('close')"
  >
    <form v-if="user" id="edit-user-form" @submit.prevent="handleUpdateUser" class="space-y-5">
      <div>
        <label :id="'field-' + fieldLabelScope + '-1'" class="input-label">{{ t('admin.users.email') }}</label>
        <input :aria-labelledby="'field-' + fieldLabelScope + '-1'" v-model="form.email" type="email" class="input" />
      </div>
      <div>
        <label :id="'field-' + fieldLabelScope + '-2'" class="input-label">{{ t('admin.users.password') }}</label>
        <div class="flex gap-2">
          <div class="relative flex-1">
            <input :aria-labelledby="'field-' + fieldLabelScope + '-2'" v-model="form.password" type="text" class="input pr-10" :placeholder="t('admin.users.enterNewPassword')" />
            <button :aria-label="t('consoleFields.copy')" v-if="form.password" type="button" @click="copyPassword" class="absolute right-2 top-1/2 -translate-y-1/2 rounded-lg p-1 transition-colors hover:bg-gray-100 dark:hover:bg-dark-700" :class="passwordCopied ? 'text-green-500' : 'text-gray-400'">
              <svg v-if="passwordCopied" class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
              <svg v-else class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184" /></svg>
            </button>
          </div>
          <button :aria-label="t('admin.users.generatePassword')" type="button" @click="generatePassword" class="btn btn-secondary px-3">
            <Icon name="refresh" size="md" />
          </button>
        </div>
      </div>
      <div>
        <label :id="'field-' + fieldLabelScope + '-3'" class="input-label">{{ t('admin.users.username') }}</label>
        <input :aria-labelledby="'field-' + fieldLabelScope + '-3'" v-model="form.username" type="text" class="input" />
      </div>
      <div>
        <label :id="'field-' + fieldLabelScope + '-4'" class="input-label">{{ t('admin.users.form.roleLabel') }}</label>
        <Select :aria-labelledby="'field-' + fieldLabelScope + '-4'" :aria-label="t('admin.users.form.roleLabel')"
          v-model="form.role"
          :options="roleOptions"
          :searchable="false"
        />
      </div>
      <ObserverGroupSelector v-if="form.role === 'observer'" v-model="form.observer_group_ids" />
      <fieldset v-if="promotingObserver" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-test="observer-setup">
        <legend class="px-1 text-sm font-medium">{{ t('admin.users.observerSetup.title') }}</legend>
        <label class="flex items-start gap-2 text-sm">
          <input v-model="observerSetup.create_dedicated_group" :disabled="authStore.isSimpleMode" type="checkbox" class="mt-1" data-test="observer-create-group" />
          <span>{{ t('admin.users.observerSetup.createGroup') }}<span class="input-hint block">{{ t(authStore.isSimpleMode ? 'admin.users.observerSetup.simpleModeHint' : 'admin.users.observerSetup.createGroupHint') }}</span></span>
        </label>
        <label class="flex items-start gap-2 text-sm">
          <input v-model="observerSetup.revoke_public_groups" type="checkbox" class="mt-1" data-test="observer-revoke-public" />
          <span>{{ t('admin.users.observerSetup.revokePublic') }}<span class="input-hint block">{{ t('admin.users.observerSetup.revokePublicHint') }}</span></span>
        </label>
        <label class="flex items-start gap-2 text-sm">
          <input v-model="observerSetup.grant_resources" type="checkbox" class="mt-1" data-test="observer-grant-resources" />
          <span>{{ t('admin.users.observerSetup.grantResources') }}<span class="input-hint block">{{ t('admin.users.observerSetup.grantResourcesHint') }}</span></span>
        </label>
        <p class="input-hint">{{ t('admin.users.observerSetup.onceHint') }}</p>
      </fieldset>
      <div>
        <label :id="'field-' + fieldLabelScope + '-5'" class="input-label">{{ t('admin.users.notes') }}</label>
        <textarea :aria-labelledby="'field-' + fieldLabelScope + '-5'" v-model="form.notes" rows="3" class="input"></textarea>
      </div>
      <div>
        <label :id="'field-' + fieldLabelScope + '-6'" class="input-label">{{ t('admin.users.columns.concurrency') }}</label>
        <input :aria-labelledby="'field-' + fieldLabelScope + '-6'"
          v-model.number="form.concurrency"
          type="number"
          min="0"
          step="1"
          class="input"
          :placeholder="t('admin.users.form.concurrencyPlaceholder')"
          data-test="concurrency-input"
        />
        <p class="input-hint">{{ t('admin.users.form.concurrencyHint') }}</p>
      </div>
      <div>
        <label :id="'field-' + fieldLabelScope + '-7'" class="input-label">{{ t('admin.users.form.rpmLimit') }}</label>
        <input :aria-labelledby="'field-' + fieldLabelScope + '-7'"
          v-model.number="form.rpm_limit"
          type="number"
          min="0"
          step="1"
          class="input"
          :placeholder="t('admin.users.form.rpmLimitPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.users.form.rpmLimitHint') }}</p>
      </div>
      <ModerationUserPanel v-if="show && user && authStore.isAdmin" :key="user.id" :user-id="user.id" />
      <UserAttributeForm v-model="form.customAttributes" :user-id="user?.id" />
      <details v-if="show && authStore.isAdmin" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-test="security-reset">
        <summary class="cursor-pointer text-sm font-medium">{{ t('consoleInline.accountSecurityAdvanced_dfb02c') }}</summary>
        <div class="mt-4 space-y-3 text-sm" :aria-busy="securityResetBusy">
          <p class="break-words font-medium" data-test="security-reset-target">UID {{ user.id }} · {{ user.email }}</p>
          <p>{{ t('consoleInline.useOnlyAfterManuallyVerifyingTheAccount_aa57d0') }}</p>
          <p class="text-red-600 dark:text-red-400">{{ t('consoleInline.clearsThisUserSTOTPAllPasskeys_3b5531') }}</p>
          <label class="block">
            <span class="input-label">{{ t('consoleInline.manualIdentityVerificationNoteRequired_33f056') }}</span>
            <textarea v-model="securityVerificationNote" :disabled="securityResetLocked" rows="3" class="input" data-test="security-reset-note" :aria-invalid="securityNoteBytes > 1000" />
            <span class="input-hint block">{{ t('consoleInline.recordHowIdentityWasVerifiedDoNot_2287b6') }} {{ securityNoteBytes }}/1000</span>
          </label>
          <label class="flex items-start gap-2">
            <input v-model="securityResetConfirmed" :disabled="securityResetLocked" type="checkbox" class="mt-1" data-test="security-reset-confirm" />
            <span>{{ t('consoleInline.iHaveVerifiedThisAccountOwnerAnd_c3442c') }}</span>
          </label>
          <button type="button" class="btn btn-danger" :disabled="!canResetSecurity" data-test="security-reset-submit" @click="handleResetAccountSecurity">
            {{ securityResetBusy ? t('consoleInline.resetting_876d9b') : t('consoleInline.manuallyResetMFAAndSignOutAll_4781f2') }}
          </button>
          <p v-if="securityResetState === 'applied'" role="status" class="text-green-600 dark:text-green-400" data-test="security-reset-applied">{{ t('consoleInline.theServerConfirmedTheResetTOTPPasskeys_ca2cc2') }}</p>
          <div v-if="securityResetState === 'reauth'" role="alert" class="space-y-2" data-test="security-reset-reauth">
            <p>{{ t('consoleInline.administratorVerificationIsRequiredREAUTHREQUIREDIn_d2286e') }}</p>
            <a :href="accountReauthenticationURL()" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline dark:text-primary-400">{{ t('consoleInline.reauthenticateInANewTab_48b847') }}</a>
          </div>
          <p v-if="securityResetState === 'rejected'" role="alert" class="text-red-600 dark:text-red-400" data-test="security-reset-rejected">{{ t('consoleInline.theResetRequestWasRejectedCheckYour_f54d96') }}</p>
          <div v-if="securityResetState === 'unknown'" role="alert" class="space-y-2" data-test="security-reset-unknown">
            <p class="text-amber-700 dark:text-amber-400">{{ t('consoleInline.theResetOutcomeIsUnknownAndIt_37f8e7') }}</p>
            <p>{{ t('consoleInline.theReadbackBelowFetchesOnlyTheBasic_bbc14a') }}</p>
            <p v-if="securityReadback" class="break-words" data-test="security-reset-readback">{{ t('consoleInline.userRecordIndependentlyRead_0d5f36') }}: UID {{ securityReadback.id }} · {{ securityReadback.email }} · {{ securityReadback.status }}</p>
            <p v-if="securityReadbackState === 'failed'" data-test="security-reset-readback-failed">{{ t('consoleInline.independentReadbackFailedTheOutcomeRemainsUnknown_e24a5e') }}</p>
            <button type="button" class="btn btn-secondary" :disabled="securityResetBusy || securityReadbackState === 'loading'" data-test="security-reset-readback-button" @click="readBackResetTarget">
              {{ securityReadbackState === 'loading' ? t('consoleInline.readingIndependently_9e94d7') : t('consoleInline.readUserRecordAgain_b9ef95') }}
            </button>
          </div>
        </div>
      </details>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="edit-user-form" :disabled="submitting || securityResetBusy" class="btn btn-primary">
          {{ submitting ? t('admin.users.updating') : t('common.update') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <!-- 角色提升为管理员时后端要求 step-up 2FA，弹出 TOTP 验证后自动重试 -->
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { useId as useFieldLabelId } from 'vue'
const fieldLabelScope = useFieldLabelId()

import { accountReauthenticationURL } from '@/utils/accountConfig'
import { computed, ref, reactive, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useClipboard } from '@/composables/useClipboard'
import { adminAPI } from '@/api/admin'
import type { AdminUser, ObserverSetupOptions, UpdateUserRequest, UserAttributeValuesMap } from '@/types'
import ObserverGroupSelector from './ObserverGroupSelector.vue'
import ModerationUserPanel from '@/components/admin/moderation/ModerationUserPanel.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import UserAttributeForm from '@/components/user/UserAttributeForm.vue'
import Icon from '@/components/icons/Icon.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'

const props = defineProps<{ show: boolean, user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n(); const appStore = useAppStore(); const { copyToClipboard } = useClipboard()
const authStore = useAuthStore()

const submitting = ref(false); const passwordCopied = ref(false)
// Reuse the console's inline bilingual mechanism; no new global dictionary keys.
const securityVerificationNote = ref('')
const securityResetConfirmed = ref(false)
const securityResetBusy = ref(false)
const securityResetState = ref<'idle' | 'applied' | 'reauth' | 'rejected' | 'unknown'>('idle')
const securityReadback = ref<AdminUser | null>(null)
const securityReadbackState = ref<'idle' | 'loading' | 'loaded' | 'failed'>('idle')
let securityViewVersion = 0
onBeforeUnmount(() => { securityViewVersion++ })
const securityNoteBytes = computed(() => new TextEncoder().encode(securityVerificationNote.value.trim()).length)
const securityResetLocked = computed(() => securityResetBusy.value || securityResetState.value === 'applied' || securityResetState.value === 'unknown')
const canResetSecurity = computed(() => props.show && !!props.user && authStore.isAdmin && !submitting.value && !securityResetLocked.value && securityResetConfirmed.value && securityNoteBytes.value > 0 && securityNoteBytes.value <= 1000)

watch([() => props.show, () => props.user?.id], () => {
  securityViewVersion++
  securityVerificationNote.value = ''
  securityResetConfirmed.value = false
  securityResetState.value = 'idle'
  securityReadback.value = null
  securityReadbackState.value = 'idle'
}, { flush: 'sync' })

async function readResetUser(userId: number, version: number) {
  if (version === securityViewVersion) securityReadbackState.value = 'loading'
  try {
    const user = await adminAPI.users.getById(userId)
    if (user?.id !== userId) throw new Error('Unexpected readback target')
    if (version === securityViewVersion) {
      securityReadback.value = user
      securityReadbackState.value = 'loaded'
    }
  } catch {
    if (version === securityViewVersion) {
      securityReadback.value = null
      securityReadbackState.value = 'failed'
    }
  }
}

function readBackResetTarget() {
  if (!props.show || !props.user || securityResetBusy.value || securityReadbackState.value === 'loading' || securityResetState.value !== 'unknown') return
  return readResetUser(props.user.id, securityViewVersion)
}

async function handleResetAccountSecurity() {
  if (!canResetSecurity.value || !props.user) return
  const userId = props.user.id
  const version = securityViewVersion
  const verificationNote = securityVerificationNote.value.trim()
  securityResetBusy.value = true
  securityResetState.value = 'idle'
  securityReadback.value = null
  securityReadbackState.value = 'idle'
  try {
    // Native AuthProof/current role/MFA are enforced by the server. Never use
    // stepUp.run here: it replays the operation after verification.
    const result = await adminAPI.users.resetAccountSecurity(userId, { confirmed: true, verification_note: verificationNote })
    if (result?.applied === true) {
      if (version === securityViewVersion) {
        securityResetState.value = 'applied'
        securityVerificationNote.value = ''
      }
      emit('success')
    } else {
      if (version === securityViewVersion) securityResetState.value = 'unknown'
      await readResetUser(userId, version)
    }
  } catch (error: unknown) {
    const failure = error as { status?: number; code?: string; reason?: string; error?: string } | null
    const status = failure?.status ?? 0
    const outcomeUnknown = status === 0 || status >= 500 || status === 408 || status === 499 || failure?.code === 'ACCOUNT_SECURITY_RESET_OUTCOME_UNKNOWN'
    if (outcomeUnknown) {
      if (version === securityViewVersion) securityResetState.value = 'unknown'
      await readResetUser(userId, version)
    } else if ([failure?.code, failure?.reason, failure?.error].includes('REAUTH_REQUIRED') || status === 401) {
      if (version === securityViewVersion) securityResetState.value = 'reauth'
    } else if (status >= 400 && status < 500) {
      if (version === securityViewVersion) securityResetState.value = 'rejected'
    } else {
      if (version === securityViewVersion) securityResetState.value = 'unknown'
      await readResetUser(userId, version)
    }
  } finally {
    if (version === securityViewVersion) securityResetConfirmed.value = false
    securityResetBusy.value = false
  }
}

const observerSetup = reactive<ObserverSetupOptions>({ create_dedicated_group: false, revoke_public_groups: false, grant_resources: false })
const observerPromotionSaved = ref(false)
const promotingObserver = computed(() => !!props.user && props.user.role !== 'observer' && form.role === 'observer' && !observerPromotionSaved.value)
const resetObserverSetup = () => Object.assign(observerSetup, { create_dedicated_group: false, revoke_public_groups: false, grant_resources: false })
const roleOptions = computed(() => [
  { value: 'user', label: t('admin.users.roles.user') },
  { value: 'observer', label: t('admin.users.roles.observer') },
  { value: 'admin', label: t('admin.users.roles.admin') }
])
const form = reactive({
  email: '',
  password: '',
  username: '',
  notes: '',
  role: 'user' as AdminUser['role'],
  observer_group_ids: [] as number[],
  concurrency: 1,
  rpm_limit: 0,
  customAttributes: {} as UserAttributeValuesMap
})

watch(() => props.user, (u) => {
  resetObserverSetup()
  observerPromotionSaved.value = false
  if (u) {
    Object.assign(form, { email: u.email, password: '', username: u.username || '', notes: u.notes || '', role: u.role || 'user', observer_group_ids: [...(u.observer_group_ids || [])], concurrency: u.concurrency, rpm_limit: u.rpm_limit ?? 0, customAttributes: {} })
    passwordCopied.value = false
  }
}, { immediate: true })
watch(() => props.show, () => { resetObserverSetup(); observerPromotionSaved.value = false })
watch(() => form.role, () => resetObserverSetup())

const generatePassword = () => {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'
  let p = ''; for (let i = 0; i < 16; i++) p += chars.charAt(Math.floor(Math.random() * chars.length))
  form.password = p
}
const copyPassword = async () => {
  if (form.password && await copyToClipboard(form.password, t('admin.users.passwordCopied'))) {
    passwordCopied.value = true; setTimeout(() => passwordCopied.value = false, 2000)
  }
}
const stepUp = useStepUp()

const handleUpdateUser = async () => {
  if (!props.user || submitting.value || securityResetBusy.value) return
  if (!form.email.trim()) {
    appStore.showError(t('admin.users.emailRequired'))
    return
  }
  // 0 = 不限制，与网关 (AcquireUserSlot: maxConcurrency <= 0) 和批量改限额一致
  if (!Number.isInteger(form.concurrency) || form.concurrency < 0) {
    appStore.showError(t('admin.users.concurrencyNonNegative'))
    return
  }
  if (promotingObserver.value && observerSetup.create_dedicated_group && !form.username.trim()) {
    appStore.showError(t('admin.users.observerSetup.usernameRequired'))
    return
  }
  const userId = props.user.id
  submitting.value = true
  try {
    const data: UpdateUserRequest = { email: form.email, username: form.username, notes: form.notes, role: form.role, observer_group_ids: form.observer_group_ids, concurrency: form.concurrency, rpm_limit: form.rpm_limit }
    if (promotingObserver.value && Object.values(observerSetup).some(Boolean)) data.observer_setup = { ...observerSetup }
    if (form.password.trim()) data.password = form.password.trim()
    // 提升为管理员属敏感操作：后端返回 STEP_UP_REQUIRED 时弹 TOTP 验证并重试
    const updated = await stepUp.run(() => adminAPI.users.update(userId, data))
    // Attribute updates use a separate request. Keep committed group grants and
    // concurrency if that request fails and the administrator retries the form.
    form.observer_group_ids = [...(updated.observer_group_ids ?? form.observer_group_ids)]
    form.concurrency = updated.concurrency ?? form.concurrency
    if (form.role === 'observer') observerPromotionSaved.value = true
    resetObserverSetup()
    if (Object.keys(form.customAttributes).length > 0) await adminAPI.userAttributes.updateUserAttributeValues(userId, form.customAttributes)
    appStore.showSuccess(t('admin.users.userUpdated'))
    emit('success'); emit('close')
  } catch (e: any) {
    if (isStepUpCancelled(e)) {
      // 用户主动取消二次验证：静默返回，表单保持打开。
    } else if (isStepUpBlocked(e)) {
      appStore.showError(
        stepUpBlockReason(e) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
          ? t('stepUp.adminApiKeyForbidden')
          : t('stepUp.notEnabled')
      )
    } else {
      appStore.showError(e?.message || t('admin.users.failedToUpdate'))
    }
  } finally { submitting.value = false }
}
</script>
