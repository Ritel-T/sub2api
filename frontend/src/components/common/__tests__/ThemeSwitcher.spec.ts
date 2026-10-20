import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ThemeSwitcher from '../ThemeSwitcher.vue'

const preference = ref('system')
const setTheme = vi.fn(value => { preference.value = value })
vi.mock('@/composables/useTheme', () => ({ useTheme: () => ({ preference, setTheme }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => ({
  'common.theme.label': 'Appearance', 'common.theme.system': 'System',
  'common.theme.light': 'Light', 'common.theme.dark': 'Dark',
}[key] ?? key) }) }))

describe('theme switcher', () => {
  it('exposes all theme choices through a named keyboard-accessible select', async () => {
    const wrapper = mount(ThemeSwitcher)
    const select = wrapper.get('select')
    expect(select.attributes('aria-label')).toBe('Appearance')
    expect(wrapper.findAll('option').map(option => option.text())).toEqual(['System', 'Light', 'Dark'])
    await select.setValue('light')
    expect(setTheme).toHaveBeenCalledWith('light')
    expect(wrapper.get('label').attributes('title')).toBe('Appearance: Light')
    wrapper.unmount()
  })
})
