import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let change: (() => void) | undefined
let storage: ((event: StorageEvent) => void) | undefined
let media: { matches: boolean; addEventListener: ReturnType<typeof vi.fn> }

beforeEach(() => {
  vi.resetModules()
  localStorage.clear()
  document.documentElement.className = ''
  document.head.innerHTML = '<meta name="theme-color" content="">'
  media = { matches: false, addEventListener: vi.fn((_type, listener) => { change = listener }) }
  vi.stubGlobal('matchMedia', vi.fn(() => media))
  const originalAdd = window.addEventListener.bind(window)
  vi.spyOn(window, 'addEventListener').mockImplementation((type, listener, options) => {
    if (type === 'storage') storage = listener as (event: StorageEvent) => void
    else originalAdd(type, listener, options)
  })
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('theme preferences', () => {
  it('defaults to the system theme and follows subsequent system changes', async () => {
    const { useTheme } = await import('../useTheme')
    const theme = useTheme()
    expect(theme.preference.value).toBe('system')
    expect(theme.resolvedTheme.value).toBe('light')
    media.matches = true
    change?.()
    expect(theme.resolvedTheme.value).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#1b1b19')
  })

  it('supports legacy media query listeners without addEventListener', async () => {
    const legacy = { matches: false, addListener: vi.fn((listener: () => void) => { change = listener }) }
    vi.stubGlobal('matchMedia', vi.fn(() => legacy))
    const { useTheme } = await import('../useTheme')
    const theme = useTheme()
    legacy.matches = true
    change?.()
    expect(legacy.addListener).toHaveBeenCalledTimes(1)
    expect(theme.resolvedTheme.value).toBe('dark')
  })

  it('restores an explicit preference before mounting and ignores system changes', async () => {
    localStorage.setItem('ritel-theme', 'dark')
    const { useTheme } = await import('../useTheme')
    const theme = useTheme()
    expect(theme.resolvedTheme.value).toBe('dark')
    change?.()
    expect(theme.resolvedTheme.value).toBe('dark')
    theme.setTheme('light')
    expect(localStorage.getItem('ritel-theme')).toBe('light')
    media.matches = true
    change?.()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(document.documentElement.style.colorScheme).toBe('light')
    theme.setTheme('system')
    expect(theme.resolvedTheme.value).toBe('dark')
  })

  it('shares one preference and one system listener between mounted controls', async () => {
    const { useTheme } = await import('../useTheme')
    const first = useTheme()
    const second = useTheme()
    first.setTheme('dark')
    expect(second.preference.value).toBe('dark')
    expect(media.addEventListener).toHaveBeenCalledTimes(1)
  })

  it('syncs other tabs and resets to system when the stored preference is removed', async () => {
    const { useTheme } = await import('../useTheme')
    const theme = useTheme()
    storage?.({ key: 'other-key', newValue: 'dark' } as StorageEvent)
    expect(theme.preference.value).toBe('system')
    storage?.({ key: 'ritel-theme', newValue: 'dark' } as StorageEvent)
    expect(theme.resolvedTheme.value).toBe('dark')
    storage?.({ key: 'ritel-theme', newValue: null } as StorageEvent)
    expect(theme.preference.value).toBe('system')
    storage?.({ key: null, newValue: null } as StorageEvent)
    expect(theme.resolvedTheme.value).toBe('light')
  })

  it('keeps switching usable when browser storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('Blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('Blocked') })
    const { useTheme } = await import('../useTheme')
    const theme = useTheme()
    expect(theme.preference.value).toBe('system')
    expect(() => theme.setTheme('dark')).not.toThrow()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('treats unknown stored preferences as system', async () => {
    localStorage.setItem('ritel-theme', 'unexpected')
    media.matches = true
    const { useTheme } = await import('../useTheme')
    expect(useTheme().preference.value).toBe('system')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
