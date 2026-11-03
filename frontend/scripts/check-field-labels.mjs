import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseSfc } from 'vue/compiler-sfc'
import ts from 'typescript'
export const attribute = (node, name) => node.props?.find(p => p.type === 6 && p.name === name || p.type === 7 && p.name === 'bind' && p.arg?.content === name)
export const valueOf = prop => prop?.type === 6 ? prop.value?.content : prop?.exp?.content
const populated = prop => !!valueOf(prop)?.trim() && !/^(?:''|""|null|undefined|false)$/.test(valueOf(prop).trim())
export const contentText = (node, excluded = () => false) => excluded(node) ? '' : node.type === 2 ? node.content.trim() : node.type === 5 ? node.content.content.trim() : (node.children ?? []).filter(n => !['input', 'select', 'textarea', 'button', 'svg', 'Icon'].includes(n.tag)).map(n => contentText(n, excluded)).filter(Boolean).join(' ')
const hiddenFromLayout = node => !!attribute(node, 'hidden') || /(?:^|\s)hidden(?:\s|$)/.test(valueOf(attribute(node, 'class')) ?? '') && !/\w+:(?:flex|block|grid|inline)/.test(valueOf(attribute(node, 'class')) ?? '') || /(?:display\s*:\s*none|visibility\s*:\s*hidden)/.test(valueOf(attribute(node, 'style')) ?? '')
const visuallyHidden = node => hiddenFromLayout(node) || /(?:^|\s)sr-only(?:\s|$)/.test(valueOf(attribute(node, 'class')) ?? '')
const inaccessible = node => hiddenFromLayout(node) || valueOf(attribute(node, 'aria-hidden')) === 'true'
const identity = prop => {
  const value = valueOf(prop)?.trim()
  return prop?.type === 6 ? `literal:${value}` : /^(?:'[^']*'|"[^"]*")$/.test(value ?? '') ? `literal:${value.slice(1, -1)}` : `binding:${value}`
}

