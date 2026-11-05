import { afterEach, describe, expect, it } from 'vitest'
import { Chart } from 'chart.js'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { useChartTheme } from '../chartTheme'

afterEach(() => { document.documentElement.removeAttribute('style') })
describe('lazy chart defaults', () => {
  it('applies font and current dark theme on first chart, updates while mounted, disposes on unmount', async () => {
    const root = document.documentElement
    root.classList.add('dark')
    root.style.setProperty('--ritel-ui', 'Fixture Sans')
    const wrapper = mount(defineComponent({ setup() { const theme = useChartTheme(); return () => theme.value.text } }))
    expect(Chart.defaults.font.family).toBe('Fixture Sans')
    expect(Chart.defaults.font.size).toBe(12)
    expect(Chart.defaults.color).toBe('#b4b1a7')
    root.classList.remove('dark')
    await nextTick(); await nextTick()
    expect(Chart.defaults.color).toBe('#69665c')
    expect(wrapper.text()).toBe('#69665c')
    wrapper.unmount()
    root.classList.add('dark')
    await nextTick(); await nextTick()
    expect(Chart.defaults.color).toBe('#69665c')
    root.classList.remove('dark')
  })
})
