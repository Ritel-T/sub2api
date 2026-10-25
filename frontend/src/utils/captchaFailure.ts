import { extractApiErrorCode } from '@/utils/apiError'

// Keep password, email-code and transport failures separate from CAPTCHA failures.
export function isCaptchaVerificationError(error: unknown): boolean {
  const object = error && typeof error === 'object'
    ? error as { message?: unknown; response?: { data?: { reason?: unknown } } }
    : undefined
  const code = object?.response?.data?.reason ?? extractApiErrorCode(error)
  if (typeof code === 'string' && /^(TURNSTILE|TENCENT_CAPTCHA|ALIYUN_CAPTCHA)_/.test(code)) return true
  const message = typeof error === 'string' ? error : object?.message
  return typeof message === 'string' && (
    /^(TURNSTILE|TENCENT_CAPTCHA|ALIYUN_CAPTCHA)_/.test(message) ||
    /\b(turnstile|(?:tencent|aliyun)\s+captcha)\b.*(?:fail|invalid|missing|not configured|unavailable|expired)/i.test(message)
  )
}
