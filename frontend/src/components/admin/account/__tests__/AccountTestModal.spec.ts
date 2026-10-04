import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        if (key === 'admin.accounts.imagePreviewAlt' && params?.index) {
          return `test-image-${params.index}`
        }
        return messages[key] || key
      }
    })
  }
})

function createStreamResponse(lines: string[]) {
  const encoder = new TextEncoder()
  const chunks = lines.map((line) => encoder.encode(line))
  let index = 0

  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

function mountModal(account: Record<string, unknown> = {
  id: 42,
  name: 'Gemini Image Test',
  platform: 'gemini',
  type: 'apikey',
  status: 'active'
}) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: { template: '<div class="select-stub"></div>' },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' },
      { id: 'gemini-2.5-flash-image', display_name: 'Gemini 2.5 Flash Image' },
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini 3.1 Flash Image' }
    ])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gemini-2.5-flash-image"}\n',
        'data: {"type":"image","image_url":"data:image/png;base64,QUJD","mime_type":"image/png"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('gemini 图片模型测试会携带提示词并渲染图片预览', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const promptInput = wrapper.find('textarea.textarea-stub')
    expect(promptInput.exists()).toBe(true)
    await promptInput.setValue('draw a tiny orange cat astronaut')

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gemini-3.1-flash-image',
      prompt: 'draw a tiny orange cat astronaut'
    })

    const preview = wrapper.find('img[alt="test-image-1"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('src')).toBe('data:image/png;base64,QUJD')
  })

  it('grok 账号测试默认选择 Grok 模型', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'grok-4.3', display_name: 'Grok 4.3' },
      { id: 'grok-build-0.1', display_name: 'Grok Build 0.1' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"grok-4.3"}\n',
        'data: {"type":"content","text":"ok"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 13,
      name: 'Grok Account',
      platform: 'grok',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'grok-4.3',
      prompt: '',
      mode: 'text'
    })
  })

  it('OpenAI Compact 探测会携带 compact 测试模式', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'compact'
    })
  })
})

describe('Borrowing account test results', () => {
  async function run(lines: string[]) {
    getAvailableModels.mockResolvedValue([{ id: 'gpt-6-astra', display_name: 'Astra' }])
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse(lines))
    const w = mountModal({ id: 34, name: 'Borrow target', platform: 'openai', type: 'oauth', status: 'active' })
    await w.setProps({ show: true }); await flushPromises()
    await w.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!.trigger('click')
    await flushPromises(); await flushPromises()
    return w
  }
  afterEach(() => vi.unstubAllGlobals())
  it('does not claim connection success when preparing borrowing, and fails an unterminated test', async () => {
    const w = await run(['data: {"type":"test_start","model":"gpt-6-astra","channel":"gateway_borrow"}\n'])
    expect(w.text()).toContain('admin.astraGateway.testPreparing')
    expect(w.text()).not.toContain('admin.accounts.connectedToApi')
    expect(w.text()).toContain('admin.astraGateway.testIncomplete')
    expect(w.text()).not.toContain('admin.accounts.testCompleted')
    w.unmount()
  })
  it.each([
    ['target_probe_degraded', 'ticketChanged'], ['target_quality_failed', 'answerFailed'],
    ['target_probe_rate_limited', 'rateLimited'], ['target_probe_auth_failed', 'authFailed'],
    ['target_validation_in_progress', 'busy'], ['borrow_route_expired', 'expired']
  ])('shows a readable %s result without claiming success', async (code, reason) => {
    const w = await run([
      'data: {"type":"test_start","model":"gpt-6-astra"}\n',
      `data: ${JSON.stringify({ type: 'error', code, channel: 'gateway_borrow', error: 'internal detail' })}\n`
    ])
    expect(w.text()).toContain(`admin.astraGateway.testReasons.${reason}`)
    expect(w.text()).not.toContain('admin.accounts.testCompleted')
    expect(w.text()).not.toContain('internal detail')
    w.unmount()
  })
  it('translates the verified-route status while waiting for the full model test', async () => {
    const w = await run(['data: {"type":"status","code":"gateway_borrow_testing","channel":"gateway_borrow","text":"Testing the verified gateway borrow route"}\n'])
    expect(w.text()).toContain('admin.astraGateway.testReasons.testing')
    expect(w.text()).toContain('admin.astraGateway.testIncomplete')
    expect(w.text()).not.toContain('admin.accounts.testCompleted')
    w.unmount()
  })
  it('keeps a reported verification error even if a later terminal event incorrectly claims success', async () => {
    const w = await run([
      'data: {"type":"error","code":"target_quality_failed","error":"internal detail"}\n',
      'data: {"type":"test_complete","success":true}\n'
    ])
    expect(w.text()).toContain('admin.astraGateway.testReasons.answerFailed')
    expect(w.text()).not.toContain('admin.accounts.testCompleted')
    w.unmount()
  })
  it('accepts only the explicit successful terminal result, including final data without a newline', async () => {
    const w = await run(['data: {"type":"test_start","model":"gpt-6-astra"}\n', 'data: {"type":"test_complete","success":true}'])
    expect(w.text()).toContain('admin.accounts.testCompleted')
    expect(w.text()).not.toContain('admin.accounts.connectedToApi')
    w.unmount()
  })
})
