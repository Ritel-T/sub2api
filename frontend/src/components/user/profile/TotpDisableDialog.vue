<template>
  <BaseDialog
    :show="true"
    :title="t('profile.totp.disableTitle')"
    width="narrow"
    :show-close-button="!busy"
    :close-on-escape="!busy"
    :close-on-click-outside="!busy"
    @close="requestClose"
  >
    <p class="mb-6 text-sm text-gray-500 dark:text-gray-400">{{ t('profile.totp.disableWarning') }}</p>
    <!-- Loading verification method -->
    <div v-if="methodLoading" class="flex items-center justify-center py-8">
      <div role="status" :aria-label="t('common.loading')" class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary-500"></div>
    </div>

    <form v-else :id="disableFormId" @submit.prevent="handleDisable" class="space-y-4">
      <!-- Email verification -->
      <div v-if="verificationMethod === 'email'">
        <label class="input-label">{{ t('profile.totp.emailCode') }}</label>
        <div class="flex gap-2">
          <input
            v-model="form.emailCode"
            :disabled="busy"
            type="text"
            maxlength="6"
            inputmode="numeric"
            class="input flex-1"
            :placeholder="t('profile.totp.enterEmailCode')"
          />
          <button
            type="button"
            class="btn btn-secondary whitespace-nowrap"
            :disabled="busy || codeCooldown > 0"
            @click="handleSendCode"
          >
            {{ codeCooldown > 0 ? `${codeCooldown}s` : (sendingCode ? t('common.sending') : t('profile.totp.sendCode')) }}
          </button>
        </div>
      </div>

      <!-- Password verification -->
      <div v-else>
        <label for="password" class="input-label">
          {{ t('profile.currentPassword') }}
        </label>
        <input
          id="password"
          v-model="form.password"
          :disabled="busy"
          type="password"
          autocomplete="current-password"
          class="input"
          :placeholder="t('profile.totp.enterPassword')"
        />
      </div>

    </form>
    <template #footer v-if="!methodLoading">
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="requestClose">{{ t('common.cancel') }}</button>
      <button type="submit" :form="disableFormId" class="btn btn-danger" :disabled="busy || !canSubmit">
        {{ loading ? t('common.processing') : t('profile.totp.confirmDisable') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { totpAPI } from '@/api'
import { extractApiErrorMessage } from '@/utils/apiError'

const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const disableFormId = useId()

const methodLoading = ref(true)
const verificationMethod = ref<'email' | 'password'>('password')
const loading = ref(false)
const sendingCode = ref(false)
const busy = computed(() => loading.value || sendingCode.value)
const requestClose = () => { if (!busy.value) emit('close') }
let disposed = false
const codeCooldown = ref(0)
const cooldownTimer = ref<ReturnType<typeof setInterval> | null>(null)
const form = ref({
  emailCode: '',
  password: ''
})

const canSubmit = computed(() => {
  if (verificationMethod.value === 'email') {
    return form.value.emailCode.length === 6
  }
  return form.value.password.length > 0
})

const loadVerificationMethod = async () => {
  methodLoading.value = true
  try {
    const method = await totpAPI.getVerificationMethod()
    verificationMethod.value = method.method
  } catch (err: any) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
    emit('close')
  } finally {
    methodLoading.value = false
  }
}

const handleSendCode = async () => {
  if (busy.value || codeCooldown.value > 0) return
  sendingCode.value = true
  try {
    await totpAPI.sendVerifyCode()
    if (disposed) return
    appStore.showSuccess(t('profile.totp.codeSent'))
    // Start cooldown
    codeCooldown.value = 60
    if (cooldownTimer.value) {
      clearInterval(cooldownTimer.value)
      cooldownTimer.value = null
    }
    cooldownTimer.value = setInterval(() => {
      codeCooldown.value--
      if (codeCooldown.value <= 0) {
        if (cooldownTimer.value) {
          clearInterval(cooldownTimer.value)
          cooldownTimer.value = null
        }
      }
    }, 1000)
  } catch (err: any) {
    appStore.showError(extractApiErrorMessage(err, t('profile.totp.sendCodeFailed')))
  } finally {
    sendingCode.value = false
  }
}

const handleDisable = async () => {
  if (busy.value || !canSubmit.value) return

  loading.value = true

  try {
    const request = verificationMethod.value === 'email'
      ? { email_code: form.value.emailCode }
      : { password: form.value.password }

    await totpAPI.disable(request)
    appStore.showSuccess(t('profile.totp.disableSuccess'))
    emit('success')
  } catch (err: any) {
    appStore.showError(extractApiErrorMessage(err, t('profile.totp.disableFailed')))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadVerificationMethod()
})

onUnmounted(() => {
  disposed = true
  if (cooldownTimer.value) {
    clearInterval(cooldownTimer.value)
    cooldownTimer.value = null
  }
})
</script>
