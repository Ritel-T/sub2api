import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import IQTestModal from '../IQTestModal.vue'
import TestModelSelect from '../TestModelSelect.vue'

const { getAvailableModels, getModelReasoning, probeOpenAICodexState } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(), getModelReasoning: vi.fn(), probeOpenAICodexState: vi.fn()
}))

vi.mock('@/api/admin/accounts', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/accounts')>('@/api/admin/accounts')
  return { ...actual, getAvailableModels, getModelReasoning, probeOpenAICodexState }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function streamResponse(events: Array<Record<string, unknown>>) {
  const encoder = new TextEncoder()
  const chunks = events.map((event) => encoder.encode(`data: ${JSON.stringify(event)}\n\n`))
  let index = 0
  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn(async () => index < chunks.length
          ? { done: false, value: chunks[index++] }
          : { done: true, value: undefined })
      })
    }
  } as Response
}

function mountModal(account: Record<string, unknown> = {}) {
  return mount(IQTestModal, {
    props: {
      show: true,
      account: {
        id: 42,
        name: 'Astra account',
        platform: 'openai',
        type: 'oauth',
        status: 'active',
        ...account
      } as any
    },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Input: true,
        TextArea: true,
        Select: true,
        Icon: true
      }
    }
  })
}

// Capability lookups are part of opening the dialog. Never make real Admin API requests.
beforeEach(() => {
  getAvailableModels.mockReset().mockResolvedValue([
    { id: 'gpt-6-astra', display_name: 'Astra' },
    { id: 'gpt-6.1-sol', display_name: 'Sol' },
    { id: 'claude-opus-5-5', display_name: 'Claude' }
  ])
  getModelReasoning.mockReset().mockResolvedValue({ supported_reasoning_levels: ['low', 'medium', 'high'], default_reasoning_level: 'medium' })
})