const readableName = prop => {
  if (!populated(prop)) return false
  if (prop.type === 6) return true
  const ast = ts.createSourceFile('name.ts', `(${valueOf(prop)})`, ts.ScriptTarget.Latest, true)
  const inspect = node => {
    if (ts.isParenthesizedExpression(node)) return inspect(node.expression)
    if (ts.isConditionalExpression(node)) return inspect(node.whenTrue) && inspect(node.whenFalse)
    if (ts.isIdentifier(node) && ['undefined', 'null', 'false'].includes(node.text)) return false
    if (node.kind === ts.SyntaxKind.NullKeyword || node.kind === ts.SyntaxKind.FalseKeyword) return false
    return !ts.isStringLiteralLike(node) || !!node.text.trim()
  }
  const expression = ast.statements[0]?.expression
  return !!expression && inspect(expression)
}
export function inspectFieldTemplate(source, file = 'src/Fixture.vue') {
  const parsed = parseSfc(source, { filename: file }), nodes = []
  function visit(node, ancestors = []) {
    if (node.type === 1) nodes.push({ node, ancestors })
    for (const child of node.children ?? []) visit(child, [...ancestors, node])
  }
  if (parsed.descriptor.template?.ast) visit(parsed.descriptor.template.ast)
  const hasText = (node, excluded) => !!contentText(node, excluded) && !(nodes.find(x => x.node === node)?.ancestors ?? []).some(excluded)
  const visible = node => hasText(node, visuallyHidden)
  const accessible = node => hasText(node, inaccessible)
  const fields = [], buttons = []
  for (const { node, ancestors } of nodes) {
    if (['input', 'select', 'textarea', 'Input', 'SearchInput', 'Select', 'ProxySelector'].includes(node.tag)) {
      const type = valueOf(attribute(node, 'type')) ?? 'text'
      const hidden = valueOf(attribute(node, 'aria-hidden')) === 'true' && valueOf(attribute(node, 'tabindex')) === '-1' || type === 'hidden' || type === 'file' && (visuallyHidden(node) || ancestors.some(visuallyHidden))
      const id = attribute(node, 'id'), labelledby = attribute(node, 'aria-labelledby')
      const linkedLabels = populated(id) ? nodes.filter(({ node: label }) => label.tag === 'label' && populated(attribute(label, 'for')) && identity(attribute(label, 'for')) === identity(id)).map(({ node }) => node) : []
      const captions = populated(labelledby) ? nodes.filter(({ node: label }) => populated(attribute(label, 'id')) && identity(attribute(label, 'id')) === identity(labelledby)).map(({ node }) => node) : []
      const wrappers = ancestors.filter(a => a.tag === 'label')
      const linked = linkedLabels.find(visible), caption = captions.find(visible), wrapper = wrappers.find(visible)
      // SearchInput owns a localized aria-label, including when callers omit label.
      const searchInput = node.tag === 'SearchInput'
      const filter = searchInput || (file === 'src/components/common/SearchInput.vue' && node.tag === 'input') || [node, ...ancestors, ...linkedLabels].some(a => identity(attribute(a, 'data-field-role')) === 'literal:filter')
      const accessibleName = searchInput || readableName(attribute(node, 'aria-label')) || (node.tag === 'Select' && readableName(attribute(node, 'ariaLabel'))) || (node.tag === 'Input' && readableName(attribute(node, 'label'))) || [...linkedLabels, ...wrappers].some(accessible) || captions.some(label => !!contentText(label))
      const filterName = filter && accessibleName
      const componentLabel = node.tag === 'Input' && populated(attribute(node, 'label'))
      fields.push({ node, ancestors, file, type, hidden, filter, caption: linked || caption || wrapper, category: hidden ? 'hidden-upload-or-value' : filter ? filterName ? 'filter-accessible-name' : 'missing-filter-name' : linked ? 'label-for' : caption ? 'visible-labelledby' : wrapper ? 'wrapping-label' : componentLabel ? 'component-label' : 'missing-visible-label', ok: !!(hidden || (filter ? filterName : linked || caption || wrapper || componentLabel)) })
    }
    if (node.tag === 'button' && !contentText(node) && !readableName(attribute(node, 'aria-label')) && !readableName(attribute(node, 'aria-labelledby')) && !readableName(attribute(node, 'title')) && !(valueOf(attribute(node, 'role')) === 'switch' && file === 'src/components/common/Toggle.vue')) buttons.push({ file, line: node.loc.start.line, code: 'button-name', message: 'Icon-only button requires a nonempty readable name' })
  }
  return { nodes, fields, buttons, parseErrors: parsed.errors }
}
export async function auditFieldLabels(root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')) {
  const errors = [], records = [], stats = { vue: 0, fields: 0, categories: {}, unnamedButtons: 0 }
  async function walk(directory) {
    for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
      if (entry.isDirectory() && !['node_modules', 'dist', 'build', '__tests__', '__fixtures__', 'tests'].includes(entry.name)) await walk(path.join(directory, entry.name))
      else if (entry.isFile() && entry.name.endsWith('.vue')) {
        const file = path.relative(root, path.join(directory, entry.name)), source = await fs.readFile(path.join(directory, entry.name), 'utf8')
        const result = inspectFieldTemplate(source, file)
        stats.vue++
        for (const error of result.parseErrors) errors.push({ file, line: error.loc?.start?.line ?? 1, code: 'parse', message: String(error.message ?? error) })
        for (const field of result.fields) {
          stats.fields++; stats.categories[field.category] = (stats.categories[field.category] ?? 0) + 1
          const record = { file, line: field.node.loc.start.line, tag: field.node.tag, type: field.type, category: field.category, model: field.node.props.find(p => p.type === 7 && p.name === 'model')?.exp?.content?.trim() }
          records.push(record)
          if (!field.ok) errors.push({ ...record, code: field.filter ? 'filter-name' : 'visible-label', message: field.filter ? 'Filter requires a nonempty accessible name; placeholder alone is insufficient' : 'Field requires a persistent visible label; placeholder/aria-label alone is insufficient' })
        }
        stats.unnamedButtons += result.buttons.length; errors.push(...result.buttons)
      }
    }
  }
  await walk(path.join(root, 'src'))
  return { ok: !errors.length, stats, errors, records }
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const result = await auditFieldLabels()
  if (process.argv.includes('--json')) console.log(JSON.stringify(result, null, 2))
  else {
    console.log(`Visible field labels and icon names: ${result.ok ? 'PASS' : 'FAIL'}; ${result.stats.fields} fields in ${result.stats.vue} Vue files`)
    console.log(JSON.stringify(result.stats.categories))
    for (const error of result.errors) console.error(`${error.file}:${error.line} [${error.code}] ${error.message}`)
  }
  if (!result.ok) process.exitCode = 1
}
