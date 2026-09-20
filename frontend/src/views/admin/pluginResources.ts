import { adminAPI, type PluginInstallation } from '@/api/admin'

/** Read-only UI metadata: available before the plugin runtime starts. */
export async function loadPluginResources(plugin: PluginInstallation) {
  if (!plugin.manifest.capabilities.some(capability =>
    capability.id === 'openai.oauth.outbound_transport.v1' &&
    capability.platform === 'openai' && capability.account_type === 'oauth')) {
    throw new Error('此插件未声明 OpenAI OAuth 账号目录权限。')
  }
  const loadAccounts = async () => {
    const accounts = new Map<number, { id: number; name: string; group_ids: number[] }>()
    for (let page = 1; ; page++) {
      const result = await adminAPI.accounts.list(page, 100, {
        platform: 'openai', type: 'oauth', lite: 'true',
        sort_by: 'id', sort_order: 'asc',
      })
      for (const account of result.items) {
        if (account.platform !== 'openai' || account.type !== 'oauth') continue
        // Never forward entire account DTOs: they can contain credentials/extra.
        accounts.set(account.id, { id: account.id, name: account.name,
          group_ids: [...new Set(account.group_ids || [])] })
      }
      if (page >= result.pages || !result.items.length) break
      if (page >= 1000) throw new Error('账号过多，无法完整读取目录。')
    }
    return [...accounts.values()]
  }
  const [accounts, groups, proxies] = await Promise.all([
    loadAccounts(), adminAPI.groups.getAllIncludingInactive(), adminAPI.proxies.getAll(),
  ])
  const groupIDs = new Set(accounts.flatMap(account => account.group_ids))
  return {
    accounts,
    groups: groups.filter(group => group.platform === 'openai' || groupIDs.has(group.id))
      .map(group => ({ id: group.id, name: group.name })),
    proxies: proxies.filter(proxy => proxy.status === 'active' &&
      (!proxy.expires_at || Date.parse(proxy.expires_at) > Date.now()))
      .map(proxy => ({ id: proxy.id, name: proxy.name, protocol: proxy.protocol,
        host: proxy.host, port: proxy.port })),
  }
}
