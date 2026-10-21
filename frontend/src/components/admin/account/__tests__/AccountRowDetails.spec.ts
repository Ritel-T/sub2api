import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, ref } from 'vue'
import AccountRowDetails from '../AccountRowDetails.vue'
import type { Column } from '@/components/common/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('AccountRowDetails', () => {
  it('groups every supplied field once and keeps cell renderers reactive', async () => {
    const value = ref('first value')
    const columns: Column[] = ['notes', 'today_stats', 'priority', 'usage', 'links', 'created_at'].map(key => ({ key, label: key }))
    const wrapper = mount(AccountRowDetails, {
      props: { columns, renderCell: (column: Column) => h('span', { 'data-rendered': column.key }, value.value) },
      slots: { actions: '<button>test operation</button>', supplementary: '<button>credits</button>' }
    })
    expect(wrapper.findAll('[data-rendered]').map(node => node.attributes('data-rendered')).sort()).toEqual(columns.map(column => column.key).sort())
    expect(wrapper.findAll('.account-detail-properties [data-field]').map(node => node.attributes('data-field'))).toEqual(['priority', 'created_at', 'notes'])
    expect(wrapper.find('.account-detail-stats [data-field="today_stats"]').exists()).toBe(true)
    expect(wrapper.find('.account-detail-links [data-rendered="links"]').exists()).toBe(true)
    value.value = 'updated value'
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-rendered="priority"]').text()).toBe('updated value')
    expect(wrapper.findAll('button').map(button => button.text())).toEqual(['test operation', 'credits'])
    wrapper.unmount()
  })

  it('keeps actions available when all configurable fields are on the main row', () => {
    const wrapper = mount(AccountRowDetails, { props: { columns: [], renderCell: () => null }, slots: { actions: '<button>delete after confirmation</button>' } })
    expect(wrapper.find('button').text()).toBe('delete after confirmation')
    expect(wrapper.find('.account-detail-stats').exists()).toBe(false)
    expect(wrapper.find('.account-detail-properties').exists()).toBe(false)
    expect(wrapper.find('.account-detail-extra').exists()).toBe(false)
    wrapper.unmount()
  })
})
