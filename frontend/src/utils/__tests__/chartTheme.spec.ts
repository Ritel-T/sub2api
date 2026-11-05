import { afterEach, describe, expect, it } from 'vitest'
import { Chart } from 'chart.js'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { getChartTheme, useChartTheme } from '../chartTheme'

afterEach(() => { document.documentElement.removeAttribute('style') })
describe('chart theme colors', () => {
  it('uses matching light and dark colors before styles are available', () => {
    expect(getChartTheme(false).tooltip.backgroundColor).toBe('#f0eee6')
    expect(getChartTheme(false).tooltip.titleColor).toBe('#27251f')
    expect(getChartTheme(true).tooltip.backgroundColor).toBe('#2b2b27')
    expect(getChartTheme(true).tooltip.titleColor).toBe('#eeece5')
  })
  it('uses the actual shared surface and text tokens for canvas consumers', () => {
    document.documentElement.style.setProperty('--theme-surface-raised', '#abcabc')
    document.documentElement.style.setProperty('--theme-muted', '#123123')
    expect(getChartTheme(false).tooltip.backgroundColor).toBe('#abcabc')
    expect(getChartTheme(false).text).toBe('#123123')
  })
})


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
