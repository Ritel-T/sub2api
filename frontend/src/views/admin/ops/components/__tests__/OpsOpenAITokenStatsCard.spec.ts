import { describe, it, expect, beforeEach, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import InfiniteListSentinel from '@/components/common/InfiniteListSentinel.vue'
import DataTable from '@/components/common/DataTable.vue'
import ColumnSettings from '@/components/common/ColumnSettings.vue'
import OpsOpenAITokenStatsCard from '../OpsOpenAITokenStatsCard.vue'

const mockGetOpenAITokenStats = vi.fn()

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getOpenAITokenStats: (...args: any[]) => mockGetOpenAITokenStats(...args),
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, any>) => {
        if (key === 'admin.ops.openaiTokenStats.pageInfo' && params) {
          return `第 ${params.page}/${params.total} 页`
        }
        return key
      },
    }),
  }
})

const SelectStub = defineComponent({
  name: 'SelectControlStub',
  props: {
    modelValue: {
      type: [String, Number],
      default: '',
    },
  },
  emits: ['update:modelValue'],
  template: '<div class="select-stub" />',
})

const EmptyStateStub = defineComponent({
  name: 'EmptyState',
  props: {
    title: { type: String, default: '' },
    description: { type: String, default: '' },
  },
  template: '<div class="empty-state">{{ title }}|{{ description }}</div>',
})

const sampleResponse = {
  time_range: '30d' as const,
  start_time: '2026-01-01T00:00:00Z',
  end_time: '2026-01-31T00:00:00Z',
  platform: 'openai',
  group_id: 7,
  items: [
    {
      model: 'gpt-4o-mini',
      request_count: 12,
      avg_tokens_per_sec: 22.5,
      avg_first_token_ms: 123.45,
      total_output_tokens: 1234,
      avg_duration_ms: 321,
      requests_with_first_token: 10,
    },
  ],
  total: 40,
  page: 1,
  page_size: 20,
  top_n: null,
}

