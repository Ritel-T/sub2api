// @vitest-environment node
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { beforeAll, describe, expect, it } from 'vitest'
import postcss, { type Root } from 'postcss'
import tailwindcss from 'tailwindcss'
import config from '../../../../tailwind.config.js'

describe('compiled typography integration', () => {
  let css: Root
  beforeAll(async () => {
    const source = await readFile(resolve(process.cwd(), 'src/style.css'), 'utf8')
    const result = await postcss([tailwindcss({ ...config, content: [{ raw: '<p class="text-xs text-sm font-sans font-display" />', extension: 'html' }] })]).process(`${source}\n.type-apply-xs { @apply text-xs; }\n.type-apply-sm { @apply text-sm; }`, { from: resolve(process.cwd(), 'src/style.css') })
    css = result.root
  })
  function declarations(selector: string) {
    const result: Record<string, string> = {}
    css.walkRules(selector, rule => { rule.walkDecls(decl => { result[decl.prop] = decl.value }) })
    return result
  }
  it.each(['xs', 'sm'])('direct %s utility and compiled apply share the same scale', size => {
    const direct = declarations(`.text-${size}`)
    const applied = declarations(`.type-apply-${size}`)
    expect(direct['font-size']).toBe(`var(--ritel-text-${size})`)
    expect(direct['font-size']).toBe(applied['font-size'])
    expect(direct['line-height']).toBe('var(--ritel-leading-body)')
    expect(direct['line-height']).toBe(applied['line-height'])
  })
  it('compiled family utilities share the role sources', () => {
    expect(declarations('.font-sans')['font-family']).toBe('var(--ritel-ui)')
    expect(declarations('.font-display')['font-family']).toBe('var(--ritel-display)')
  })
})
