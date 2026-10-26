<template>
  <BaseDialog
    :show="controller.visible.value"
    :title="t('stepUp.title')"
    width="narrow"
    :z-index="60"
    :show-close-button="false"
    :close-on-escape="!verifying"
    :close-on-click-outside="!verifying"
    @close="handleCancel"
  >
    <div v-if="centralAccountEnabled" class="space-y-4">
      <p>{{ locale.startsWith('zh') ? '请在新标签页完成账号再次验证，返回后关闭此对话框并手动重试操作。此处不会自动重放请求。' : 'Reauthenticate in a new tab, then return, close this dialog and retry the operation manually. No request is replayed automatically.' }}</p>
      <a :href="accountReauthenticationURL()" target="_blank" rel="noopener noreferrer" class="btn btn-primary">{{ locale.startsWith('zh') ? '再次验证' : 'Reauthenticate' }}</a>
    </div>
    <template v-else>
      <p class="mb-6 text-sm text-gray-500 dark:text-gray-400">{{ t('stepUp.hint') }}</p>
      <div class="mb-6">
        <input
          ref="hiddenOtpInputRef"
          type="text"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="6"
          class="pointer-events-none absolute left-0 top-0 h-px w-px opacity-0"
          aria-hidden="true"
          tabindex="-1"
          @input="handleHiddenOtpInput"
        />
        <div class="flex justify-center gap-2">
          <input
            v-for="(_, index) in 6"
            :key="index"
            :value="code[index]"
            :ref="(el) => setInputRef(el, index)"
            type="text"
            maxlength="1"
            inputmode="numeric"
            pattern="[0-9]"
            autocomplete="off"
            class="h-12 w-10 rounded-lg border border-gray-300 text-center text-lg font-semibold focus:border-primary-500 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
            :disabled="verifying"
            @input="handleCodeInput($event, index)"
            @keydown="handleKeydown($event, index)"
            @paste="handlePaste"
          />
        </div>
        <div v-if="verifying" class="mt-3 flex items-center justify-center gap-2 text-sm text-gray-500">
          <div role="status" :aria-label="t('common.loading')" class="animate-spin rounded-full h-4 w-4 border-b-2 border-primary-500"></div>
          {{ t('common.verifying') }}
        </div>
      </div>

    </template>

    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="verifying" @click="handleCancel">
        {{ t('common.cancel') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { accountReauthenticationURL, centralAccountEnabled } from '@/utils/accountConfig'

import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores'
import { totpAPI } from '@/api'
import type { StepUpController } from '@/composables/useStepUp'

const props = defineProps<{
  controller: StepUpController
}>()

const { t, locale } = useI18n()
const appStore = useAppStore()

const verifying = ref(false)
const code = ref<string[]>(['', '', '', '', '', ''])
const inputRefs = ref<(HTMLInputElement | null)[]>([])
const hiddenOtpInputRef = ref<HTMLInputElement | null>(null)

// Focus the first cell whenever the dialog opens.
watch(
  () => props.controller.visible.value,
  (open) => {
    if (open) {
      resetInputs()
      nextTick(() => inputRefs.value[0]?.focus())
    }
  }
)

// Auto-submit once 6 digits are entered.
watch(
  () => code.value.join(''),
  (newCode) => {
    if (!centralAccountEnabled && newCode.length === 6 && !verifying.value) {
      submit(newCode)
    }
  }
)

async function submit(otp: string) {
  if (centralAccountEnabled) return
  verifying.value = true
  try {
    await totpAPI.stepUp(otp)
    verifying.value = false
    resetInputs()
    props.controller.onVerified()
  } catch (err: any) {
    verifying.value = false
    appStore.showError(err?.message || t('stepUp.verifyFailed'))
    resetInputs()
    nextTick(() => inputRefs.value[0]?.focus())
  }
}

function resetInputs() {
  code.value = ['', '', '', '', '', '']
  inputRefs.value.forEach((input) => {
    if (input) input.value = ''
  })
  if (hiddenOtpInputRef.value) hiddenOtpInputRef.value.value = ''
}

function handleCancel() {
  if (verifying.value) return
  props.controller.onCancel()
}

const setInputRef = (el: any, index: number) => {
  inputRefs.value[index] = el as HTMLInputElement | null
}

const handleCodeInput = (event: Event, index: number) => {
  const input = event.target as HTMLInputElement
  const value = input.value.replace(/[^0-9]/g, '')
  code.value[index] = value
  if (value && index < 5) {
    nextTick(() => inputRefs.value[index + 1]?.focus())
  }
}

const handleHiddenOtpInput = (event: Event) => {
  const input = event.target as HTMLInputElement
  const digits = input.value.replace(/[^0-9]/g, '').slice(0, 6).split('')
  for (let i = 0; i < 6; i++) {
    code.value[i] = digits[i] || ''
    if (inputRefs.value[i]) inputRefs.value[i]!.value = digits[i] || ''
  }
}

const handleKeydown = (event: KeyboardEvent, index: number) => {
  if (event.key === 'Backspace') {
    const input = event.target as HTMLInputElement
    if (!input.value && index > 0) {
      event.preventDefault()
      inputRefs.value[index - 1]?.focus()
    }
  }
}

const handlePaste = (event: ClipboardEvent) => {
  event.preventDefault()
  const pastedData = event.clipboardData?.getData('text') || ''
  const digits = pastedData.replace(/[^0-9]/g, '').slice(0, 6).split('')
  for (let i = 0; i < 6; i++) {
    code.value[i] = digits[i] || ''
    if (inputRefs.value[i]) inputRefs.value[i]!.value = digits[i] || ''
  }
  const focusIndex = Math.min(digits.length, 5)
  nextTick(() => inputRefs.value[focusIndex]?.focus())
}
</script>
