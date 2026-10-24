import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ConfirmationPrompt from '@/components/common/ConfirmationPrompt.vue'
import BulkEditUserModal from '../BulkEditUserModal.vue'

const { batchUpdateLimits, showSuccess, showError } = vi.hoisted(() => ({
  batchUpdateLimits: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      batchUpdateLimits
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const mountModal = () => mount(BulkEditUserModal, {
  props: {
    show: true,
    selectedIds: [4, 7]
  },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show', 'title'],
        emits: ['close'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      }
    }
  }
})

describe('BulkEditUserModal', () => {
  beforeEach(() => {
    batchUpdateLimits.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    batchUpdateLimits.mockResolvedValue({ affected: 2 })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('marks bulk changes dangerous and cancels when the selected users change', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    await wrapper.get('[data-test="concurrency-input"]').setValue('5')
    await wrapper.get('form').trigger('submit')
    const confirmation = (wrapper.getComponent(ConfirmationPrompt).props('confirmation'))
    expect(confirmation.danger.value).toBe(true)
    await wrapper.setProps({ selectedIds: [99] })
    confirmation.settle(true)
    await flushPromises()
    expect(batchUpdateLimits).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('locks repeat confirmation and ignores a result after its parent closes', async () => {
    let finish!: (value: { affected: number }) => void
    batchUpdateLimits.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountModal()
    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    await wrapper.get('[data-test="concurrency-input"]').setValue('5')
    await wrapper.get('form').trigger('submit')
    const confirmation = wrapper.getComponent(ConfirmationPrompt).props('confirmation')
    confirmation.settle(true)
    confirmation.settle(true)
    await flushPromises()
    expect(confirmation.busy.value).toBe(true)
    expect(batchUpdateLimits).toHaveBeenCalledTimes(1)
    expect(batchUpdateLimits).toHaveBeenCalledWith({ user_ids: [4, 7], all: false, concurrency: 5 })
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, selectedIds: [99] })
    finish({ affected: 2 })
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([
    [{ message: 'Selected users are no longer available' }, 'Selected users are no longer available'],
    [{ response: { data: { message: 'Legacy message' } } }, 'Legacy message'],
    [{ response: { data: { detail: 'Legacy detail' } } }, 'Legacy detail'],
    [{}, 'admin.users.bulkLimits.failed'],
  ])('shows API error details and allows retry after failure', async (error, expected) => {
    batchUpdateLimits.mockRejectedValueOnce(error)
    const wrapper = mountModal()
    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    await wrapper.get('[data-test="concurrency-input"]').setValue('5')
    await wrapper.get('form').trigger('submit')
    expect(batchUpdateLimits).not.toHaveBeenCalled()
    await wrapper.getComponent(ConfirmationPrompt).get('.btn-danger').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(expected)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('disables submission until at least one enabled field has a value', async () => {
    const wrapper = mountModal()

    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="concurrency-input"]').setValue('5')
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeUndefined()
  })

  it('disables submission when more than 500 users are selected', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ selectedIds: Array.from({ length: 501 }, (_, index) => index + 1) })
    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    await wrapper.get('[data-test="concurrency-input"]').setValue('5')

    expect(wrapper.text()).toContain('admin.users.bulkLimits.selectionLimit')
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeDefined()
  })

  it('submits only the enabled RPM field and preserves zero as unlimited', async () => {
    const wrapper = mountModal()

    await wrapper.get('[data-test="enable-rpm-limit"]').trigger('click')
    await wrapper.get('[data-test="rpm-limit-input"]').setValue('0')
    expect(wrapper.text()).toContain('admin.users.bulkLimits.unlimited')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.getComponent(ConfirmationPrompt).text()).toContain('admin.users.bulkLimits.rpmUnlimitedValue')
    expect(batchUpdateLimits).not.toHaveBeenCalled()
    await wrapper.getComponent(ConfirmationPrompt).get('.btn-danger').trigger('click')
    await flushPromises()

    expect(batchUpdateLimits).toHaveBeenCalledWith({
      user_ids: [4, 7],
      all: false,
      rpm_limit: 0
    })
    expect(wrapper.emitted('success')).toEqual([[2]])
  })

  it('omits disabled fields from the request', async () => {
    const wrapper = mountModal()

    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    await wrapper.get('[data-test="concurrency-input"]').setValue('9')
    await wrapper.get('form').trigger('submit')
    expect(batchUpdateLimits).not.toHaveBeenCalled()
    await wrapper.getComponent(ConfirmationPrompt).get('.btn-danger').trigger('click')
    await flushPromises()

    expect(batchUpdateLimits).toHaveBeenCalledWith({
      user_ids: [4, 7],
      all: false,
      concurrency: 9
    })
  })

  it('does not call the API when overwrite confirmation is cancelled', async () => {
    const wrapper = mountModal()

    await wrapper.get('[data-test="enable-concurrency"]').trigger('click')
    await wrapper.get('[data-test="concurrency-input"]').setValue('9')
    await wrapper.get('form').trigger('submit')
    expect(batchUpdateLimits).not.toHaveBeenCalled()
    await wrapper.getComponent(ConfirmationPrompt).get('.btn-secondary').trigger('click')
    await flushPromises()

    expect(batchUpdateLimits).not.toHaveBeenCalled()
  })
})
