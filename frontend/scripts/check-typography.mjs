#!/usr/bin/env node
/**
 * Enforce the authored-heading architecture, without treating body copy, data
 * labels, editor content, and chart canvas text as interchangeable headings.
 * Run from any directory: node frontend/scripts/check-typography.mjs [--json].
 */
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import { parse as parseSfc } from 'vue/compiler-sfc'
import postcss from 'postcss'
import ts from 'typescript'

const require = createRequire(import.meta.url)
// Tailwind already depends on this parser; no new runtime or dev dependency.
const selectorParser = createRequire(require.resolve('tailwindcss'))('postcss-selector-parser')
export const headingRoles = ['page', 'section', 'subsection', 'minor', 'dialog', 'auth', 'hero', 'empty', 'metric', 'subtle']
export const textScales = ['2xs', 'xs', 'sm', 'base', 'lg', 'xl', '2xl', '3xl', '4xl', '5xl', '6xl', '7xl', '8xl', '9xl']
const familyMap = { sans: 'ui', serif: 'display', display: 'display', heading: 'heading', mono: 'mono' }
const canonicalFamilies = new Set(Object.values(familyMap).map(value => `--ritel-${value}`))
const headingProperties = new Set(['font', 'font-family', 'font-size', 'font-weight', 'font-style', 'font-stretch', 'line-height', 'letter-spacing', 'text-transform'])
const headingTag = /^h[1-6]$/i
const runtimeExtensions = new Set(['.vue', '.css', '.ts', '.js', '.tsx', '.jsx'])
const skipDirectory = new Set(['node_modules', 'build', 'dist', '__tests__', '__fixtures__', 'test', 'tests', '.git'])
const centralFamilyFile = 'public/brand/brand.css'
const centralRoleFile = 'public/brand/typography.css'
const headingComponent = 'src/components/common/AppHeading.vue'

const normalPath = value => value.split(path.sep).join('/')
const cssName = value => value.replace(/[A-Z]/g, letter => `-${letter.toLowerCase()}`)
const isTypeProperty = value => headingProperties.has(cssName(value)) || /^(?:--ritel-type-|--type-(?:family|size|weight|leading|tracking)$)/.test(value)
const lineAt = (source, offset) => source.slice(0, offset).split('\n').length
const staticProp = (node, name) => node.props?.find(prop => prop.type === 6 && prop.name === name)?.value?.content

function utilityBase(value) {
  let depth = 0
  let start = 0
  for (let i = 0; i < value.length; i++) {
    if ('[('.includes(value[i])) depth++
    else if ('])'.includes(value[i])) depth--
    else if (value[i] === ':' && depth === 0) start = i + 1
  }
  return value.slice(start).replace(/^!|!$/g, '')
}

