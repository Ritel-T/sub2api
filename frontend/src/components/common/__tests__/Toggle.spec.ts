import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import Toggle from '../Toggle.vue'
import PaymentToggle from '../../payment/ToggleSwitch.vue'

describe('Toggle behavior across form and payment controls', () => {
  it.each([false, true])('emits the inverse of %s without changing the controlled value', async (value) => {
    const wrapper = mount(Toggle, { props: { modelValue: value }, attrs: { 'aria-label': 'Enable' } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[!value]])
    expect(wrapper.get('button').attributes('aria-checked')).toBe(String(value))
    expect(wrapper.get('button').attributes('aria-label')).toBe('Enable')
    wrapper.unmount()
  })

  it('blocks changes while disabled and resumes when enabled', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false, disabled: true } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: false })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    wrapper.unmount()
  })

  it('runs a legacy click consumer once without a second model mutation', async () => {
    const update = vi.fn()
    const clicked = vi.fn()
    const wrapper = mount(Toggle, { props: { modelValue: false }, attrs: { 'aria-label': 'Scheduling', onClick: clicked, 'onUpdate:modelValue': update } })
    await wrapper.get('button').trigger('click')
    expect(clicked).toHaveBeenCalledTimes(1)
    expect(update).toHaveBeenCalledTimes(1)
    expect(update).toHaveBeenCalledWith(true)
    expect(wrapper.get('button').attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('updates its accessible name when the represented row changes', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false }, attrs: { 'aria-label': 'Scheduling: first account' } })
    await wrapper.setProps({ 'aria-label': 'Scheduling: second account' })
    expect(wrapper.get('button').attributes('aria-label')).toBe('Scheduling: second account')
    wrapper.unmount()
  })

  it('retains compact payment sizing while using the shared switch behavior', async () => {
    const wrapper = mount(PaymentToggle, { props: { checked: true, label: 'Refunds' } })
    expect(wrapper.get('button').attributes('role')).toBe('switch')
    expect(wrapper.get('button').attributes('aria-label')).toBe('Refunds')
    expect(wrapper.get('button').classes()).toContain('w-9')
    expect(wrapper.get('button').attributes('type')).toBe('button')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('toggle')).toEqual([[]])
    await wrapper.setProps({ disabled: true })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
    wrapper.unmount()
  })
})
