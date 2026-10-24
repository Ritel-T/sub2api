// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAccountOpsThresholdAccounts, testAccountOpsWebhook, saveAccountOpsRule, deleteAccountOpsRule, saveAccountOpsWebhook, deleteAccountOpsWebhook, saveAccountOpsNotificationSettings } from '../admin/accountOps'
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('../client', () => ({ apiClient: client }))
beforeEach(() => {
  vi.resetAllMocks()
  client.put.mockResolvedValue({ data: { enabled: false } })
  client.delete.mockResolvedValue({ data: { enabled: false } })
})
describe('account operations notification endpoints', () => {
  it('loads monetary and percentage threshold candidates from the full account endpoint', async () => {
    const items = [{ account_id: 1, type: 'oauth', usage_windows: [] }]
    client.get.mockResolvedValue({ data: { items } })
    expect(await getAccountOpsThresholdAccounts()).toEqual(items)
    expect(client.get).toHaveBeenCalledWith('/admin/account-ops/threshold-accounts')
  })
  it('tests saved robot configuration using an encoded identifier and no credential request body', async () => {
    client.post.mockResolvedValue({ data: { ok: true } })
    expect(await testAccountOpsWebhook('bot/a')).toEqual({ ok: true })
    expect(client.post).toHaveBeenCalledWith('/admin/account-ops/webhooks/bot%2Fa/test')
  })
})

describe('scoped account-ops updates', () => {
  it('saves just one rule with its independent notification flags', async () => {
    const rule = { metric: 'balance' as const, enabled: true, threshold: 5, unit: 'USD', notify_alert: false, notify_recovery: true }
    await saveAccountOpsRule(12, rule)
    expect(client.put).toHaveBeenCalledWith('/admin/account-ops/rules/12', rule)
    await deleteAccountOpsRule(12, 'quota')
    expect(client.delete).toHaveBeenCalledWith('/admin/account-ops/rules/12', { params: { metric: 'quota' } })
  })
  it('changes a bot without posting rules or unrelated credentials', async () => {
    const input = { provider: 'wecom' as const, enabled: true, url: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-only' }
    await saveAccountOpsWebhook('bot/a', input)
    expect(client.put).toHaveBeenCalledWith('/admin/account-ops/webhooks/bot%2Fa', input)
    await deleteAccountOpsWebhook('bot/a')
    expect(client.delete).toHaveBeenCalledWith('/admin/account-ops/webhooks/bot%2Fa')
  })
  it('writes only common notification fields', async () => {
    const settings = { enabled: false, recipient: '', balance_low: true, weekly_quota: false, cooldown_minutes: 60 }
    await saveAccountOpsNotificationSettings(settings)
    expect(client.put).toHaveBeenCalledWith('/admin/account-ops/notification-settings', settings)
  })
})
