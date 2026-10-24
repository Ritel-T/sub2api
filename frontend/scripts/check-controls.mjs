#!/usr/bin/env node
/** Keep authored switches and selects named, and switch rendering shared. */
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseSfc } from 'vue/compiler-sfc'

const skipped = new Set(['node_modules', 'dist', 'build', '__tests__', '__fixtures__', 'test', 'tests', '.git'])
const empty = /^(?:\s*|\s*(?:''|""|`\s*`|null|undefined|false)\s*)$/
const normalize = value => value?.replace(/\s+/g, ' ').trim()
const attribute = (node, names) => node.props?.find(prop => prop.type === 6 && names.includes(prop.name) || prop.type === 7 && prop.name === 'bind' && names.includes(prop.arg?.content))
const valueOf = prop => prop?.type === 6 ? prop.value?.content : prop?.exp?.content
const populated = prop => prop && !empty.test(valueOf(prop) ?? '')
const identity = prop => {
  const value = normalize(valueOf(prop))
  return prop?.type === 6 ? `literal:${value}` : /^(?:'[^']*'|"[^"]*")$/.test(value ?? '') ? `literal:${value.slice(1, -1)}` : `binding:${value}`
}
const hasText = node => node.type === 2 ? !!node.content.trim() : node.type === 5 ? !empty.test(node.content.content) : (node.children ?? []).some(hasText)

export function auditControlTemplate(source, { file = 'src/Fixture.vue' } = {}) {
  const errors = []
  const stats = { switches: 0, selects: 0, rawSwitches: 0 }
  const parsed = parseSfc(source, { filename: file })
  if (parsed.errors.length) return { errors: parsed.errors.map(error => ({ file, line: error.loc?.start?.line ?? 1, code: 'parse', message: String(error.message ?? error) })), stats }
  const ast = parsed.descriptor.template?.ast
  if (!ast) return { errors, stats }
  const nodes = []
  const visit = node => { if (node.type === 1) nodes.push(node); for (const child of node.children ?? []) visit(child) }
  visit(ast)
  const aliases = new Map()
  const register = (name, kind) => {
    for (const spelling of [name, name.replace(/\B([A-Z])/g, '-$1').toLowerCase(), name[0].toLowerCase() + name.slice(1)]) {
      // Lowercase select is always the native HTML element in Vue templates.
      if (spelling !== 'select') aliases.set(spelling, kind)
    }
  }
  for (const kind of ['Toggle', 'ToggleSwitch', 'Select']) register(kind, kind)
  for (const match of source.matchAll(/import\s+(\w+)\s+from\s+['"][^'"]*\/(Toggle|ToggleSwitch|Select)\.vue['"]/g)) register(match[1], match[2])
  const labels = new Set(nodes.filter(node => node.tag === 'label' && populated(attribute(node, ['for'])) && hasText(node)).map(node => identity(attribute(node, ['for']))))
  const add = (node, code, message) => errors.push({ file, line: node.loc.start.line, code, message })
  for (const node of nodes) {
    const kind = aliases.get(node.tag)
    if (kind) {
      stats[kind === 'Select' ? 'selects' : 'switches']++
      const names = kind === 'ToggleSwitch' ? ['label'] : ['aria-label', 'aria-labelledby']
      if (kind === 'Select') names.push('ariaLabel')
      const named = node.props.some(prop => names.some(name => attribute({ props: [prop] }, [name])) && populated(prop))
      const id = attribute(node, ['id'])
      const linkedLabel = kind === 'Select' && populated(id) && labels.has(identity(id))
      if (!named && !linkedLabel) add(node, 'control-name', `${kind} needs an explicit nonempty accessible name${kind === 'Select' ? ' or a matching label for/id' : ''}`)
    }
    const role = attribute(node, ['role'])
    if (identity(role) === 'literal:switch') {
      stats.rawSwitches++
      if (file !== 'src/components/common/Toggle.vue') add(node, 'shared-switch', 'Use the shared Toggle instead of rendering role=switch locally')
    }
  }
  return { errors, stats }
}

export async function auditControls({ root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..') } = {}) {
  const errors = []
  const stats = { vue: 0, switches: 0, selects: 0, rawSwitches: 0 }
  async function scan(directory) {
    for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
      if (entry.isDirectory() && !skipped.has(entry.name)) await scan(path.join(directory, entry.name))
      else if (entry.isFile() && entry.name.endsWith('.vue') && !/\.(?:spec|test)\.vue$/.test(entry.name)) {
        const full = path.join(directory, entry.name)
        const file = path.relative(root, full).split(path.sep).join('/')
        const result = auditControlTemplate(await fs.readFile(full, 'utf8'), { file })
        stats.vue++
        for (const key of ['switches', 'selects', 'rawSwitches']) stats[key] += result.stats[key]
        errors.push(...result.errors)
      }
    }
  }
  await scan(path.join(root, 'src'))
  return { ok: errors.length === 0, stats, errors }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const index = process.argv.indexOf('--root')
  const result = await auditControls({ root: index >= 0 ? process.argv[index + 1] : undefined })
  if (process.argv.includes('--json')) console.log(JSON.stringify(result, null, 2))
  else {
    console.log(`Control architecture: ${result.ok ? 'PASS' : 'FAIL'}; ${result.stats.vue} Vue files, ${result.stats.switches} shared switches, ${result.stats.selects} selects, ${result.stats.rawSwitches} switch renderer`)
    for (const error of result.errors) console.error(`${error.file}:${error.line} [${error.code}] ${error.message}`)
  }
  if (!result.ok) process.exitCode = 1
}
