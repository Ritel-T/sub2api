import type { ApiKey } from '@/types'

export function isUsableOverviewKey(key: ApiKey, now = Date.now()): boolean {
  return key.status === 'active'
    && (!key.expires_at || new Date(key.expires_at).getTime() > now)
    && (key.quota <= 0 || key.quota_used < key.quota)
}

export function selectOverviewKey(keys: ApiKey[], now = Date.now()): ApiKey | null {
  return keys.filter(key => isUsableOverviewKey(key, now)).sort((a, b) => {
    const aTime = a.last_used_at ? new Date(a.last_used_at).getTime() : 0
    const bTime = b.last_used_at ? new Date(b.last_used_at).getTime() : 0
    return bTime - aTime || b.id - a.id
  })[0] ?? null
}
