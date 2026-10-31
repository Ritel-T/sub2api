import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createStartupRecovery } from '../startupRecovery'

const controllers: ReturnType<typeof createStartupRecovery>[] = []
const settle = async () => { await vi.advanceTimersByTimeAsync(0) }
function start(finish: (whenActive: () => Promise<void>) => Promise<void>) {
  const root = document.createElement('div')
  document.body.append(root)
  const recovery = createStartupRecovery({ root, locale: 'zh', finish })
  controllers.push(recovery)
  return { root, ...recovery }
}
function visible(value: boolean) {
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue(value ? 'visible' : 'hidden')
  document.dispatchEvent(new Event('visibilitychange'))
}
function online(value: boolean) {
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(value)
  window.dispatchEvent(new Event(value ? 'online' : 'offline'))
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
})
afterEach(() => {
  controllers.splice(0).forEach(controller => controller.dispose())
  document.body.replaceChildren()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('startup recovery', () => {
  it('shows a neutral branded shell and coalesces manual/online attempts until mounting', async () => {
    let resolve!: () => void
    const finish = vi.fn(() => new Promise<void>(done => { resolve = done }))
    const recovery = start(finish)
    expect(recovery.root.textContent).toContain('Ritel.SI')
    expect(recovery.root.textContent).toContain('正在连接')
    expect(recovery.root.querySelector('button')!.hidden).toBe(true)
    const first = recovery.retry()
    expect(recovery.retry()).toBe(first)
    online(true)
    await settle()
    expect(finish).toHaveBeenCalledTimes(1)
    resolve()
    await first
    online(true)
    await recovery.retry()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(finish).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps the same failure region during quiet retries and mounts after a 502 recovers', async () => {
    let resolve!: () => void
    const finish = vi.fn()
      .mockRejectedValueOnce(new Error('502 private session details'))
      .mockImplementationOnce(() => new Promise<void>(done => { resolve = done }))
    const recovery = start(finish)
    await recovery.retry()
    const status = recovery.root.querySelector('p')!
    const button = recovery.root.querySelector('button')!
    expect(status.textContent).toContain('连接暂时不稳定')
    expect(recovery.root.textContent).not.toContain('private session')
    await vi.advanceTimersByTimeAsync(29_999)
    expect(finish).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(finish).toHaveBeenCalledTimes(2)
    expect(recovery.root.querySelector('p')).toBe(status)
    expect(recovery.root.querySelector('button')).toBe(button)
    expect(button.hidden).toBe(false)
    expect(status.textContent).toContain('正在重试')
    resolve()
    await settle()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('allows same-page manual retry immediately and schedules only one slow retry after failure', async () => {
    const finish = vi.fn().mockRejectedValue(new Error('unavailable'))
    const recovery = start(finish)
    await recovery.retry()
    recovery.root.querySelector('button')!.click()
    await settle()
    expect(finish).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(finish).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(1)
  })

  it('defers hidden/offline work and resumes when visible and online', async () => {
    visible(false)
    const finish = vi.fn().mockRejectedValue(new Error('unavailable'))
    const recovery = start(finish)
    await recovery.retry()
    online(true)
    await settle()
    expect(finish).not.toHaveBeenCalled()
    visible(true)
    await settle()
    expect(finish).toHaveBeenCalledTimes(1)
    visible(false)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(finish).toHaveBeenCalledTimes(1)
    online(false)
    visible(true)
    await settle()
    expect(finish).toHaveBeenCalledTimes(1)
    online(true)
    await settle()
    expect(finish).toHaveBeenCalledTimes(2)
  })

  it('pauses between async startup stages if the tab hides, then cleans listeners on success', async () => {
    let release!: () => void
    const hydrated = new Promise<void>(done => { release = done })
    const mount = vi.fn()
    const finish = vi.fn().mockRejectedValueOnce(new Error('502')).mockImplementationOnce(async whenActive => {
      await hydrated
      await whenActive()
      mount()
    })
    const recovery = start(finish)
    await recovery.retry()
    const button = recovery.root.querySelector('button')!
    const attempt = recovery.retry(true)
    await settle()
    visible(false)
    release()
    await settle()
    expect(mount).not.toHaveBeenCalled()
    expect(button.hidden).toBe(false)
    visible(true)
    expect(button.hidden).toBe(false)
    await attempt
    online(false)
    expect(mount).toHaveBeenCalledTimes(1)
    expect(recovery.root.textContent).not.toContain('正在等待网络')
  })
})
