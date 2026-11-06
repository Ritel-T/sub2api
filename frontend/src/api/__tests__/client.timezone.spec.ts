import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'

vi.mock('@/utils/centralAccount', () => ({
  waitForCentralRequest: vi.fn(), consoleAuthHeaders: () => ({}), centralSessionVersion: () => 0,
  centralIdentityChanged: vi.fn(), markCentralUnavailable: vi.fn()
}))
vi.mock('@/utils/consoleDisplay', () => ({ browserTimezone: () => 'America/New_York' }))

describe('viewer timezone propagation', () => {
  afterEach(() => vi.unstubAllGlobals())
  it.each(['get', 'post'])('attaches viewer timezone to %s including batch statistics', async method => {
    let captured: any
    await apiClient.request({ url: '/usage', method, params: { timezone: undefined, page: 1 }, adapter: async config => {
      captured = config
      return { data: { code: 0, data: {} }, status: 200, statusText: 'OK', headers: {}, config }
    } })
    expect(captured.params).toEqual({ timezone: 'America/New_York', page: 1 })
  })
  it('preserves explicit policy zone and URLSearchParams', async () => {
    for (const params of [{ timezone: 'Asia/Tokyo' }, new URLSearchParams({ timezone: 'Asia/Tokyo' })]) {
      let zone: string | null = null
      await apiClient.get('/usage', { params, adapter: async config => {
        zone = config.params instanceof URLSearchParams ? config.params.get('timezone') : config.params.timezone
        return { data: { code: 0, data: {} }, status: 200, statusText: 'OK', headers: {}, config }
      } })
      expect(zone).toBe('Asia/Tokyo')
    }
  })
})
