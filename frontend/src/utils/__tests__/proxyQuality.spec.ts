// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { summarizeProxyQuality } from '../proxyQuality'

describe('proxy quality presentation', () => {
  it('summarizes with challenge > failed > warn > healthy precedence', () => {
    expect(summarizeProxyQuality({ challenge_count: 1, failed_count: 1, warn_count: 1 })).toBe('challenge')
    expect(summarizeProxyQuality({ challenge_count: 0, failed_count: 2, warn_count: 1 })).toBe('failed')
    expect(summarizeProxyQuality({ challenge_count: 0, failed_count: 0, warn_count: 1 })).toBe('warn')
    expect(summarizeProxyQuality({ challenge_count: 0, failed_count: 0, warn_count: 0 })).toBe('healthy')
  })
})