describe('IQTestModal', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('auth_token', 'test-token')
    global.fetch = vi.fn(() => Promise.resolve(streamResponse([
      { type: 'test_start', model: 'gpt-6-astra' },
      { type: 'content', text: '<!doctype html><html><head><title>Pelican</title></head><body><svg></svg>' },
      { type: 'content', text: '<script>document.body.dataset.animated="true"</script></body></html>' },
      { type: 'test_complete', success: true }
    ]))) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('keeps all eight configured model IDs when discovery only returns six', async () => {
    const ids = ['codex-auto-review', 'gpt-5.5', 'gpt-5.6', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-6-astra', 'gpt-6-sol', 'gpt-6.1-sol']
    getAvailableModels.mockResolvedValue(ids.filter(id => !['gpt-5.6-terra', 'gpt-6.1-sol'].includes(id))
      .map(id => ({ id, display_name: id.toUpperCase() })))
    const wrapper = mountModal({ credentials: { model_mapping: { ...Object.fromEntries(ids.map(id => [id, id])), 'custom-*': 'upstream-target' } } })
    await flushPromises()
    const picker = wrapper.getComponent(TestModelSelect)
    expect(picker.props('options').map(option => option.value).sort()).toEqual([...ids].sort())
    expect(picker.props('options').map(option => option.label)).not.toContain('upstream-target')
    picker.vm.$emit('update:modelValue', 'gpt-6.1-sol')
    await flushPromises()
    await (wrapper.vm as any).startTest()
    expect(JSON.parse((global.fetch as any).mock.calls[0][1].body).model_id).toBe('gpt-6.1-sol')
    wrapper.unmount()
  })

  it('submits a manually entered model without discovery overwriting it', async () => {
    let finishDiscovery!: (models: Array<{ id: string }>) => void
    getAvailableModels.mockReturnValue(new Promise(resolve => { finishDiscovery = resolve }))
    const wrapper = mountModal()
    await wrapper.get('[data-testid="model-input-toggle"]').trigger('click')
    await wrapper.get('[data-testid="manual-model-input"]').setValue('gpt-6.1-sol')
    finishDiscovery([{ id: 'gpt-5.5' }])
    await flushPromises()
    expect((wrapper.get('[data-testid="manual-model-input"]').element as HTMLInputElement).value).toBe('gpt-6.1-sol')
    await (wrapper.vm as any).startTest()
    expect(JSON.parse((global.fetch as any).mock.calls[0][1].body).model_id).toBe('gpt-6.1-sol')
    expect(getModelReasoning).toHaveBeenLastCalledWith(42, 'gpt-6.1-sol')
    wrapper.unmount()
  })

  it('retains configured choices when discovery fails', async () => {
    getAvailableModels.mockRejectedValue(new Error('discovery unavailable'))
    const wrapper = mountModal({ credentials: { model_mapping: { 'gpt-6.1-sol': 'gpt-6.1-sol' } } })
    await flushPromises()
    expect(wrapper.getComponent(TestModelSelect).props('options')).toEqual([{ value: 'gpt-6.1-sol', label: 'gpt-6.1-sol' }])
    wrapper.unmount()
  })

  it('uses the dedicated endpoint and sends identical settings to parallel runs', async () => {
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectQuestion('pelican')
    ;(wrapper.vm as any).parallelCount = 2
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(2)
    for (const [url, request] of (global.fetch as any).mock.calls) {
      expect(url).toContain('/admin/accounts/42/pelican-test')
      const body = JSON.parse(request.body)
      expect(body).toMatchObject({
        model_id: 'gpt-6-astra',
        mode: 'default',
        reasoning_effort: 'medium'
      })
      expect(body.prompt).toContain('SVG 绘制一个鹈鹕骑自行车的 2D 动画')
      expect(body.prompt).toContain('直接返回独立 HTML')
      expect(body.prompt).not.toContain('不要有任何限制')
    }

    const frames = wrapper.findAll('iframe')
    expect(frames).toHaveLength(2)
    expect(frames[0].attributes('srcdoc')).toContain('Content-Security-Policy')
    expect(frames[0].attributes('srcdoc')).toContain('<svg></svg>')
    expect(wrapper.text()).toContain('admin.accounts.pelicanTest.success')
    expect(localStorage.getItem('sub2api-pelican-test:42')).toContain('gpt-6-astra')
  })

  it('starts Claude accounts on a Claude model and other accounts on the OpenAI default', async () => {
    const wrapper = mountModal({ platform: 'anthropic', name: 'Claude account' })
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const body = JSON.parse((global.fetch as any).mock.calls[0][1].body)
    expect(body.model_id).toBe('claude-opus-5-5')

    await wrapper.setProps({ account: { id: 43, name: 'Astra account', platform: 'openai', type: 'oauth', status: 'active' } as any })
    expect((wrapper.vm as any).modelId).toBe('gpt-6-astra')
  })

  it('keeps non-HTML output visible but marks it as failed', async () => {
    global.fetch = vi.fn(() => Promise.resolve(streamResponse([
      { type: 'content', text: 'I cannot provide HTML.' },
      { type: 'test_complete', success: true }
    ]))) as any
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectQuestion('pelican')
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('I cannot provide HTML.')
    expect(wrapper.text()).toContain('admin.accounts.pelicanTest.failed')
  })
  it('persists manual timing and model snapshots independently of later form edits', async () => {
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectQuestion('pelican')
    await (wrapper.vm as any).startTest()
    const saved = JSON.parse(localStorage.getItem('sub2api-pelican-test:42')!)[0]
    expect(saved.runs[0]).toMatchObject({ source: 'manual', modelId: 'gpt-6-astra', reasoningEffort: 'medium' })
    expect(Number.isFinite(Date.parse(saved.runs[0].startedAt))).toBe(true)
    expect(saved.runs[0].durationMs).toBeGreaterThanOrEqual(0)
    ;(wrapper.vm as any).modelId = 'changed-model'
    await flushPromises()
    const metadata = wrapper.get('[data-testid="run-metadata"]').text()
    expect(metadata).toContain('gpt-6-astra')
    expect(metadata).not.toContain('changed-model')
    expect(metadata).toContain('admin.accounts.pelicanTest.sourceManual')
    wrapper.unmount()
  })

  it('shows server timing and saved settings when previewing a scheduled output', async () => {
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectQuestion('pelican')
    ;(wrapper.vm as any).previewScheduled({ id: 9, status: 'success', response_text: '<html><body>pelican</body></html>', error_message: '', started_at: '2026-09-23T11:32:30Z', finished_at: '2026-09-23T11:34:42Z', latency_ms: 132100, pelican_config: { prompt: 'pelican', model_id: 'saved-model', reasoning_effort: 'high', parallel_count: 1 } })
    await flushPromises()
    const metadata = wrapper.get('[data-testid="run-metadata"]').text()
    expect(metadata).toContain('admin.accounts.pelicanTest.sourceScheduled')
    expect(metadata).toContain('saved-model / high')
    expect(metadata).toContain('132.1 s')
    expect(metadata).toContain('admin.accounts.pelicanTest.generatedAt')
    wrapper.unmount()
  })

})


