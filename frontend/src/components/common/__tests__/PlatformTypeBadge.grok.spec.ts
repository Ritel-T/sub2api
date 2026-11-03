import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})
describe('PlatformTypeBadge OpenAI authentication modes', () => {
  it('distinguishes Agent Identity, PAT, and OAuth accounts', async () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'openai',
        type: 'oauth',
        authMode: 'agentIdentity',
      },
    })

    expect(wrapper.text()).toContain('Agent Identity')

    await wrapper.setProps({ authMode: 'personalAccessToken' })
    expect(wrapper.text()).toContain('PAT')
    expect(wrapper.text()).not.toContain('Agent Identity')

    await wrapper.setProps({ authMode: undefined })
    expect(wrapper.text()).toContain('OAuth')
  })
})

describe('PlatformTypeBadge MiniMax', () => {
  it('labels MiniMax API keys as MiniMax, not Gemini', () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'minimax',
        type: 'apikey',
      },
    })

    expect(wrapper.text()).toContain('MiniMax')
    expect(wrapper.text()).toContain('Key')
    expect(wrapper.text()).not.toContain('Gemini')
  })
})