export function isTypographyUtility(value) {
  const base = utilityBase(value)
  if (/^(?:font-|leading-|tracking-)/.test(base) || /^(?:italic|not-italic|uppercase|lowercase|capitalize|normal-case)$/.test(base)) return true
  if (/^text-(?:2xs|xs|sm|base|lg|xl|[2-9]xl)(?:\/.*)?$/.test(base)) return true
  if (/^\[(?:font(?:-[\w-]+)?|line-height|letter-spacing|text-transform|--ritel-type-[\w-]+|--type-(?:family|size|weight|leading|tracking)):/.test(base)) return true
  // Arbitrary text colors stay legal. Ambiguous text-[var(...)] must use an
  // explicit color: hint; otherwise it could silently override font-size.
  if (/^text-\[/.test(base) && !/^text-\[(?:color:|#|(?:rgb|hsl|oklch|oklab|lab|lch)\()/.test(base)) return true
  return false
}

function expressionNode(expression) {
  const source = ts.createSourceFile('expression.ts', `const __value = (${expression});`, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  return source.statements[0]?.declarationList?.declarations[0]?.initializer
}

function unwrap(node) {
  while (node && (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isNonNullExpression(node) || ts.isSatisfiesExpression(node))) node = node.expression
  return node
}

function propertyName(node) {
  if (!node) return null
  return ts.isIdentifier(node) || ts.isStringLiteralLike(node) || ts.isNumericLiteral(node) ? node.text : null
}

function bindingsIn(scripts) {
  const result = new Map()
  for (const script of scripts) {
    const ast = ts.createSourceFile('script.ts', script.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
    const visit = node => {
      if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) result.set(node.name.text, node.initializer)
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
  return result
}

/** A closed static subset prevents variable/concatenation escape hatches. */
function valuesOf(expression, mode, bindings, seen = new Set()) {
  const node = unwrap(expression)
  if (!node) return { values: [], opaque: true }
  if (ts.isStringLiteralLike(node)) return { values: [node.text], opaque: false }
  if (node.kind === ts.SyntaxKind.NullKeyword || node.kind === ts.SyntaxKind.FalseKeyword || node.kind === ts.SyntaxKind.TrueKeyword) return { values: [], opaque: false }
  if (ts.isIdentifier(node)) {
    if (node.text === 'undefined') return { values: [], opaque: false }
    if (seen.has(node.text) || !bindings.has(node.text)) return { values: [], opaque: true }
    return valuesOf(bindings.get(node.text), mode, bindings, new Set([...seen, node.text]))
  }
  if (ts.isArrayLiteralExpression(node)) return mergeValues(node.elements.map(item => valuesOf(item, mode, bindings, seen)))
  if (ts.isConditionalExpression(node)) return mergeValues([valuesOf(node.whenTrue, mode, bindings, seen), valuesOf(node.whenFalse, mode, bindings, seen)])
  if (ts.isBinaryExpression(node)) {
    if (node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) return valuesOf(node.right, mode, bindings, seen)
    if ([ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(node.operatorToken.kind)) return mergeValues([valuesOf(node.left, mode, bindings, seen), valuesOf(node.right, mode, bindings, seen)])
    if (node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
      const left = valuesOf(node.left, mode, bindings, seen)
      const right = valuesOf(node.right, mode, bindings, seen)
      return { values: left.values.flatMap(a => right.values.map(b => a + b)), opaque: left.opaque || right.opaque }
    }
  }
  if (ts.isObjectLiteralExpression(node)) {
    const values = []
    let opaque = false
    for (const prop of node.properties) {
      const name = propertyName(prop.name)
      if (!name) { opaque = true; continue }
      if (mode === 'class') values.push(name)
      else if (mode === 'style') values.push(`${cssName(name)}: ${ts.isPropertyAssignment(prop) ? prop.initializer.getText() : 'unknown'}`)
    }
    return { values, opaque }
  }
  if (ts.isNoSubstitutionTemplateLiteral(node)) return { values: [node.text], opaque: false }
  return { values: [], opaque: true }
}

function mergeValues(items) {
  return { values: items.flatMap(item => item.values), opaque: items.some(item => item.opaque) }
}

function templateWalk(node, callback, ancestors = []) {
  if (node.type === 1) callback(node, ancestors)
  for (const child of node.children ?? []) templateWalk(child, callback, [...ancestors, node])
}

function hasExplicitTitleOwnership(layout) {
  const prop = layout.props.find(item => item.type === 7 && item.name === 'bind' && item.arg?.isStatic && ['show-page-title', 'showPageTitle'].includes(item.arg.content))
  if (!prop?.exp?.content.trim()) return false
  const value = unwrap(expressionNode(prop.exp.content))
  // A literal false disables the fallback. A dynamic expression declares that
  // the page owns the mutually-exclusive conditions; literal true/string values
  // would preserve the duplicate fallback rather than establish ownership.
  if (!value || value.kind === ts.SyntaxKind.TrueKeyword || ts.isStringLiteralLike(value) || ts.isNumericLiteral(value) || value.kind === ts.SyntaxKind.NullKeyword) return false
  return true
}

function familyIsCentral(value) {
  return /^(?:inherit|initial|unset|revert|revert-layer)$/.test(value.trim()) || /^var\(--ritel-(?:ui|body|display|heading|mono)\)$/.test(value.trim())
}

function selectorTargetsHeading(selector, classes) {
  let target = false
  selectorParser(selectors => {
    selectors.each(branch => {
      let nodes = branch.nodes
      const lastCombinator = nodes.findLastIndex(node => node.type === 'combinator')
      nodes = nodes.slice(lastCombinator + 1)
      function inspect(node) {
        if (node.type === 'tag' && (headingTag.test(node.value) || /^app-heading$/i.test(node.value))) target = true
        if (node.type === 'class' && (classes.has(node.value) || node.value === 'ritel-type')) target = true
        if (node.type === 'id' && classes.has(`#${node.value}`)) target = true
        if (node.type === 'attribute' && (node.attribute === 'data-type-role' || (node.attribute === 'role' && node.value === 'heading'))) target = true
        // :not(.heading) does not target headings; :has(h2) targets a parent.
        if (node.type === 'pseudo' && [':is', ':where', ':deep', '::v-deep', ':global'].includes(node.value)) node.each?.(child => child.each?.(inspect))
      }
      nodes.forEach(inspect)
    })
  }).processSync(selector)
  return target
}

async function runtimeFiles(root, dir) {
  const result = []
  const absolute = path.join(root, dir)
  let entries
  try { entries = await fs.readdir(absolute, { withFileTypes: true }) } catch (error) { if (error.code === 'ENOENT') return []; throw error }
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    if (entry.isSymbolicLink() || skipDirectory.has(entry.name)) continue
    const relative = normalPath(path.join(dir, entry.name))
    if (entry.isDirectory()) result.push(...await runtimeFiles(root, relative))
    else if (runtimeExtensions.has(path.extname(entry.name)) && !/\.(?:spec|test|d)\.[cm]?[jt]sx?$/.test(entry.name)) result.push(relative)
  }
  return result
}

export async function auditTypography({ root = fileURLToPath(new URL('..', import.meta.url)), exceptions: suppliedExceptions } = {}) {
  root = path.resolve(root)
  const errors = []
  const review = []
  const accepted = []
  const stats = { vue: 0, authoredHeadings: 0, rawHeadings: 0, css: 0, scripts: 0 }
  const add = (code, file, line, message, detail = {}) => errors.push({ code, file, line, message, ...detail })
  let exceptions = suppliedExceptions
  if (!exceptions) {
    try { exceptions = JSON.parse(await fs.readFile(path.join(root, 'scripts/typography-exceptions.json'), 'utf8')) }
    catch (error) { add('exception-registry', 'scripts/typography-exceptions.json', 1, error.message); exceptions = { version: 1, rawHeadings: [], contentSelectors: [] } }
  }
  if (exceptions.version !== 1) add('exception-registry', 'scripts/typography-exceptions.json', 1, 'Unsupported exception registry version')
  const rawExceptions = (exceptions.rawHeadings ?? []).map(item => ({ ...item, matched: 0 }))
  const contentExceptions = (exceptions.contentSelectors ?? []).map(item => ({ ...item, matched: 0 }))
  for (const item of rawExceptions) {
    if (!item.file || !headingTag.test(item.tag) || !item.id || item.class !== 'sr-only' || item.expectedCount !== 1 || !item.reason) add('exception-registry', 'scripts/typography-exceptions.json', 1, 'Raw heading exceptions require exact file/tag/id/sr-only, expectedCount 1, and reason')
  }
  for (const item of contentExceptions) {
    if (!item.file || !item.selector || item.expectedCount !== 1 || !item.reason) add('exception-registry', 'scripts/typography-exceptions.json', 1, 'Content exceptions require exact file and full selector, expectedCount 1, and reason')
  }

  const files = [...await runtimeFiles(root, 'src'), ...await runtimeFiles(root, 'public/brand')]
  const styleBlocks = []
  const allHeadingClasses = new Set()
  for (const file of files) {
    const source = await fs.readFile(path.join(root, file), 'utf8')
    if (file.endsWith('.css')) {
      styleBlocks.push({ file, source, offset: 0, headingClasses: null })
      continue
    }
    if (!file.endsWith('.vue')) {
      scanScript(source, file, 0)
      continue
    }
    stats.vue++
    const parsed = parseSfc(source, { filename: file })
    for (const error of parsed.errors) add('parse-vue', file, error.loc?.start?.line ?? 1, String(error.message ?? error))
    const descriptor = parsed.descriptor
    const scripts = [descriptor.script, descriptor.scriptSetup].filter(Boolean)
    const bindings = bindingsIn(scripts)
    const headingClasses = new Set()
    for (const script of scripts) scanScript(script.content, file, script.loc.start.line - 1)
    if (descriptor.template?.ast) templateWalk(descriptor.template.ast, (node, ancestors) => {
      const line = node.loc.start.line
      const raw = headingTag.test(node.tag)
      const isHeading = node.tag === 'AppHeading' || node.tag === 'app-heading'
      const roleHeading = staticProp(node, 'role') === 'heading'
      if (raw) {
        stats.rawHeadings++
        const match = rawExceptions.find(item => item.file === file && item.tag === node.tag && item.id === staticProp(node, 'id') && staticProp(node, 'class')?.split(/\s+/).includes(item.class))
        if (match) { match.matched++; accepted.push({ file, line, kind: 'sr-only', reason: match.reason }) }
        else if (file !== headingComponent) add('raw-heading', file, line, `Use AppHeading with an explicit role instead of <${node.tag}>`)
      }
      if (roleHeading && !isHeading && !raw && file !== headingComponent) add('raw-heading', file, line, 'A role="heading" also requires AppHeading')
      if (node.tag === 'component' && file !== headingComponent) {
        const dynamicIs = node.props.find(prop => prop.type === 7 && prop.name === 'bind' && prop.arg?.content === 'is')
        const values = dynamicIs?.exp ? valuesOf(expressionNode(dynamicIs.exp.content), 'class', bindings).values : [staticProp(node, 'is')]
        if (values.some(value => value && headingTag.test(value))) add('raw-heading', file, line, 'Dynamic authored heading bypasses AppHeading')
      }
      const isVisibleRaw = raw && !staticProp(node, 'class')?.split(/\s+/).includes('sr-only')
      const trackHeading = isHeading || isVisibleRaw || roleHeading
      const headingId = staticProp(node, 'id')
      if (trackHeading && headingId) { headingClasses.add(`#${headingId}`); allHeadingClasses.add(`#${headingId}`) }
      if (isHeading) {
        stats.authoredHeadings++
        const variant = staticProp(node, 'variant')
        if (!headingRoles.includes(variant)) add('heading-role', file, line, `AppHeading needs an explicit static variant (${headingRoles.join(', ')})`)
        const semanticTag = staticProp(node, 'as')
        const isPageHeading = semanticTag === 'h1' || (!semanticTag && ['page', 'auth', 'hero'].includes(variant))
        const layout = ancestors.findLast(parent => parent.type === 1 && ['AppLayout', 'app-layout'].includes(parent.tag))
        if (isPageHeading && layout && !hasExplicitTitleOwnership(layout)) add('page-title-ownership', file, line, 'A local h1 needs AppLayout :show-page-title="false" or an explicit conditional policy, so the layout fallback cannot silently duplicate it')
      }
      for (const prop of node.props ?? []) {
        const attribute = prop.type === 6 ? prop.name : prop.type === 7 && prop.name === 'bind' ? prop.arg?.content : null
        const value = prop.type === 6 ? prop.value?.content ?? '' : prop.exp?.content ?? ''
        const dynamic = prop.type === 7
        if (isHeading && prop.type === 7 && prop.name === 'bind' && (!prop.arg || !prop.arg.isStatic)) add('heading-opaque-attributes', file, line, 'Heading v-bind spreads/dynamic attribute names can override typography; bind explicit attributes')
        if (attribute === 'font-family' && !familyIsCentral(value) && !dynamic) add('font-family-literal', file, line, 'Use a central font-family variable', { value })
        if (attribute === 'style' && !isHeading) {
          const styles = dynamic ? valuesOf(expressionNode(value), 'style', bindings) : { values: [value], opaque: false }
          for (const style of styles.values) {
            try {
              postcss.parse(`a{${style}}`).walkDecls(decl => {
                const family = decl.value.replace(/^(["'])(.*)\1$/, '$2')
                if (decl.prop === 'font-family' && !familyIsCentral(family) && (!dynamic || /^['"]/.test(decl.value))) add('font-family-literal', file, line, 'Inline styles must reference the central font family', { value: family })
              })
            } catch { /* General presentation bindings stay outside the heading gate. */ }
          }
        }
        if (!trackHeading) continue
        if (attribute === 'class') {
          const classes = dynamic ? valuesOf(expressionNode(value), 'class', bindings) : { values: [value], opaque: false }
          for (const token of classes.values.flatMap(item => item.split(/\s+/)).filter(Boolean)) {
            headingClasses.add(token)
            allHeadingClasses.add(token)
            if (isHeading && isTypographyUtility(token)) add('heading-utility', file, line, `AppHeading typography belongs to its role token, not ${token}`)
          }
          if (isHeading && classes.opaque) add('heading-opaque-class', file, line, 'Cannot statically verify heading classes; use literal presentation classes or a static conditional object')
        }
        if (isHeading && attribute === 'style') {
          const styles = dynamic ? valuesOf(expressionNode(value), 'style', bindings) : { values: [value], opaque: false }
          for (const style of styles.values) {
            try {
              postcss.parse(`a{${style}}`).walkDecls(decl => {
                if (isTypeProperty(decl.prop)) add('heading-inline-type', file, line, `AppHeading cannot override ${decl.prop}`)
              })
            } catch { add('heading-opaque-style', file, line, 'Cannot statically parse heading style') }
          }
          if (styles.opaque) add('heading-opaque-style', file, line, 'Cannot statically verify heading styles; bind an explicit object of presentation properties')
        }
        if (isHeading && attribute && isTypeProperty(attribute)) add('heading-inline-type', file, line, `AppHeading cannot override ${attribute}`)
        if (isHeading && attribute === 'data-type-role') add('heading-role-override', file, line, 'Use variant; do not override the internal data-type-role attribute')
      }
    })
    for (const style of descriptor.styles) styleBlocks.push({ file, source: style.content, offset: style.loc.start.line - 1, headingClasses: style.scoped ? headingClasses : null })
  }

  for (const block of styleBlocks) scanCss(block)
  await scanTailwind()
  await scanCentralContract()
  for (const item of [...rawExceptions, ...contentExceptions]) {
    if (item.matched !== item.expectedCount) add('stale-exception', item.file, 1, `Exception expected ${item.expectedCount} match but found ${item.matched}: ${item.id ?? item.selector}`)
  }
  errors.sort((a, b) => a.file.localeCompare(b.file) || a.line - b.line || a.code.localeCompare(b.code))
  return { ok: errors.length === 0, root, stats, errors, accepted, review }

  function scanScript(source, file, offset) {
    stats.scripts++
    const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, /x$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
    function visit(node) {
      if (ts.isCallExpression(node) && /^(?:h|createVNode|createElementVNode)$/.test(node.expression.getText(ast)) && ts.isStringLiteralLike(node.arguments[0] ?? {})) {
        if (headingTag.test(node.arguments[0].text) && file !== headingComponent) add('raw-render-heading', file, offset + ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1, 'Render-function headings must use AppHeading')
      }
      if (ts.isPropertyAssignment(node)) {
        const name = propertyName(node.name)
        const containerName = propertyName(node.parent?.parent?.name)
        if (name === 'fontFamily' || name === 'font-family' || (name === 'family' && containerName === 'font')) {
          const value = unwrap(node.initializer)
          if (ts.isStringLiteralLike(value) && !familyIsCentral(value.text)) add('font-family-literal', file, offset + ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1, 'Use the shared font family resolver/token rather than a copied stack', { value: value.text })
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }

  function scanCss({ file, source, offset, headingClasses }) {
    stats.css++
    let ast
    try { ast = postcss.parse(source, { from: file }) }
    catch (error) { add('parse-css', file, offset + (error.line ?? 1), error.reason); return }
    ast.walkDecls(decl => {
      const line = offset + (decl.source?.start?.line ?? 1)
      let inFontFace = false
      for (let parent = decl.parent; parent; parent = parent.parent) if (parent.type === 'atrule' && parent.name === 'font-face') inFontFace = true
      if (decl.prop === 'font-family' && !inFontFace && file !== centralFamilyFile && file !== centralRoleFile && !familyIsCentral(decl.value)) add('font-family-literal', file, line, 'Font families must reference the central brand tokens', { value: decl.value })
      if (canonicalFamilies.has(decl.prop) && file !== centralFamilyFile) add('font-family-duplicate', file, line, `${decl.prop} is defined only in ${centralFamilyFile}`)
      if (/^(?:--ritel-type-|--ritel-text-|--type-(?:family|size|weight|leading|tracking)$)/.test(decl.prop) && file !== centralRoleFile) add('type-token-duplicate', file, line, `Role token definitions belong to ${centralRoleFile}`)
      if (isTypeProperty(decl.prop) && !inFontFace && file !== centralRoleFile && decl.parent?.type === 'rule') {
        const selector = decl.parent.selector
        if (targets(selector, file, headingClasses, line)) add('heading-css-type', file, line, `Heading selector ${selector} cannot define ${decl.prop} outside the shared role stylesheet`, { value: decl.value })
        else if (!['font-family'].includes(decl.prop) && !decl.prop.startsWith('--')) review.push({ file, line, kind: 'non-heading-css', selector, property: decl.prop, value: decl.value })
      }
    })
    ast.walkAtRules('apply', rule => {
      const tokens = rule.params.split(/\s+/).filter(isTypographyUtility)
      if (!tokens.length || file === centralRoleFile || rule.parent?.type !== 'rule') return
      const line = offset + (rule.source?.start?.line ?? 1)
      if (targets(rule.parent.selector, file, headingClasses, line)) add('heading-css-apply', file, line, `Heading @apply bypasses role typography: ${tokens.join(' ')}`, { selector: rule.parent.selector })
      else review.push({ file, line, kind: 'non-heading-apply', selector: rule.parent.selector, values: tokens })
    })
    // Content exceptions count rules, not individual declarations or @apply.
    ast.walkRules(rule => {
      const match = contentExceptions.find(item => item.file === file && item.selector === rule.selector)
      if (match) { match.matched++; accepted.push({ file, line: offset + (rule.source?.start?.line ?? 1), kind: 'user-content', selector: rule.selector, reason: match.reason }) }
    })
  }

  function targets(selector, file, localClasses, line) {
    if (contentExceptions.some(item => item.file === file && item.selector === selector)) return false
    try { return selectorTargetsHeading(selector, localClasses ?? allHeadingClasses) }
    catch (error) { add('parse-selector', file, line, error.message); return true }
  }

  async function scanTailwind() {
    const file = 'tailwind.config.js'
    let source
    try { source = await fs.readFile(path.join(root, file), 'utf8') }
    catch (error) { add('tailwind-family', file, 1, error.message); return }
    const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS)
    let maps = 0
    let sizeMaps = 0
    function visit(node) {
      if (ts.isPropertyAssignment(node) && propertyName(node.name) === 'fontFamily') {
        maps++
        const mapping = unwrap(node.initializer)
        if (!ts.isObjectLiteralExpression(mapping)) { add('tailwind-family', file, 1, 'fontFamily must be an explicit central variable mapping'); return }
        for (const [family, variable] of Object.entries(familyMap)) {
          const prop = mapping.properties.find(item => propertyName(item.name) === family)
          const values = prop && ts.isPropertyAssignment(prop) ? valuesOf(prop.initializer, 'class', new Map()) : { values: [], opaque: true }
          if (values.opaque || values.values.length !== 1 || values.values[0] !== `var(--ritel-${variable})`) add('tailwind-family', file, prop ? lineAt(source, prop.getStart(ast)) : 1, `${family} must be ['var(--ritel-${variable})']`)
        }
        for (const prop of mapping.properties) if (!Object.hasOwn(familyMap, propertyName(prop.name))) add('tailwind-family', file, lineAt(source, prop.getStart(ast)), 'Add new family roles to the central contract before configuring Tailwind')
      }
      if (ts.isPropertyAssignment(node) && propertyName(node.name) === 'fontSize') {
        sizeMaps++
        const mapping = unwrap(node.initializer)
        if (!ts.isObjectLiteralExpression(mapping)) { add('tailwind-size', file, lineAt(source, node.getStart(ast)), 'fontSize must be an explicit central variable mapping'); return }
        for (const scale of textScales) {
          const props = mapping.properties.filter(item => propertyName(item.name) === scale)
          const prop = props[0]
          const line = prop ? lineAt(source, prop.getStart(ast)) : 1
          const tuple = prop && ts.isPropertyAssignment(prop) ? unwrap(prop.initializer) : null
          const size = tuple && ts.isArrayLiteralExpression(tuple) ? unwrap(tuple.elements[0]) : null
          const options = tuple && ts.isArrayLiteralExpression(tuple) ? unwrap(tuple.elements[1]) : null
          if (props.length !== 1 || !tuple || !ts.isArrayLiteralExpression(tuple) || tuple.elements.length !== 2 || !size || !ts.isStringLiteralLike(size) || size.text !== `var(--ritel-text-${scale})`) add('tailwind-size', file, line, `${scale} must use exactly ['var(--ritel-text-${scale})', { lineHeight: 'var(--ritel-leading-body)' }]`)
          const leadingProp = options && ts.isObjectLiteralExpression(options) && options.properties.length === 1 ? options.properties[0] : null
          const leading = leadingProp && ts.isPropertyAssignment(leadingProp) && propertyName(leadingProp.name) === 'lineHeight' ? unwrap(leadingProp.initializer) : null
          if (!leading || !ts.isStringLiteralLike(leading) || leading.text !== 'var(--ritel-leading-body)') add('tailwind-leading', file, line, `${scale} lineHeight must use the shared --ritel-leading-body token without other typography overrides`)
        }
        for (const prop of mapping.properties) if (!textScales.includes(propertyName(prop.name))) add('tailwind-size', file, lineAt(source, prop.getStart(ast)), 'Add new text scales to the central contract before configuring Tailwind')
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
    if (maps !== 1) add('tailwind-family', file, 1, `Expected one fontFamily mapping, found ${maps}`)
    if (sizeMaps !== 1) add('tailwind-size', file, 1, `Expected one fontSize mapping, found ${sizeMaps}`)
  }

  async function scanCentralContract() {
    let source
    try { source = await fs.readFile(path.join(root, centralRoleFile), 'utf8') }
    catch (error) { add('central-role-contract', centralRoleFile, 1, error.message); return }
    const ast = postcss.parse(source)
    const declarations = new Map()
    ast.walkDecls(decl => { if (!declarations.has(decl.prop)) declarations.set(decl.prop, decl.value) })
    for (const token of [...textScales.map(scale => `--ritel-text-${scale}`), '--ritel-leading-body']) {
      if (!declarations.has(token)) add('central-scale-contract', centralRoleFile, 1, `Missing shared text-scale token ${token}`)
    }
    const dimensions = ['family', 'size', 'weight', 'leading', 'tracking']
    for (const role of headingRoles) {
      for (const dimension of dimensions) {
        const token = `--ritel-type-${role}-${dimension}`
        if (!declarations.has(token)) add('central-role-contract', centralRoleFile, 1, `Missing role token ${token}`)
      }
      const selector = `.ritel-type[data-type-role="${role}"]`
      const rules = ast.nodes.filter(node => node.type === 'rule' && node.selector === selector)
      if (rules.length !== 1) { add('central-role-contract', centralRoleFile, 1, `Expected one explicit ${role} role selector`); continue }
      for (const dimension of dimensions) {
        const decl = rules[0].nodes.find(node => node.type === 'decl' && node.prop === `--type-${dimension}`)
        if (decl?.value !== `var(--ritel-type-${role}-${dimension})`) add('central-role-contract', centralRoleFile, 1, `${role} must consume the shared ${dimension} token`)
      }
    }
    const consumers = { 'font-family': 'family', 'font-size': 'size', 'font-weight': 'weight', 'line-height': 'leading', 'letter-spacing': 'tracking' }
    const roleBase = ast.nodes.find(node => node.type === 'rule' && node.selector === '.ritel-type[data-type-role]')
    for (const [prop, dimension] of Object.entries(consumers)) {
      if (!roleBase?.nodes.some(node => node.type === 'decl' && node.prop === prop && node.value === `var(--type-${dimension})`)) add('central-role-contract', centralRoleFile, 1, `The base role selector must consume ${prop} from --type-${dimension}`)
    }
    const brand = await fs.readFile(path.join(root, centralFamilyFile), 'utf8').catch(() => '')
    const brandAst = postcss.parse(brand)
    let imported = false
    const definedFamilies = new Set()
    brandAst.walkAtRules('import', rule => {
      const url = rule.params.match(/^url\(\s*(['"]?)(.*?)\1\s*\)$/)?.[2] ?? rule.params.replace(/^(['"])(.*)\1$/, '$2')
      if (/^\.\/typography\.css(?:\?v=[0-9a-f]{16})?$/.test(url)) {
        imported = true
        const version = url.split('?v=')[1]
        if (version && version !== createHash('sha256').update(source).digest('hex').slice(0, 16)) {
          add('central-role-import-version', centralFamilyFile, 1, 'Typography import version must match its exact content hash')
        }
      }
    })
    brandAst.walkDecls(decl => { if (canonicalFamilies.has(decl.prop)) definedFamilies.add(decl.prop) })
    if (!imported) add('central-role-import', centralFamilyFile, 1, 'Brand stylesheet must import ./typography.css')
    for (const family of canonicalFamilies) if (!definedFamilies.has(family)) add('central-family-contract', centralFamilyFile, 1, `Missing central family token ${family}`)
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const index = process.argv.indexOf('--root')
  const result = await auditTypography({ root: index >= 0 ? process.argv[index + 1] : undefined })
  if (process.argv.includes('--json')) console.log(JSON.stringify(result, null, 2))
  else {
    console.log(`Typography architecture: ${result.ok ? 'PASS' : 'FAIL'}; ${result.stats.vue} Vue files, ${result.stats.authoredHeadings} role headings, ${result.stats.rawHeadings} raw headings`)
    for (const error of result.errors) console.error(`${error.file}:${error.line} [${error.code}] ${error.message}`)
    console.log(`${result.accepted.length} exact exceptions; ${result.review.length} non-heading typography locations retained for separate review (use --json)`)
  }
  if (!result.ok) process.exitCode = 1
}
