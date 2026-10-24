import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import EmptyState from '../EmptyState.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('EmptyState content contract', () => {
  it('shows the message supplied by usage and error tables', () => {
    const wrapper = mount(EmptyState, { props: { message: 'No failed requests' } })
    expect(wrapper.get('h3').text()).toBe('No failed requests')
    expect(wrapper.find('.empty-state-description').exists()).toBe(false)
    wrapper.unmount()
  })

  it('preserves explicit title and description', () => {
    const wrapper = mount(EmptyState, { props: { title: 'No keys', description: 'Create one to begin', message: 'Fallback' } })
    expect(wrapper.get('h3').text()).toBe('No keys')
    expect(wrapper.get('.empty-state-description').text()).toBe('Create one to begin')
    wrapper.unmount()
  })

  it('keeps the localized default and action event when content is omitted', async () => {
    const wrapper = mount(EmptyState, { props: { actionText: 'Create', actionIcon: false } })
    expect(wrapper.get('h3').text()).toBe('common.noData')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('action')).toHaveLength(1)
    wrapper.unmount()
  })
})
