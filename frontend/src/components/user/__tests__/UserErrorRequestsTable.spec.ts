import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import UserErrorRequestsTable from '../UserErrorRequestsTable.vue'
import DataTable from '@/components/common/DataTable.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

function mountTable() {
  return mount(UserErrorRequestsTable, {
    props: { rows: [], total: 0, loading: false, page: 1, pageSize: 20 },
    global: { stubs: { Pagination: true, IpGeoBatchToolbar: true, UserErrorRequestDetailModal: true } },
  })
}

describe('UserErrorRequestsTable column preferences', () => {
  it('starts fully visible and follows explicit visible key order', async () => {
    const wrapper = mountTable()
    const table = wrapper.findComponent(DataTable)
    expect(table.props('columns').map((column: { key: string }) => column.key)).toEqual([
      'created_at', 'key_name', 'model', 'status', 'message', 'category', 'group',
      'type', 'platform', 'endpoint', 'client_ip', 'user_agent',
    ])
    await wrapper.setProps({ visibleColumnKeys: ['message', 'created_at', 'key_name'] })
    expect(table.props('columns').map((column: { key: string }) => column.key)).toEqual([
      'message', 'created_at', 'key_name',
    ])
    wrapper.unmount()
  })

  it('forwards header reordering without triggering a data sort', () => {
    const wrapper = mountTable()
    wrapper.findComponent(DataTable).vm.$emit('columnReorder', ['message', 'created_at'])
    expect(wrapper.emitted('column-reorder')).toEqual([[['message', 'created_at']]])
    expect(wrapper.emitted('sort')).toBeUndefined()
    wrapper.unmount()
  })
})