describe('Intelligence question selection', () => {
  it('defaults to candy and accepts plain text without an HTML contract', async () => {
    global.fetch = vi.fn(() => Promise.resolve(streamResponse([
      { type: 'content', text: '21' }, { type: 'test_complete', success: true }
    ]))) as any
    const wrapper = mountModal()
    expect((wrapper.vm as any).questionKind).toBe('candy')
    await (wrapper.vm as any).startTest()
    await flushPromises()
    const body = JSON.parse((global.fetch as any).mock.calls[0][1].body)
    expect(body.prompt).toContain('圆形 7 9 8')
    expect(body.prompt).toContain('只输出最终整数')
    expect(body.prompt).not.toContain('独立 HTML')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('21')
    expect((wrapper.vm as any).runs[0].status).toBe('success')
    expect((wrapper.vm as any).records[0].questionKind).toBe('candy')
    ;(wrapper.vm as any).selectQuestion('pelican')
    ;(wrapper.vm as any).loadRecord((wrapper.vm as any).records[0])
    expect((wrapper.vm as any).questionKind).toBe('candy')
    wrapper.unmount()
  })
  it('previews scheduled candy results without marking text as invalid HTML', async () => {
    const wrapper = mountModal()
    ;(wrapper.vm as any).previewScheduled({ id: 1, status: 'success', response_text: '29', error_message: '', latency_ms: 100, started_at: new Date().toISOString(), pelican_config: { question_kind: 'candy', prompt: 'question', reasoning_effort: 'medium', parallel_count: 1 } })
    await flushPromises()
    expect((wrapper.vm as any).questionKind).toBe('candy')
    expect((wrapper.vm as any).runs[0].status).toBe('success')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('29')
    wrapper.unmount()
  })
})

function probeResult(overrides: Record<string, unknown> = {}) {
  return {
    account_id: 42,
    model: 'gpt-6-astra',
    verdict: 'healthy',
    reason: '门票延续正常',
    mint_status: 200,
    continue_status: 200,
    minted: true,
    new_ticket: false,
    ticket_length: 780,
    continue_ticket_length: 0,
    reported_model: 'gpt-6-astra',
    latency_ms: 2300,
    started_at: '2026-09-27T10:00:00Z',
    finished_at: '2026-09-27T10:00:02Z',
    ...overrides
  }
}

