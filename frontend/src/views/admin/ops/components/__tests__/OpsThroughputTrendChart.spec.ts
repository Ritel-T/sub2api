import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import OpsThroughputTrendChart from '../OpsThroughputTrendChart.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    name: 'Line',
    props: ['data', 'options'],
    template: '<div class="line-chart" />',
  },
}))

describe('OpsThroughputTrendChart', () => {
  it('updates resolved canvas colors when the application theme changes without remounting', async () => {
    const previousClass = document.documentElement.className
    const themeStyle = document.createElement('style')
    themeStyle.textContent = `
      :root {
        --theme-surface-raised: #faf8f5;
        --theme-foreground: #1e1e1c;
        --theme-muted: #6f6254;
        --theme-border: #ddd8d0;
      }
      :root.dark {
        --theme-surface-raised: #2e2d29;
        --theme-foreground: #f0ede7;
        --theme-muted: #aba59b;
        --theme-border: #393833;
      }
    `
    document.head.appendChild(themeStyle)
    document.documentElement.classList.remove('dark')
    const wrapper = mount(OpsThroughputTrendChart, {
      props: {
        points: [{ bucket_start: '2026-09-29T00:00:00Z', request_count: 10, qps: 1, tps: 500 }],
        loading: false,
        timeRange: '1h'
      },
      global: { stubs: { EmptyState: true, HelpTooltip: true } }
    })

    try {
      const chart = wrapper.findComponent({ name: 'Line' })
      // The production component passes actual colors to Chart.js, never unresolved var().
      const lightOptions = chart.props('options')
      expect(lightOptions.plugins.tooltip.backgroundColor).toBe('#faf8f5')
      expect(lightOptions.scales.x.ticks.color).toBe('#6f6254')
      expect(lightOptions.scales.y.grid.color).toBe('#ddd8d0')
      const originalData = chart.props('data')

      document.documentElement.classList.add('dark')
      await flushPromises()

      const darkOptions = chart.props('options')
      expect(darkOptions).not.toBe(lightOptions)
      expect(darkOptions.plugins.tooltip).toMatchObject({
        backgroundColor: '#2e2d29',
        titleColor: '#f0ede7',
        bodyColor: '#aba59b',
        borderColor: '#393833'
      })
      expect(darkOptions.scales.x.ticks.color).toBe('#aba59b')
      expect(darkOptions.scales.y.grid.color).toBe('#393833')
      expect(darkOptions.scales.x.border.color).toBe('#393833')
      expect(chart.props('data')).toEqual(originalData)
      expect(chart.props('data').datasets[0].borderColor).toBe('#3b82f6')

      document.documentElement.classList.remove('dark')
      await flushPromises()
      expect(chart.props('options').plugins.tooltip.backgroundColor).toBe('#faf8f5')
    } finally {
      wrapper.unmount()
      themeStyle.remove()
      document.documentElement.className = previousClass
    }
  })

  it('allows the header controls to wrap on narrow screens', () => {
    const wrapper = mount(OpsThroughputTrendChart, {
      props: {
        points: [],
        loading: false,
        timeRange: '1h',
      },
      global: {
        stubs: {
          EmptyState: true,
          HelpTooltip: true,
        },
      },
    })

    const header = wrapper.get('[data-testid="throughput-chart-header"]')
    expect(header.classes()).toEqual(expect.arrayContaining(['flex-col', 'sm:flex-row']))

    const toolbar = wrapper.get('[data-testid="throughput-chart-toolbar"]')
    expect(toolbar.classes()).toEqual(expect.arrayContaining(['w-full', 'flex-wrap', 'sm:w-auto']))
    expect(toolbar.findAll('button')).toHaveLength(3)
    toolbar.findAll('button').forEach((button) => {
      expect(button.classes()).toContain('shrink-0')
      expect(button.classes()).not.toContain('ml-2')
    })
  })
})
