import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, get } = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn() }))
vi.mock('axios', () => ({ default: { create: () => ({ post, get }) } }))
vi.mock('@/api/url', () => ({ buildGatewayUrl: (path: string) => path }))

import { getSetupStatus, install, testDatabase, testRedis } from '@/api/setup'

describe('setup bootstrap credential', () => {
  beforeEach(() => {
    post.mockReset().mockResolvedValue({ data: { data: { restart: true } } })
    get.mockReset().mockResolvedValue({ data: { data: { needs_setup: true } } })
  })

  it('carries the current credential only in each protected POST header', async () => {
    const database = { host: 'db', port: 5432, user: 'app', password: '', dbname: 'app', sslmode: 'require' }
    const redis = { host: 'redis', port: 6379, username: '', password: '', db: 0, enable_tls: false }
    const config = { database, redis, admin: { email: 'admin@example.com', password: 'example-password' }, server: { host: '0.0.0.0', port: 8080, mode: 'release' } }

    await testDatabase(database, 'first-bootstrap-credential')
    await testRedis(redis, 'replacement-bootstrap-credential')
    await install(config, 'replacement-bootstrap-credential')

    expect(post.mock.calls).toEqual([
      ['/setup/test-db', database, { headers: { 'X-Setup-Token': 'first-bootstrap-credential' } }],
      ['/setup/test-redis', redis, { headers: { 'X-Setup-Token': 'replacement-bootstrap-credential' } }],
      ['/setup/install', config, { headers: { 'X-Setup-Token': 'replacement-bootstrap-credential' } }],
    ])
    await getSetupStatus()
    expect(get).toHaveBeenCalledWith('/setup/status')
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })
})
