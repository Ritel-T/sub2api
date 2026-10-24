import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppHeading from '../AppHeading.vue'

describe('AppHeading semantic contract', () => {
  it('changes document level independently from visual role', async () => {
    const wrapper = mount(AppHeading, { props: { variant: 'page', as: 'h2' }, slots: { default: 'Account details' } })
    expect(wrapper.element.tagName).toBe('H2')
    expect(wrapper.attributes('data-type-role')).toBe('page')
    await wrapper.setProps({ as: 'h1' })
    expect(wrapper.element.tagName).toBe('H1')
    expect(wrapper.attributes('data-type-role')).toBe('page')
    await wrapper.setProps({ variant: 'section' })
    expect(wrapper.element.tagName).toBe('H1')
    expect(wrapper.attributes('data-type-role')).toBe('section')
    wrapper.unmount()
  })

  it.each([['page', 'H1'], ['auth', 'H1'], ['hero', 'H1'], ['section', 'H2'], ['subsection', 'H3'], ['minor', 'H4'], ['dialog', 'H2']] as const)('defaults %s to %s', (variant, tag) => {
    const wrapper = mount(AppHeading, { props: { variant } })
    expect(wrapper.element.tagName).toBe(tag)
    wrapper.unmount()
  })

  it('preserves ids, labels, layout classes, rich slots and native events', async () => {
    const wrapper = mount(AppHeading, { attrs: { id: 'dialog-label', class: 'mb-4', 'aria-label': 'Details' }, slots: { default: '<span>Details <em>updated</em></span>' } })
    expect(wrapper.attributes('id')).toBe('dialog-label')
    expect(wrapper.attributes('aria-label')).toBe('Details')
    expect(wrapper.classes()).toContain('ritel-type')
    expect(wrapper.classes()).toContain('mb-4')
    expect(wrapper.get('em').text()).toBe('updated')
    await wrapper.trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
    wrapper.unmount()
  })
})
