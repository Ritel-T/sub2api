import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Input from '../Input.vue'
import TextArea from '../TextArea.vue'

describe.each([[Input, 'input'], [TextArea, 'textarea']] as const)('%s field contract', (component, tag) => {
  it('links distinct labels and descriptions while forwarding native constraints', async () => {
    const wrapper = mount({ components: { Field: component }, template: '<div><Field model-value="" label="First" hint="First hint" min="1" max="8" maxlength="12" class="layout-width" aria-describedby="external-hint" /><Field model-value="" label="Second" /></div>' })
    const fields = wrapper.findAll(tag)
    const id = fields[0].attributes('id')
    expect(id).toBeTruthy()
    expect(fields[1].attributes('id')).not.toBe(id)
    expect(wrapper.findAll('label')[0].attributes('for')).toBe(id)
    expect(fields[0].attributes('aria-describedby')).toBe(`external-hint ${id}-description`)
    expect(wrapper.get(`[id="${id}-description"]`).text()).toBe('First hint')
    expect(fields[0].attributes('min')).toBe('1')
    expect(fields[0].attributes('maxlength')).toBe('12')
    expect(wrapper.get('.layout-width').element.tagName).toBe('DIV')
    expect(wrapper.get('.layout-width').attributes('min')).toBeUndefined()
    wrapper.unmount()
  })

  it('announces errors and preserves explicit id, readonly, disabled, and changes', async () => {
    const wrapper = mount(component, { props: { modelValue: '', id: 'explicit-field', label: 'Field', error: 'Required', readonly: true, disabled: true } })
    const field = wrapper.get(tag)
    expect(field.attributes('id')).toBe('explicit-field')
    expect(field.attributes('aria-invalid')).toBe('true')
    expect(field.attributes('aria-describedby')).toBe('explicit-field-description')
    expect(field.attributes('readonly')).toBeDefined()
    expect(field.attributes('disabled')).toBeDefined()
    await wrapper.setProps({ error: '', disabled: false, readonly: false })
    expect(field.attributes('aria-invalid')).toBeUndefined()
    expect(field.attributes('aria-describedby')).toBeUndefined()
    await field.setValue('Changed')
    expect(wrapper.emitted('update:modelValue')).toEqual([['Changed']])
    wrapper.unmount()
  })
})
