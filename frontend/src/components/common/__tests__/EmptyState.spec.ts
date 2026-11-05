import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import EmptyState from '../EmptyState.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('EmptyState content contract', () => {

  it('keeps the localized default and action event when content is omitted', async () => {
    const wrapper = mount(EmptyState, { props: { actionText: 'Create', actionIcon: false } })
    expect(wrapper.get('h3').text()).toBe('common.noData')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('action')).toHaveLength(1)
    wrapper.unmount()
  })
})
