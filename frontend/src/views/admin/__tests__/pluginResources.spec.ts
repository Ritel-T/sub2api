import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginInstallation } from '@/api/admin'
import { loadPluginResources } from '../pluginResources'

const { list, groups, proxies } = vi.hoisted(() => ({ list: vi.fn(), groups: vi.fn(), proxies: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: { list }, groups: { getAllIncludingInactive: groups }, proxies: { getAll: proxies },
} }))
const plugin = { state: 'disabled', manifest: { capabilities: [{
  id: 'openai.oauth.outbound_transport.v1', platform: 'openai', account_type: 'oauth',
}] } } as PluginInstallation

describe('passive plugin resource directory', () => {
  beforeEach(() => { vi.clearAllMocks(); groups.mockResolvedValue([]); proxies.mockResolvedValue([]) })
  it('loads every page while stopped, restricts accounts and strips secrets', async () => {
    list.mockResolvedValueOnce({ pages: 2, items: [
      { id: 1, name: 'One', platform: 'openai', type: 'oauth', group_ids: [3, 4, 3], credentials: { token: 'secret' } },
      { id: 99, name: 'Other', platform: 'openai', type: 'apikey', group_ids: [9] },
    ] }).mockResolvedValueOnce({ pages: 2, items: [
      { id: 2, name: 'Two', platform: 'openai', type: 'oauth', extra: { private: 'secret' } },
    ] })
    groups.mockResolvedValue([
      { id: 3, name: 'OpenAI', platform: 'openai', private: 'secret' },
      { id: 4, name: 'Mixed', platform: 'composite' },
      { id: 9, name: 'Other', platform: 'anthropic' },
    ])
    proxies.mockResolvedValue([
      { id: 5, name: 'Proxy', status: 'active', protocol: 'socks5', host: 'example.com', port: 1080, username: 'secret', password: 'secret' },
      { id: 6, status: 'inactive' }, { id: 7, status: 'active', expires_at: '2000-01-01T00:00:00Z' },
    ])
    expect(await loadPluginResources(plugin)).toEqual({
      accounts: [{ id: 1, name: 'One', group_ids: [3, 4] }, { id: 2, name: 'Two', group_ids: [] }],
      groups: [{ id: 3, name: 'OpenAI' }, { id: 4, name: 'Mixed' }],
      proxies: [{ id: 5, name: 'Proxy', protocol: 'socks5', host: 'example.com', port: 1080 }],
    })
    expect(list).toHaveBeenNthCalledWith(2, 2, 100, {
      platform: 'openai', type: 'oauth', lite: 'true', sort_by: 'id', sort_order: 'asc',
    })
  })
  it('rejects a plugin without matching capability before any API reads', async () => {
    await expect(loadPluginResources({ ...plugin, manifest: { ...plugin.manifest, capabilities: [] } })).rejects.toThrow('权限')
    expect(list).not.toHaveBeenCalled(); expect(groups).not.toHaveBeenCalled(); expect(proxies).not.toHaveBeenCalled()
  })
  it('does not deliver a partial directory when a later page fails', async () => {
    list.mockResolvedValueOnce({ pages: 2, items: [{ id: 1, name: 'One', platform: 'openai', type: 'oauth' }] })
      .mockRejectedValueOnce(new Error('network'))
    await expect(loadPluginResources(plugin)).rejects.toThrow('network')
  })
})
