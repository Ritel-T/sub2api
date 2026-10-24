// @vitest-environment node
import { describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { useConfirmation, useConfirmedAction } from '../useConfirmation'

function deferred() {
  let resolve!: () => void
  let reject!: (error: Error) => void
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

function setup() {
  const scope = effectScope()
  const identity = ref('A')
  const active = ref(true)
  const confirmation = scope.run(() => useConfirmation(() => ({ active: active.value, identity: identity.value })))!
  return { scope, identity, active, confirmation }
}

describe('useConfirmation lifecycle', () => {
  it('preserves string requests and resets intent options for each opening', async () => {
    const { confirmation: c, scope } = setup()
    const first = c.request({ message: 'Delete A?', title: 'Delete', confirmText: 'Delete A', cancelText: 'Keep', danger: true })
    expect([c.message.value, c.title.value, c.confirmText.value, c.cancelText.value, c.danger.value]).toEqual(['Delete A?', 'Delete', 'Delete A', 'Keep', true])
    c.settle(false)
    expect(await first).toBe(false)
    const next = c.request('Ordinary warning')
    expect(c.title.value).toBeUndefined()
    expect(c.confirmText.value).toBeUndefined()
    expect(c.cancelText.value).toBeUndefined()
    expect(c.danger.value).toBe(false)
    c.settle(true)
    expect(await next).toBe(true)
    scope.stop()
  })

  it('does not let a second request replace the pending target or message', async () => {
    const { confirmation: c, scope } = setup()
    const first = c.request('A')
    expect(await c.request('B')).toBe(false)
    expect(c.message.value).toBe('A')
    c.settle(true)
    expect(await first).toBe(true)
    scope.stop()
  })

  it('rejects confirmation if identity changes before or just after accept', async () => {
    const { confirmation: c, identity, scope } = setup()
    const first = c.request('A')
    identity.value = 'B'
    expect(c.show.value).toBe(false)
    c.settle(true)
    expect(await first).toBe(false)
    const second = c.request('B')
    c.settle(true)
    identity.value = 'C'
    expect(await second).toBe(false)
    scope.stop()
  })

  it('locks synchronously through the action and ignores repeated decisions', async () => {
    const { confirmation: c, scope } = setup()
    const gate = deferred()
    const action = vi.fn(() => gate.promise)
    const result = c.run({ message: 'A' }, action)
    c.settle(true)
    c.settle(true)
    c.settle(false)
    expect(action).toHaveBeenCalledTimes(1)
    expect(c.busy.value).toBe(true)
    expect(c.show.value).toBe(true)
    expect(await c.run({ message: 'B' }, action)).toBe(false)
    gate.resolve()
    expect(await result).toBe(true)
    expect(c.busy.value).toBe(false)
    expect(c.show.value).toBe(false)
    scope.stop()
  })

  it('invalidates pending action UI effects while keeping its execution lock', async () => {
    const { confirmation: c, identity, scope } = setup()
    const gate = deferred()
    const applied: string[] = []
    const result = c.run({ message: 'A' }, async isCurrent => {
      await gate.promise
      if (isCurrent()) applied.push('A')
    })
    c.settle(true)
    identity.value = 'B'
    expect(await result).toBe(false)
    expect(c.show.value).toBe(false)
    expect(c.busy.value).toBe(true)
    expect(await c.request('B')).toBe(false)
    gate.resolve()
    await gate.promise
    await Promise.resolve()
    expect(applied).toEqual([])
    expect(c.busy.value).toBe(false)
    const next = c.request('B')
    c.settle(true)
    expect(await next).toBe(true)
    scope.stop()
  })

  it('closes and propagates current errors, then permits explicit retry', async () => {
    const { confirmation: c, scope } = setup()
    const error = new Error('Network failed')
    const action = vi.fn().mockRejectedValueOnce(error).mockResolvedValueOnce(undefined)
    const result = c.run({ message: 'A' }, action)
    const rejected = expect(result).rejects.toBe(error)
    c.settle(true)
    await rejected
    expect(c.busy.value).toBe(false)
    expect(c.show.value).toBe(false)
    const retry = c.run({ message: 'A' }, action)
    c.settle(true)
    expect(await retry).toBe(true)
    expect(action).toHaveBeenCalledTimes(2)
    scope.stop()
  })

  it('settles on parent close or disposal and ignores stale rejection', async () => {
    const { confirmation: c, active, scope } = setup()
    const gate = deferred()
    const result = c.run({ message: 'A' }, () => gate.promise)
    c.settle(true)
    active.value = false
    scope.stop()
    expect(await result).toBe(false)
    gate.reject(new Error('Late response'))
    await gate.promise.catch(() => {})
    await Promise.resolve()
    expect(c.busy.value).toBe(false)
    expect(await c.request('B')).toBe(false)
  })
})

describe('useConfirmedAction', () => {
  it('protects direct dialog handlers from repeat execution and stale effects', async () => {
    const scope = effectScope()
    const identity = ref('A')
    const action = scope.run(() => useConfirmedAction(() => ({ active: true, identity: identity.value })))!
    const gate = deferred()
    const applied = vi.fn()
    const run = action.run(async isCurrent => {
      await gate.promise
      if (isCurrent()) applied()
    })
    expect(action.busy.value).toBe(true)
    identity.value = 'B'
    expect(await action.run(applied)).toBe(false)
    expect(action.busy.value).toBe(true)
    gate.resolve()
    expect(await run).toBe(false)
    expect(applied).not.toHaveBeenCalled()
    expect(action.busy.value).toBe(false)
    expect(await action.run(applied)).toBe(true)
    scope.stop()
  })

  it('releases the lock after a synchronous error and honors explicit invalidation/disposal', async () => {
    const scope = effectScope()
    const action = scope.run(() => useConfirmedAction())!
    const error = new Error('Failure')
    await expect(action.run(() => { throw error })).rejects.toBe(error)
    expect(action.busy.value).toBe(false)
    const gate = deferred()
    const run = action.run(() => gate.promise)
    action.invalidate()
    gate.reject(error)
    expect(await run).toBe(false)
    scope.stop()
    expect(await action.run(vi.fn())).toBe(false)
  })
})
