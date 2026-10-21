import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '@/types'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('AccountActionMenu inline disclosure', () => {
  it('renders all eligible actions in place and emits the existing operations', async () => {
    const account = { id: 42, name: 'parent', platform: 'openai', type: 'oauth', status: 'error', parent_account_id: null } as Account
    const wrapper = mount(AccountActionMenu, { props: { inline: true, account }, global: { stubs: { Icon: true } } })
    expect(wrapper.find('.action-menu-content').exists()).toBe(false)
    const actions = [
      ['admin.accounts.testConnection', 'test'], ['admin.accounts.viewStats', 'stats'],
      ['admin.scheduledTests.schedule', 'schedule'], ['admin.accounts.pelicanTest.menu', 'iq-test'],
      ['admin.accounts.reAuthorize', 'reauth'], ['admin.accounts.refreshToken', 'refresh-token'],
      ['admin.accounts.createSparkShadow', 'create-spark-shadow'], ['admin.accounts.setPrivacy', 'set-privacy'],
      ['admin.accounts.recoverState', 'recover-state'], ['common.delete', 'delete']
    ]
    for (const [label, event] of actions) {
      const button = wrapper.findAll('button').find(button => button.text() === label)
      expect(button, label).toBeTruthy()
      await button!.trigger('click')
      expect(wrapper.emitted(event)?.[0]).toEqual([account])
    }
    expect(wrapper.findAll('button')).toHaveLength(actions.length)
    wrapper.unmount()
  })

  it('keeps credential actions unavailable for shadow accounts in the inline layout', () => {
    const account = { id: 43, platform: 'openai', type: 'oauth', status: 'active', parent_account_id: 42 } as Account
    const wrapper = mount(AccountActionMenu, { props: { inline: true, account }, global: { stubs: { Icon: true } } })
    const labels = wrapper.findAll('button').map(button => button.text())
    expect(labels).toContain('common.delete')
    for (const label of ['reAuthorize', 'refreshToken', 'createSparkShadow', 'setPrivacy', 'duplicateAccount']) expect(labels).not.toContain(`admin.accounts.${label}`)
    wrapper.unmount()
  })
})