describe('OpsOpenAITokenStatsCard', () => {
  beforeEach(() => {
    localStorage.removeItem('admin-ops-openai-token-stats')
    vi.clearAllMocks()
    mockGetOpenAITokenStats.mockReset()
  })

  it('keeps Top N bounded and appends all-model batches without losing precision or publishing stale filters', async () => {
    mockGetOpenAITokenStats.mockResolvedValue({ ...sampleResponse, total: 3 })
    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: { refreshToken: 0 }, global: { stubs: { Select: SelectStub, EmptyState: EmptyStateStub } },
    })
    await flushPromises()
    expect(wrapper.getComponent(InfiniteListSentinel).props('hasMore')).toBe(false)
    await wrapper.findAllComponents(SelectStub)[1].vm.$emit('update:modelValue', 'pagination')
    await flushPromises()
    mockGetOpenAITokenStats.mockResolvedValueOnce({ ...sampleResponse, items: [{ ...sampleResponse.items[0], model: 'gpt-5.4', avg_tokens_per_sec: 12.3456 }], total: 3 })
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    expect(wrapper.text()).toContain('gpt-4o-mini')
    expect(wrapper.text()).toContain('gpt-5.4')
    expect(wrapper.text()).toContain('12.35')
    let complete!: (value: unknown) => void
    mockGetOpenAITokenStats.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    const signal = mockGetOpenAITokenStats.mock.calls.at(-1)![1].signal as AbortSignal
    mockGetOpenAITokenStats.mockResolvedValueOnce({ ...sampleResponse, items: [{ ...sampleResponse.items[0], model: 'claude' }], total: 1 })
    await wrapper.setProps({ platformFilter: 'anthropic' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    complete({ ...sampleResponse, items: [{ ...sampleResponse.items[0], model: 'obsolete' }], total: 3 })
    await flushPromises()
    expect(wrapper.text()).toContain('claude')
    expect(wrapper.text()).not.toContain('obsolete')
    expect(wrapper.text()).not.toContain('gpt-4o-mini')
    expect(wrapper.getComponent(InfiniteListSentinel).props('hasMore')).toBe(false)
    wrapper.unmount()
  })

  it('默认不限定平台并展示各平台模型的统计', async () => {
    const models = ['gpt-4o-mini', 'o3', 'claude-sonnet-4-5', 'gemini-2.5-pro']
    mockGetOpenAITokenStats.mockResolvedValue({
      ...sampleResponse,
      platform: '',
      group_id: null,
      items: models.map(model => ({ ...sampleResponse.items[0], model })),
      total: models.length,
    })

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: { refreshToken: 0 },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })
    await flushPromises()

    expect(mockGetOpenAITokenStats).toHaveBeenCalledTimes(1)
    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith({
      time_range: '30d',
      platform: undefined,
      group_id: undefined,
      top_n: 20,
    }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    for (const model of models) {
      expect(wrapper.text()).toContain(model)
    }
  })

  it.each([
    ['anthropic', 'claude-sonnet-4-5'],
    ['gemini', 'gemini-2.5-pro'],
  ])('切换至 %s 平台后刷新统计并重置分页', async (platform, model) => {
    mockGetOpenAITokenStats.mockImplementation(async (params: Record<string, any>) => ({
      ...sampleResponse,
      platform: params.platform,
      page: params.page ?? 1,
      items: [{
        ...sampleResponse.items[0],
        model: params.platform === platform ? model : 'gpt-4o-mini',
      }],
    }))

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: { platformFilter: 'openai', groupIdFilter: 7, refreshToken: 0 },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })
    await flushPromises()
    await wrapper.findAllComponents(SelectStub)[1].vm.$emit('update:modelValue', 'pagination')
    await flushPromises()
    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()
    expect(mockGetOpenAITokenStats).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }), expect.objectContaining({ signal: expect.any(AbortSignal) }))

    mockGetOpenAITokenStats.mockClear()
    await wrapper.setProps({ platformFilter: platform })
    await flushPromises()

    expect(mockGetOpenAITokenStats).toHaveBeenCalledTimes(1)
    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith({
      time_range: '30d',
      platform,
      group_id: 7,
      page: 1,
      page_size: 20,
    }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain(model)
    expect(wrapper.text()).not.toContain('gpt-4o-mini')
  })

  it('默认加载并透传 platform/group 过滤，支持时间窗口切换', async () => {
    mockGetOpenAITokenStats.mockResolvedValue(sampleResponse)

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: {
        platformFilter: 'openai',
        groupIdFilter: 7,
        refreshToken: 0,
      },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })

    await flushPromises()
    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith(
      expect.objectContaining({
        time_range: '30d',
        platform: 'openai',
        group_id: 7,
        top_n: 20,
      }), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    const selects = wrapper.findAllComponents(SelectStub)
    await selects[0].vm.$emit('update:modelValue', '1h')
    await flushPromises()

    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith(
      expect.objectContaining({
        time_range: '1h',
        platform: 'openai',
        group_id: 7,
      }), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('支持分页与 TopN 模式切换并按参数请求', async () => {
    mockGetOpenAITokenStats.mockImplementation(async (params: Record<string, any>) => ({
      ...sampleResponse,
      time_range: params.time_range ?? '30d',
      page: params.page ?? 1,
      page_size: params.page_size ?? 20,
      top_n: params.top_n ?? null,
      total: 40,
    }))

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: {
        refreshToken: 0,
      },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })
    await flushPromises()

    let selects = wrapper.findAllComponents(SelectStub)
    await selects[1].vm.$emit('update:modelValue', 'pagination')
    await flushPromises()

    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith(
      expect.objectContaining({
        page: 1,
        page_size: 20,
      }), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more')
    await flushPromises()

    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith(
      expect.objectContaining({
        page: 2,
        page_size: 20,
      }), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    selects = wrapper.findAllComponents(SelectStub)
    await selects[1].vm.$emit('update:modelValue', 'topn')
    await flushPromises()
    selects = wrapper.findAllComponents(SelectStub)
    await selects[2].vm.$emit('update:modelValue', 50)
    await flushPromises()

    expect(mockGetOpenAITokenStats).toHaveBeenCalledWith(
      expect.objectContaining({
        top_n: 50,
      }), expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('接口返回空数据时显示空态', async () => {
    mockGetOpenAITokenStats.mockResolvedValue({
      ...sampleResponse,
      items: [],
      total: 0,
    })

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: { refreshToken: 0 },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('.empty-state').exists()).toBe(true)
  })

  it('数据表默认保全字段，用户移入展开明细的额外统计保持可达', async () => {
    mockGetOpenAITokenStats.mockResolvedValue(sampleResponse)

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: { refreshToken: 0 },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('.max-h-\\[420px\\]').exists()).toBe(false)
    const table = wrapper.getComponent(DataTable)
    expect(table.props('columns')).toHaveLength(7)
    expect(table.props('columns').every(column => column.primary)).toBe(true)
    table.getComponent(ColumnSettings).vm.$emit('toggle', 'avg_duration_ms')
    table.getComponent(ColumnSettings).vm.$emit('toggle', 'requests_with_first_token')
    await flushPromises()
    await wrapper.get('[data-test="toggle-details"]').trigger('click')
    expect(wrapper.get('[data-field="avg_duration_ms"] dd').text()).not.toBe('')
    expect(wrapper.get('[data-field="requests_with_first_token"] dd').text()).not.toBe('')
  })

  it('接口异常时显示错误提示', async () => {
    mockGetOpenAITokenStats.mockRejectedValue(new Error('加载失败'))

    const wrapper = mount(OpsOpenAITokenStatsCard, {
      props: { refreshToken: 0 },
      global: {
        stubs: {
          Select: SelectStub,
          EmptyState: EmptyStateStub,
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('加载失败')
  })
  it('windows all-model mobile history after six batches while keeping all seven fields reachable', async () => {
    const media = vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => ({ matches: false, media: query, onchange: null, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn() }))
    const rows = Array.from({ length: 120 }, (_, index) => ({ ...sampleResponse.items[0], model: `model-${index}` }))
    mockGetOpenAITokenStats.mockImplementation(async (query: Record<string, number>) => ({ ...sampleResponse, items: query.top_n ? rows.slice(0, query.top_n) : rows.slice(((query.page ?? 1) - 1) * 20, (query.page ?? 1) * 20), total: 120 }))
    const wrapper = mount(OpsOpenAITokenStatsCard, { attachTo: document.body, props: { refreshToken: 0 }, global: { stubs: { Select: SelectStub, EmptyState: EmptyStateStub } } })
    try {
      await flushPromises()
      wrapper.findAllComponents(SelectStub)[1].vm.$emit('update:modelValue', 'pagination'); await flushPromises()
      for (let page = 2; page <= 6; page++) { wrapper.getComponent(InfiniteListSentinel).vm.$emit('load-more'); await flushPromises() }
      const table = wrapper.getComponent(DataTable)
      expect(table.props('data')).toHaveLength(120)
      expect(table.props('columnOrderKey')).toBe('admin-ops-openai-token-stats')
      const rendered = wrapper.findAll('.table-mobile-records [data-index]')
      expect(rendered.length).toBeGreaterThan(0); expect(rendered.length).toBeLessThan(40)
      const first = wrapper.get('.table-mobile-records [data-index="0"]')
      for (const column of table.props('columns')) expect(first.get(`[data-field="${column.key}"]`).text()).toContain(column.label)
      expect(first.get('[data-field="avg_duration_ms"]').text()).toContain('321')
      expect(first.get('[data-field="requests_with_first_token"]').text()).toContain('10')
      expect(table.props('columns').every(column => !column.sortable)).toBe(true)
    } finally { wrapper.unmount(); media.mockRestore() }
  })

  it('keeps initial loading separate from the empty result and shared table', async () => {
    let complete!: (value: unknown) => void
    mockGetOpenAITokenStats.mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = mount(OpsOpenAITokenStatsCard, { props: { refreshToken: 0 }, global: { stubs: { Select: SelectStub, EmptyState: EmptyStateStub } } })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toBe('admin.ops.loadingText')
    expect(wrapper.findComponent(DataTable).exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(false)
    complete({ ...sampleResponse, items: [], total: 0 }); await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    wrapper.unmount()
  })

})