describe('IQTestModal state probe mode', () => {
  beforeEach(() => {
    probeOpenAICodexState.mockReset()
    global.fetch = vi.fn() as any
  })

  it('defaults to the question test and hides the probe panel', () => {
    const wrapper = mountModal()
    expect((wrapper.vm as any).testMode).toBe('question')
    expect(wrapper.find('[data-testid="probe-panel"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="probe-start"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('switches to the probe and calls the state-probe API with the chosen model', async () => {
    probeOpenAICodexState.mockResolvedValueOnce(probeResult())
    const wrapper = mountModal()
    await wrapper.get('[data-testid="mode-probe"]').trigger('click')
    expect(wrapper.find('[data-testid="probe-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="question-select"]').exists()).toBe(false)
    ;(wrapper.vm as any).modelId = 'gpt-6-astra-probe'
    await wrapper.get('[data-testid="probe-start"]').trigger('click')
    await flushPromises()

    expect(probeOpenAICodexState).toHaveBeenCalledTimes(1)
    expect(probeOpenAICodexState.mock.calls[0][0]).toBe(42)
    expect(probeOpenAICodexState.mock.calls[0][1]).toBe('gpt-6-astra-probe')
    expect(global.fetch).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="probe-verdict"]').text()).toBe('admin.accounts.pelicanTest.probe.verdictHealthy')
    expect(wrapper.get('[data-testid="probe-new-ticket"]').text()).toBe('admin.accounts.pelicanTest.probe.no')
    expect(wrapper.get('[data-testid="probe-result"]').text()).toContain('HTTP 200')
    expect(wrapper.get('[data-testid="probe-result"]').text()).toContain('780')
    expect((wrapper.vm as any).running).toBe(false)
    wrapper.unmount()
  })

  it('renders a degraded verdict and keeps newest results first', async () => {
    probeOpenAICodexState
      .mockResolvedValueOnce(probeResult())
      .mockResolvedValueOnce(probeResult({ verdict: 'degraded', new_ticket: true, continue_ticket_length: 780, reason: '回了新门票', started_at: '2026-09-27T10:01:00Z' }))
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectMode('probe')
    await (wrapper.vm as any).startProbe()
    await (wrapper.vm as any).startProbe()
    await flushPromises()

    const verdicts = wrapper.findAll('[data-testid="probe-verdict"]')
    expect(verdicts).toHaveLength(2)
    expect(verdicts[0].text()).toBe('admin.accounts.pelicanTest.probe.verdictDegraded')
    expect(verdicts[1].text()).toBe('admin.accounts.pelicanTest.probe.verdictHealthy')
    expect(wrapper.findAll('[data-testid="probe-new-ticket"]')[0].text()).toBe('admin.accounts.pelicanTest.probe.yes')
    expect(wrapper.findAll('[data-testid="probe-result"]')[0].text()).toContain('780 → 780')
    wrapper.unmount()
  })

  it('shows inconclusive results with the failure kind and upstream detail', async () => {
    probeOpenAICodexState.mockResolvedValueOnce(probeResult({
      verdict: 'inconclusive',
      failure: 'model_unsupported',
      reason: '该账号的套餐不支持这个模型',
      detail: "The 'gpt-6-astra' model is not supported",
      mint_status: 400,
      continue_status: 0,
      minted: false,
      ticket_length: 0,
      reported_model: undefined
    }))
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectMode('probe')
    await (wrapper.vm as any).startProbe()
    await flushPromises()

    expect(wrapper.get('[data-testid="probe-verdict"]').text()).toBe('admin.accounts.pelicanTest.probe.verdictInconclusive')
    expect(wrapper.get('[data-testid="probe-failure"]').text()).toBe('admin.accounts.pelicanTest.probe.failures.model_unsupported')
    expect(wrapper.get('[data-testid="probe-detail"]').text()).toContain('not supported')
    expect(wrapper.get('[data-testid="probe-new-ticket"]').text()).toBe('—')
    expect(wrapper.get('[data-testid="probe-result"]').text()).toContain('HTTP 400')
    wrapper.unmount()
  })

  it('shows the API error, such as a busy account, without a result card', async () => {
    probeOpenAICodexState.mockRejectedValueOnce({ status: 409, code: 'STATE_PROBE_BUSY', message: '该账号已有一次探针在进行中，请稍后再试' })
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectMode('probe')
    await (wrapper.vm as any).startProbe()
    await flushPromises()

    expect(wrapper.get('[data-testid="probe-error"]').text()).toContain('该账号已有一次探针在进行中')
    expect(wrapper.find('[data-testid="probe-result"]').exists()).toBe(false)
    expect((wrapper.vm as any).running).toBe(false)
    wrapper.unmount()
  })

  it('does not probe accounts that are not OpenAI OAuth', async () => {
    const wrapper = mountModal({ type: 'apikey' })
    ;(wrapper.vm as any).selectMode('probe')
    await flushPromises()
    expect(wrapper.find('[data-testid="probe-unsupported"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="probe-start"]').attributes('disabled')).toBeDefined()
    await (wrapper.vm as any).startProbe()
    expect(probeOpenAICodexState).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('locks the mode switch while probing and resets to the question test on reopen', async () => {
    let resolveProbe: (value: unknown) => void = () => {}
    probeOpenAICodexState.mockImplementationOnce(() => new Promise((resolve) => { resolveProbe = resolve }))
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectMode('probe')
    const pending = (wrapper.vm as any).startProbe()
    await flushPromises()
    expect((wrapper.vm as any).running).toBe(true)
    expect(wrapper.get('[data-testid="mode-question"]').attributes('disabled')).toBeDefined()
    ;(wrapper.vm as any).selectMode('question')
    expect((wrapper.vm as any).testMode).toBe('probe')

    resolveProbe(probeResult())
    await pending
    await flushPromises()
    expect((wrapper.vm as any).probeResults).toHaveLength(1)

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect((wrapper.vm as any).testMode).toBe('question')
    expect((wrapper.vm as any).probeResults).toHaveLength(0)
    wrapper.unmount()
  })

  it('drops a probe result that arrives after the dialog was closed', async () => {
    probeOpenAICodexState.mockImplementationOnce((_id: number, _model: string, options: { signal: AbortSignal }) => new Promise((resolve) => {
      options.signal.addEventListener('abort', () => resolve(probeResult()))
    }))
    const wrapper = mountModal()
    ;(wrapper.vm as any).selectMode('probe')
    const pending = (wrapper.vm as any).startProbe()
    await flushPromises()
    await wrapper.setProps({ show: false })
    await pending
    await flushPromises()
    expect(probeOpenAICodexState.mock.calls[0][2].signal.aborted).toBe(true)
    expect((wrapper.vm as any).probeResults).toHaveLength(0)
    expect((wrapper.vm as any).running).toBe(false)
    wrapper.unmount()
  })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise })
  return { promise, resolve, reject }
}

describe('IQTestModal account model capabilities', () => {
  beforeEach(() => {
    localStorage.clear()
    global.fetch = vi.fn().mockResolvedValue(streamResponse([{ type: 'content', text: '21' }, { type: 'test_complete', success: true }]))
  })

  it('uses account-specific model efforts, rejects an unsupported none default and leaves empty capabilities unset', async () => {
    getAvailableModels.mockImplementation(async (id: number) => id === 42
      ? [{ id: 'gpt-6-astra', display_name: 'Astra' }, { id: 'gpt-6.1-sol', display_name: 'Sol' }, { id: 'unknown-model', display_name: 'Unknown' }]
      : [{ id: 'claude-opus-5-5', display_name: 'Claude' }])
    getModelReasoning.mockImplementation(async (_id: number, model: string) => model === 'gpt-6.1-sol'
      ? { supported_reasoning_levels: ['none', 'low', 'xhigh'], default_reasoning_level: 'xhigh' }
      : model === 'unknown-model'
        ? { supported_reasoning_levels: [], default_reasoning_level: '' }
        : model === 'claude-opus-5-5'
          ? { supported_reasoning_levels: ['low', 'max'], default_reasoning_level: 'max' }
          : { supported_reasoning_levels: ['low', 'medium', 'high'], default_reasoning_level: 'none' })
    const wrapper = mountModal()
    try {
      await flushPromises()
      const vm = wrapper.vm as any
      expect(getAvailableModels).toHaveBeenCalledWith(42)
      expect(vm.reasoningOptions.map((option: { value: string }) => option.value)).toEqual(['low', 'medium', 'high'])
      expect(vm.reasoningEffort).toBe('low')
      vm.modelId = 'gpt-6.1-sol'; await flushPromises()
      expect(getModelReasoning).toHaveBeenLastCalledWith(42, 'gpt-6.1-sol')
      expect(vm.reasoningOptions.map((option: { value: string }) => option.value)).toEqual(['none', 'low', 'xhigh'])
      expect(vm.reasoningEffort).toBe('xhigh')
      vm.modelId = 'unknown-model'; await flushPromises()
      expect(vm.reasoningOptions).toEqual([])
      expect(vm.reasoningEffort).toBe('')
      await vm.startTest(); await flushPromises()
      expect(JSON.parse(vi.mocked(global.fetch).mock.calls[0][1]!.body as string)).not.toHaveProperty('reasoning_effort')
      await wrapper.setProps({ account: { id: 43, name: 'Claude', platform: 'anthropic', type: 'oauth', status: 'active' } as any })
      await flushPromises()
      expect(getAvailableModels).toHaveBeenLastCalledWith(43)
      expect(vm.modelId).toBe('claude-opus-5-5')
      expect(vm.reasoningOptions.map((option: { value: string }) => option.value)).toEqual(['low', 'max'])
      expect(vm.reasoningEffort).toBe('max')
    } finally { wrapper.unmount() }
  })

  it('ignores older account capability successes and failures after switching accounts', async () => {
    for (const outcome of ['resolve', 'reject'] as const) {
      const oldModels = deferred<Array<{ id: string; display_name: string }>>()
      const oldReasoning = deferred<{ supported_reasoning_levels: string[]; default_reasoning_level: string }>()
      getAvailableModels.mockImplementation((id: number) => id === 42 ? oldModels.promise : Promise.resolve([{ id: 'gpt-6.1-sol', display_name: 'Sol' }]))
      getModelReasoning.mockImplementation((id: number) => id === 42 ? oldReasoning.promise : Promise.resolve({ supported_reasoning_levels: ['low', 'xhigh'], default_reasoning_level: 'xhigh' }))
      const wrapper = mountModal()
      try {
        await wrapper.setProps({ account: { id: 43, name: 'Sol account', platform: 'openai', type: 'oauth', status: 'active' } as any })
        await flushPromises()
        if (outcome === 'resolve') {
          oldModels.resolve([{ id: 'stale-model', display_name: 'Stale' }])
          oldReasoning.resolve({ supported_reasoning_levels: ['none'], default_reasoning_level: 'none' })
        } else {
          oldModels.reject(new Error('older account lookup failed'))
          oldReasoning.reject(new Error('older account lookup failed'))
        }
        await flushPromises()
        const vm = wrapper.vm as any
        expect(vm.modelOptions.map((option: { value: string }) => option.value)).toEqual(['gpt-6.1-sol'])
        expect(vm.modelId).toBe('gpt-6.1-sol')
        expect(vm.reasoningOptions.map((option: { value: string }) => option.value)).toEqual(['low', 'xhigh'])
        expect(vm.reasoningEffort).toBe('xhigh')
      } finally { wrapper.unmount() }
    }
  })

  it('does not update model or effort choices when lookups finish after closing the dialog', async () => {
    for (const outcome of ['resolve', 'reject'] as const) {
      const pendingModels = deferred<Array<{ id: string; display_name: string }>>()
      const pendingReasoning = deferred<{ supported_reasoning_levels: string[]; default_reasoning_level: string }>()
      getAvailableModels.mockReturnValue(pendingModels.promise)
      getModelReasoning.mockReturnValue(pendingReasoning.promise)
      const wrapper = mountModal()
      try {
        await wrapper.setProps({ show: false })
        const vm = wrapper.vm as any
        const before = { model: vm.modelId, models: [...vm.modelOptions], efforts: [...vm.reasoningOptions], effort: vm.reasoningEffort }
        if (outcome === 'resolve') {
          pendingModels.resolve([{ id: 'closed-model', display_name: 'Closed' }])
          pendingReasoning.resolve({ supported_reasoning_levels: ['none'], default_reasoning_level: 'none' })
        } else {
          pendingModels.reject(new Error('closed dialog lookup failed'))
          pendingReasoning.reject(new Error('closed dialog lookup failed'))
        }
        await flushPromises()
        expect({ model: vm.modelId, models: [...vm.modelOptions], efforts: [...vm.reasoningOptions], effort: vm.reasoningEffort }).toEqual(before)
      } finally { wrapper.unmount() }
    }
  })
})
