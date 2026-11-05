import type { Group, GroupPlatform } from '@/types'
import { getKeyGroupProvider } from './keyGroupProviders'

export const quickKeyPlatformBucket = (platform: GroupPlatform): string => getKeyGroupProvider(platform) === 'domestic' ? 'domestic' : platform

export function selectQuickKeyGroups(groups: Group[], defaults: Record<string, number> = {}): Group[] {
  const active = groups.filter(group => group.status === 'active')
  const buckets = new Map<string, Group>()
  for (const group of active) {
    const bucket = quickKeyPlatformBucket(group.platform)
    if (!buckets.has(bucket)) {
      buckets.set(bucket, active.find(candidate => candidate.id === defaults[bucket] && quickKeyPlatformBucket(candidate.platform) === bucket) ?? group)
    }
  }
  return [...buckets.values()]
}
