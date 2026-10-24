// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { useAnnouncementStore } from '../announcements'
import type { UserAnnouncement } from '@/types'

const { list, markRead } = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { list, markRead } }))

const notice = (id: number): UserAnnouncement => ({
  id, title: `Notice ${id}`, content: 'Content', notify_mode: 'popup',
  created_at: '2026-09-18T00:00:00Z', updated_at: '2026-09-18T00:00:00Z'
})

const announcement = (id: number): UserAnnouncement => ({
  id, title: `Notice ${id}`, content: 'Content', notify_mode: 'silent',
  created_at: '2026-09-15T00:00:00Z', updated_at: '2026-09-15T00:00:00Z'
})

function pendingList() {
  let resolve!: (value: UserAnnouncement[]) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<UserAnnouncement[]>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())

describe('announcement fetch ownership', () => {
  it('does not restore announcements or popups after logout resets the store', async () => {
    const store = useAnnouncementStore()
    const old = pendingList()
    list.mockReturnValueOnce(old.promise)
    const request = store.fetchAnnouncements()
    store.reset()
    old.resolve([notice(1)])
    await request
    expect(store.announcements).toEqual([])
    expect(store.currentPopup).toBeNull()
    expect(store.loading).toBe(false)
  })

  it('keeps the new session loading when an old request finishes', async () => {
    const store = useAnnouncementStore()
    const old = pendingList()
    const current = pendingList()
    list.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const oldRequest = store.fetchAnnouncements()
    store.reset()
    const currentRequest = store.fetchAnnouncements()
    old.resolve([])
    await oldRequest
    expect(store.loading).toBe(true)
    current.resolve([notice(2)])
    await currentRequest
    expect(store.announcements.map(a => a.id)).toEqual([2])
    expect(store.loading).toBe(false)
  })

  it('keeps the latest forced refresh when responses arrive out of order', async () => {
    const store = useAnnouncementStore()
    const old = pendingList()
    list.mockReturnValueOnce(old.promise).mockResolvedValueOnce([notice(2)])
    const oldRequest = store.fetchAnnouncements()
    await store.fetchAnnouncements(true)
    old.resolve([notice(1)])
    await oldRequest
    expect(store.announcements.map(a => a.id)).toEqual([2])
    expect(store.currentPopup?.id).toBe(2)
  })

  it('does not clear the new session throttle when an old request rejects', async () => {
    const store = useAnnouncementStore()
    const old = pendingList()
    list.mockReturnValueOnce(old.promise).mockResolvedValue([notice(2)])
    const oldRequest = store.fetchAnnouncements()
    store.reset()
    await store.fetchAnnouncements()
    old.reject(new Error('old request failed'))
    await oldRequest
    await store.fetchAnnouncements()
    expect(list).toHaveBeenCalledTimes(2)
    expect(store.announcements.map(a => a.id)).toEqual([2])
  })

  it('still retries a failed current request', async () => {
    const store = useAnnouncementStore()
    list.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce([notice(1)])
    await store.fetchAnnouncements()
    await store.fetchAnnouncements()
    expect(list).toHaveBeenCalledTimes(2)
    expect(store.currentPopup?.id).toBe(1)
  })

  it('still throttles ordinary concurrent fetches', async () => {
    const store = useAnnouncementStore()
    const current = pendingList()
    list.mockReturnValueOnce(current.promise)
    const request = store.fetchAnnouncements()
    await store.fetchAnnouncements()
    expect(list).toHaveBeenCalledTimes(1)
    expect(store.loading).toBe(true)
    current.resolve([])
    await request
    expect(store.loading).toBe(false)
  })
})

describe('mark all announcements read', () => {
  it('keeps successful results and retries only failed announcements', async () => {
    const store = useAnnouncementStore()
    store.announcements = [announcement(1), announcement(2)]
    const error = new Error('offline')
    markRead.mockImplementation((id: number) => id === 1 ? Promise.resolve() : Promise.reject(error))
    await expect(store.markAllAsRead()).rejects.toBe(error)
    expect(store.announcements[0].read_at).toBeTruthy()
    expect(store.announcements[1].read_at).toBeFalsy()
    expect(store.unreadCount).toBe(1)
    markRead.mockClear().mockResolvedValue(undefined)
    await store.markAllAsRead()
    expect(markRead.mock.calls).toEqual([[2]])
    expect(store.unreadCount).toBe(0)
  })

  it('keeps loading until all submitted requests finish, even after a rejection', async () => {
    const store = useAnnouncementStore()
    store.announcements = [announcement(1), announcement(2)]
    let finish!: () => void
    const error = new Error('offline')
    markRead.mockImplementation((id: number) => id === 1
      ? Promise.reject(error) : new Promise<void>(resolve => { finish = resolve }))
    const result = store.markAllAsRead().catch(err => err)
    await flushPromises()
    expect(store.loading).toBe(true)
    finish()
    expect(await result).toBe(error)
    expect(store.loading).toBe(false)
    expect(store.announcements[1].read_at).toBeTruthy()
  })

  it('does not mark a newly arrived announcement that was not submitted', async () => {
    const store = useAnnouncementStore()
    store.announcements = [announcement(1)]
    let finish!: () => void
    markRead.mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    const result = store.markAllAsRead()
    store.announcements.push(announcement(2))
    finish()
    await result
    expect(markRead.mock.calls).toEqual([[1]])
    expect(store.announcements[0].read_at).toBeTruthy()
    expect(store.announcements[1].read_at).toBeFalsy()
  })
})
