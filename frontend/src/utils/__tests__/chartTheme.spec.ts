import { afterEach, describe, expect, it } from 'vitest'
import { getChartTheme } from '../chartTheme'

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
