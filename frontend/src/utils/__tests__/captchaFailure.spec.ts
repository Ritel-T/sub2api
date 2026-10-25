import { describe, expect, it } from 'vitest'
import { isCaptchaVerificationError } from '../captchaFailure'

describe('CAPTCHA rejection classification', () => {
  it.each([
    { reason: 'TURNSTILE_VERIFICATION_FAILED', code: 400 },
    { reason: 'TENCENT_CAPTCHA_VERIFICATION_FAILED' },
    { code: 'ALIYUN_CAPTCHA_VERIFICATION_FAILED' },
    { response: { data: { reason: 'TURNSTILE_NOT_CONFIGURED', code: 503 } } },
    'turnstile verification failed',
    'TURNSTILE_VERIFICATION_FAILED',
    { message: 'tencent captcha verification failed' }
  ])('recognizes provider failure %j', error => expect(isCaptchaVerificationError(error)).toBe(true))
  it.each([null, undefined, {}, 'Verification failed', { reason: 'INVALID_CREDENTIALS' }, { reason: 'INVALID_VERIFY_CODE' }, { code: 'ERR_NETWORK' }, { message: 'email verification failed' }])('does not mislabel other errors %j', error => expect(isCaptchaVerificationError(error)).toBe(false))
})
