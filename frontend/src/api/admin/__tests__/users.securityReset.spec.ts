import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'
import { apiClient } from '../../client'
import { resetAccountSecurity, usersAPI } from '../users'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const { refreshAuthTokens } = vi.hoisted(() => ({ refreshAuthTokens: vi.fn() }))
vi.mock('../../tokenRefresh', () => ({ refreshAuthTokens }))
const originalAdapter = apiClient.defaults.adapter

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  sessionStorage.clear()
  localStorage.setItem('auth_token', 'native-signed-proof-test-token')
  localStorage.setItem('refresh_token', 'refresh-test-token')
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter })

describe('admin manual account security reset transport', () => {
  it('uses the existing native-auth client and selected route, with an exact minimal body', async () => {
    const adapter = vi.fn(async config => ({
      config, status: 200, statusText: 'OK', headers: {},
      data: { code: 0, data: { applied: true } }
    }))
    apiClient.defaults.adapter = adapter
    const body = { confirmed: true as const, verification_note: 'Owner verified manually', user_id: 99 }
    await expect(usersAPI.resetAccountSecurity(7, body)).resolves.toEqual({ applied: true })
    expect(adapter).toHaveBeenCalledTimes(1)
    const config = adapter.mock.calls[0][0]
    expect(config.url).toBe('/admin/users/7/account-security/reset')
    expect(config.method).toBe('post')
    expect(config.baseURL).toBe('/api/v1')
    expect(JSON.parse(config.data)).toEqual({ confirmed: true, verification_note: 'Owner verified manually' })
    expect(config.headers.get('Authorization')).toBe('Bearer native-signed-proof-test-token')
    expect(config._retry).toBe(true)
    expect(refreshAuthTokens).not.toHaveBeenCalled()
  })

  it.each([401, 403, 503])('never refreshes or replays the reset after HTTP %i', async status => {
    const adapter = vi.fn(async config => {
      const response = { config, status, statusText: 'Failure', headers: {}, data: { code: status === 503 ? 'ACCOUNT_SECURITY_RESET_OUTCOME_UNKNOWN' : 'REAUTH_REQUIRED' } }
      throw new axios.AxiosError('reset rejected', 'ERR_BAD_RESPONSE', config, undefined, response)
    })
    apiClient.defaults.adapter = adapter
    await expect(resetAccountSecurity(7, { confirmed: true, verification_note: 'Owner verified' })).rejects.toMatchObject({ status })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(refreshAuthTokens).not.toHaveBeenCalled()
    expect(localStorage.getItem('auth_token')).toBe('native-signed-proof-test-token')
  })

  it('surfaces network uncertainty without retrying or inventing an applied result', async () => {
    const adapter = vi.fn(async config => { throw new axios.AxiosError('disconnected', 'ERR_NETWORK', config) })
    apiClient.defaults.adapter = adapter
    await expect(resetAccountSecurity(7, { confirmed: true, verification_note: 'Owner verified' })).rejects.toMatchObject({ status: 0 })
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(refreshAuthTokens).not.toHaveBeenCalled()
  })
})
