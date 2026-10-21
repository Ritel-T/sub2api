import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import InfiniteListSentinel from '../InfiniteListSentinel.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
afterEach(() => vi.unstubAllGlobals())

describe('InfiniteListSentinel', () => {
  it('loads near the viewport, pauses for errors, and only retries on user action', async () => {
    let intersect!: IntersectionObserverCallback
    const disconnect = vi.fn()
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: IntersectionObserverCallback) { intersect = callback }
      observe() {}
      disconnect = disconnect
    })
    const wrapper = mount(InfiniteListSentinel, { props: { hasMore: true } })
    intersect([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    expect(wrapper.emitted('load-more')).toHaveLength(1)
    await wrapper.setProps({ loading: true })
    await flushPromises()
    expect(wrapper.emitted('load-more')).toHaveLength(1)
    await wrapper.setProps({ loading: false, error: new Error('offline') })
    await flushPromises()
    expect(wrapper.emitted('load-more')).toHaveLength(1)
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
    expect(wrapper.emitted('load-more')).toHaveLength(1)
    await wrapper.setProps({ error: null, hasMore: false })
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('supports local prefix progress and a manual fallback without IntersectionObserver', async () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    const wrapper = mount(InfiniteListSentinel, { props: { hasMore: true, progress: 1 } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('load-more')).toHaveLength(1)
    wrapper.unmount()
  })
})
