import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { AccountPlatform } from '@/types'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountPlan(platform: AccountPlatform, planType: string) {
  return mount(PlatformTypeBadge, {
    props: { platform, type: 'oauth', planType },
  })
}

describe('PlatformTypeBadge ChatGPT plan tiers', () => {
  it('hides legacy upstream plan and expiration metadata for OpenAI API keys', async () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'openai',
        type: 'apikey',
        planType: 'pro',
        subscriptionExpiresAt: '2027-01-01T00:00:00Z',
      },
    })

    for (const planType of ['pro', 'chatgptpro', 'plus', 'team', 'free', 'future_sku']) {
      await wrapper.setProps({ planType })
      expect(wrapper.text().replace(/\s+/g, '')).toBe('OpenAIKey')
      expect(wrapper.text()).not.toContain('admin.accounts.subscriptionExpires')
      expect(wrapper.text()).not.toContain('2027-01-01')
    }

    await wrapper.setProps({ type: 'oauth', planType: 'pro' })
    expect(wrapper.text()).toContain('Pro 200')
    expect(wrapper.text()).toContain('admin.accounts.subscriptionExpires')

    await wrapper.setProps({ type: 'apikey' })
    expect(wrapper.text().replace(/\s+/g, '')).toBe('OpenAIKey')
  })

  it('falls back to the raw value for unknown plans', () => {
    const wrapper = mountPlan('openai', 'future_sku')

    expect(wrapper.text()).toContain('future_sku')
    expect(wrapper.html()).not.toContain('bg-violet-100')
    expect(wrapper.html()).not.toContain('bg-indigo-100')
  })

  it('does not apply the ChatGPT tier naming to other platforms', () => {
    // Antigravity 的 Pro 与 Grok 的 pro 是各自产品线的档位，不能显示成 Pro 200。
    for (const platform of ['antigravity', 'grok'] as AccountPlatform[]) {
      const wrapper = mountPlan(platform, 'pro')

      expect(wrapper.text()).toContain('Pro')
      expect(wrapper.text()).not.toContain('Pro 200')
    }

    const antigravityTeam = mountPlan('antigravity', 'team')
    expect(antigravityTeam.text()).toContain('Team')
    expect(antigravityTeam.text()).not.toContain('Business')
  })
})
