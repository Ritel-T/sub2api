import { afterEach, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import HarvestNodeRecords from '@/components/admin/HarvestNodeRecords.vue'
const state = vi.hoisted(() => ({ rows: null as any, api: vi.fn() }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key, te: () => false, locale: { value: 'en' } }) }))
vi.mock('@/api/admin/codexHarvest', () => ({ getCodexHarvestNodes: (...args: any[]) => state.api(...args), resetCodexHarvestNodes: vi.fn() }))
vi.mock('@/composables/useDocumentRows', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/composables/useDocumentRows')>()
  return { useDocumentRows: (...args: Parameters<typeof original.useDocumentRows>) => (state.rows = original.useDocumentRows(...args)) }
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })
it('renders the gap before the visible range while retaining an earlier focused row', async () => {
  const frames = new Map<number, FrameRequestCallback>(); let serial = 0
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { frames.set(++serial, cb); return serial })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  vi.stubGlobal('scrollTo', vi.fn()); vi.stubGlobal('scrollY', 0); vi.stubGlobal('innerHeight', 400)
  const records = Array.from({ length: 120 }, (_, i) => ({ id: i + 1, node_name: `Audit node ${i+1}`, provider: 'openai', account_id: i, model: 'audit', successes: 1, misses: 0, network_errors: 0, account_errors: 0, last_result: 'success', latency_ms: 100 }))
  state.api.mockImplementation(async (offset: number, limit: number) => ({ items: records.slice(offset, offset + limit), total: records.length }))
  const wrapper = mount(HarvestNodeRecords, { attachTo: document.body })
  const body = wrapper.get('.divide-y').element as HTMLElement
  body.getBoundingClientRect = () => ({ top: 1000-window.scrollY, bottom: 1000-window.scrollY+120*124, height:120*124, width:800, x:0,y:0,left:0,right:800,toJSON(){} })
  const settle = async () => {
    for (let i = 0; i < 30; i++) {
      await flushPromises(); if (!frames.size) return
      const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(cb => cb(0))
    }
    throw new Error('frames did not settle')
  }
  try {
    await settle()
    for (let page=1; page<15; page++) { await wrapper.get('.infinite-list-action').trigger('click'); await settle() }
    vi.stubGlobal('scrollY', 1000); window.dispatchEvent(new Event('scroll')); await settle()
    expect(wrapper.find('[data-index="0"] button').exists()).toBe(true)
    ;(wrapper.get('[data-index="0"] button').element as HTMLButtonElement).focus()
    vi.stubGlobal('scrollY', 1000+80*124); window.dispatchEvent(new Event('scroll')); await settle()
    const rendered = state.rows.rendered.value
    expect(rendered[0].index).toBe(0)
    expect(rendered.some((r: any) => r.index === 80)).toBe(true)
    const gap = rendered.find((r: any) => r.gapBefore > 0)
    expect(gap.gapBefore).toBeGreaterThan(5000)
    expect(wrapper.findAll('.divide-y > [aria-hidden="true"]').map(e => e.attributes('style')).some(s => s?.includes(`${gap.gapBefore}px`))).toBe(true)
    expect(document.activeElement).toBe(wrapper.get('[data-index="0"] button').element)
    expect(wrapper.findAll('article').length).toBeLessThan(40)
  } finally { wrapper.unmount() }
})