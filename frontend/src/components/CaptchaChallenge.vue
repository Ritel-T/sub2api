<template>
  <CaptchaBrowserNotice v-if="captchaEnabled" :verification-failed="verificationFailed" />
  <TurnstileWidget
    v-if="turnstileEnabled && turnstileSiteKey"
    ref="turnstileRef"
    :site-key="turnstileSiteKey"
    @verify="onVerify"
    @expire="onExpire"
    @error="onError"
  />
  <TencentCaptchaGate
    v-else-if="tencentEnabled && tencentAppId"
    ref="tencentRef"
    :app-id="tencentAppId"
    :region="tencentRegion"
    @error="onError"
    @verified="verificationFailed = false"
  />
  <AliyunCaptchaWidget
    v-else-if="aliyunEnabled && aliyunSceneId && aliyunPrefix"
    ref="aliyunRef"
    :scene-id="aliyunSceneId"
    :prefix="aliyunPrefix"
    :region="aliyunRegion === 'sgp' ? 'sgp' : 'cn'"
    @verify="onVerify"
    @expire="onExpire"
    @error="onError"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { isCaptchaVerificationError } from '@/utils/captchaFailure'
import CaptchaBrowserNotice from '@/components/CaptchaBrowserNotice.vue'
import TurnstileWidget from '@/components/TurnstileWidget.vue'
import TencentCaptchaGate from '@/components/TencentCaptchaGate.vue'
import AliyunCaptchaWidget from '@/components/AliyunCaptchaWidget.vue'

// ActionCaptchaResult 动作触发式验证（腾讯/阿里云弹窗）的结果：
// 腾讯 token=ticket、randstr 非空；阿里云 token=captchaVerifyParam、randstr 恒为空。
export interface ActionCaptchaResult {
  token: string
  randstr: string
}

const props = defineProps<{
  siteKey?: string
  turnstileEnabled: boolean
  turnstileSiteKey: string
  tencentEnabled: boolean
  tencentAppId: string
  tencentRegion?: string
  aliyunEnabled?: boolean
  aliyunSceneId?: string
  aliyunPrefix?: string
  aliyunRegion?: string
}>()

const captchaEnabled = computed(() =>
  Boolean((props.turnstileEnabled && props.turnstileSiteKey) ||
    (props.tencentEnabled && props.tencentAppId) ||
    (props.aliyunEnabled && props.aliyunSceneId && props.aliyunPrefix))
)

const emit = defineEmits<{
  verify: [tokenOrTicket: string, randstr: string]
  expire: []
  error: []
}>()

const verificationFailed = ref(false)

function reportFailure(error?: unknown): void {
  if (error === undefined || isCaptchaVerificationError(error)) verificationFailed.value = true
}

function onError(): void {
  reportFailure()
  emit('error')
}

function onExpire(): void {
  reportFailure()
  emit('expire')
}

function onVerify(token: string): void {
  verificationFailed.value = false
  emit('verify', token, '')
}

const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const tencentRef = ref<InstanceType<typeof TencentCaptchaGate> | null>(null)
const aliyunRef = ref<InstanceType<typeof AliyunCaptchaWidget> | null>(null)

function reset(): void {
  turnstileRef.value?.reset()
  tencentRef.value?.reset()
  aliyunRef.value?.reset()
}

// verifyAction 弹出当前启用的动作触发式验证码（腾讯/阿里云）并等待结果；
// 用户关闭弹窗返回 null，验证异常 emit('error') 并返回 null。
async function verifyAction(): Promise<ActionCaptchaResult | null> {
  if (props.tencentEnabled && props.tencentAppId) {
    try {
      const proof = (await tencentRef.value?.verify()) ?? null
      if (!proof) return null
      verificationFailed.value = false
      return { token: proof.ticket, randstr: proof.randstr }
    } catch {
      onError()
      return null
    }
  }
  if (props.aliyunEnabled && props.aliyunSceneId && props.aliyunPrefix) {
    try {
      const param = (await aliyunRef.value?.verify()) ?? null
      if (!param) return null
      verificationFailed.value = false
      return { token: param, randstr: '' }
    } catch {
      onError()
      return null
    }
  }
  return null
}

defineExpose({ reset, verifyAction, reportFailure })
</script>
