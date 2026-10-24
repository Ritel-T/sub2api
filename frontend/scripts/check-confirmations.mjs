#!/usr/bin/env node
/** Enforce structural confirmation ownership; runtime lifecycle tests cover behavior. */
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseSfc } from 'vue/compiler-sfc'
import ts from 'typescript'

const adapterFile = 'src/components/common/ConfirmationPrompt.vue'
export const customContracts = [
  { file: 'src/views/user/UserOrdersView.vue', tag: 'button', event: 'click', handler: 'confirmRefund', owner: 'refundAction', disabled: 'actionLoading || !refundReason.trim()' },
  { file: 'src/components/user/profile/ProfilePasskeyCard.vue', tag: 'form', event: 'submit', handler: 'confirmDelete', owner: 'deleteAction' },
  { file: 'src/views/admin/AccountQualityView.vue', tag: 'button', event: 'click', handler: 'confirmDelete', owner: 'deleteAction', disabled: 'deleting' },
  { file: 'src/views/admin/AccountQualityView.vue', tag: 'button', event: 'click', handler: 'confirmTemplateDelete', owner: 'templateDeleteAction', disabled: 'templateDeleting' },
]
export const customAdapterContracts = [
  { file: 'src/views/admin/orders/AdminOrdersView.vue', component: 'AdminRefundDialog' },
  { file: 'src/features/prompt-audit/PromptAuditView.vue', component: 'FilterDeleteDialog' },
]
const customBusyProps = { AdminRefundDialog: 'submitting', FilterDeleteDialog: 'deleting' }
const skipped = new Set(['node_modules', 'dist', 'build', '__tests__', '__fixtures__', 'test', 'tests', '.git'])
const extensions = new Set(['.vue', '.ts', '.tsx', '.js', '.jsx'])
const norm = value => value.split(path.sep).join('/')
const unwrap = value => {
  while (value && (ts.isParenthesizedExpression(value) || ts.isAsExpression(value) || ts.isNonNullExpression(value) || ts.isSatisfiesExpression(value))) value = value.expression
  return value
}
const propName = node => node && (ts.isIdentifier(node) || ts.isStringLiteralLike(node)) ? node.text : undefined
const member = node => {
  node = unwrap(node)
  if (node && ts.isPropertyAccessExpression(node)) return { object: unwrap(node.expression), name: node.name.text }
  if (node && ts.isElementAccessExpression(node) && ts.isStringLiteralLike(unwrap(node.argumentExpression))) return { object: unwrap(node.expression), name: unwrap(node.argumentExpression).text }
  return undefined
}
const walk = (node, visit) => { visit(node); ts.forEachChild(node, child => walk(child, visit)) }
const expression = text => unwrap(ts.createSourceFile('expression.ts', 'const value = (' + text + ');', ts.ScriptTarget.Latest, true).statements[0]?.declarationList?.declarations[0]?.initializer)
const directive = (node, kind, name) => node.props.find(prop => prop.type === 7 && prop.name === kind && prop.arg?.isStatic && prop.arg.content === name)
const textOf = node => node?.exp?.content.trim() ?? ''

export async function auditConfirmations({ root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..'), requireAdapter = true, requireCustom = true } = {}) {
  const errors = [], review = [], inventory = [], customMatches = new Map()
  const stats = { files: 0, dialogs: 0, helpers: 0, requests: 0, runs: 0, adapters: 0, customDialogs: 0, inlineActions: 0 }
  const add = (code, file, line, message) => errors.push({ code, file, line, message })
  async function scanDirectory(directory) {
    for (const item of await fs.readdir(directory, { withFileTypes: true }).catch(() => [])) {
      if (item.isDirectory() && !skipped.has(item.name)) await scanDirectory(path.join(directory, item.name))
      else if (item.isFile() && extensions.has(path.extname(item.name)) && !/\.(?:test|spec)\.[^.]+$/.test(item.name)) await scanFile(path.join(directory, item.name))
    }
  }
  async function scanFile(absolute) {
    stats.files++
    const file = norm(path.relative(root, absolute))
    const source = await fs.readFile(absolute, 'utf8')
    const vue = file.endsWith('.vue') ? parseSfc(source, { filename: file }) : null
    if (vue?.errors.length) { add('parse', file, 1, String(vue.errors[0])); return }
    const scripts = vue ? [vue.descriptor.script, vue.descriptor.scriptSetup].filter(Boolean) : [{ content: source, loc: { start: { line: 1 } } }]
    const importedComponents = new Map(), bindings = new Map(), functions = new Map(), helpers = new Map(), factoryNames = new Map(), dialogNames = new Set(['ConfirmDialog', 'confirm-dialog'])
    const asts = scripts.map(script => ({ ast: ts.createSourceFile(file, script.content, ts.ScriptTarget.Latest, true, file.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS), offset: script.loc.start.line - 1 }))
    const lineOf = (ast, node, offset) => offset + ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1
    for (const { ast } of asts) walk(ast, node => {
      if (ts.isImportDeclaration(node) && ts.isStringLiteralLike(node.moduleSpecifier)) {
        const from = node.moduleSpecifier.text
        if (from.endsWith('.vue') && node.importClause?.name) importedComponents.set(node.importClause.name.text, path.basename(from, '.vue'))
        if (/(?:^|\/)useConfirmation$/.test(from)) {
          if (node.importClause?.namedBindings && ts.isNamespaceImport(node.importClause.namedBindings)) add('helper-import', file, 1, 'Import lifecycle helpers by name so ownership stays inspectable')
          for (const item of node.importClause?.namedBindings?.elements ?? []) {
            const name = item.propertyName?.text ?? item.name.text
            if (['useConfirmation', 'useConfirmedAction'].includes(name)) factoryNames.set(item.name.text, name)
          }
        }
        if (/(?:^|\/)ConfirmDialog\.vue$/.test(from) && node.importClause?.name) dialogNames.add(node.importClause.name.text)
      }
      if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
        bindings.set(node.name.text, node.initializer)
        if (ts.isArrowFunction(unwrap(node.initializer)) || ts.isFunctionExpression(unwrap(node.initializer))) functions.set(node.name.text, unwrap(node.initializer))
      }
      if (ts.isFunctionDeclaration(node) && node.name) functions.set(node.name.text, node)
    })
    for (const { ast, offset } of asts) walk(ast, node => {
      if (!ts.isVariableDeclaration(node)) return
      if (node.initializer && ts.isIdentifier(node.initializer) && factoryNames.has(node.initializer.text)) add('helper-alias', file, lineOf(ast, node, offset), 'Use named imports for lifecycle factories instead of rebinding the factory')
      if (ts.isObjectBindingPattern(node.name)) {
        for (const item of node.name.elements) {
          if (!item.dotDotDotToken && ts.isIdentifier(item.name) && !item.initializer) bindings.set(item.name.text, ts.factory.createPropertyAccessExpression(node.initializer ?? ts.factory.createIdentifier('undefined'), propName(item.propertyName) ?? item.name.text))
        }
      }
    })
    const resolve = (input, seen = new Set()) => {
      const node = unwrap(input)
      if (node && ts.isIdentifier(node) && bindings.has(node.text) && !seen.has(node.text)) return resolve(bindings.get(node.text), new Set([...seen, node.text]))
      if (node && ts.isIdentifier(node) && functions.has(node.text)) return functions.get(node.text)
      return node
    }
    for (const { ast, offset } of asts) walk(ast, node => {
      if (!ts.isVariableDeclaration(node) || !ts.isIdentifier(node.name)) return
      const call = unwrap(node.initializer)
      if (!call || !ts.isCallExpression(call) || !ts.isIdentifier(call.expression) || !factoryNames.has(call.expression.text)) return
      const kind = factoryNames.get(call.expression.text)
      helpers.set(node.name.text, { kind, context: call.arguments.length > 0, line: lineOf(ast, node, offset) })
      stats.helpers++
    })
    function helperIdentity(input, seen = new Set()) {
      const node = unwrap(input)
      if (!node || !ts.isIdentifier(node) || seen.has(node.text)) return undefined
      if (helpers.has(node.text)) return node.text
      return helperIdentity(bindings.get(node.text), new Set([...seen, node.text]))
    }
    function helperMethod(input) {
      const item = member(resolve(input))
      const identity = item && helperIdentity(item.object)
      return identity ? { helper: identity, method: item.name, ...helpers.get(identity) } : undefined
    }
    for (const { ast, offset } of asts) walk(ast, node => {
      if (!ts.isCallExpression(node)) return
      const line = lineOf(ast, node, offset)
      if (ts.isIdentifier(node.expression) && factoryNames.has(node.expression.text) && !(ts.isVariableDeclaration(node.parent) && ts.isIdentifier(node.parent.name))) add('helper-owner', file, line, 'Assign the lifecycle helper to a named owner before using its methods')
      const target = resolve(node.expression)
      const native = member(target)
      if ((ts.isIdentifier(target) && target.text === 'confirm' && !functions.has('confirm') && !bindings.has('confirm')) || (native?.name === 'confirm' && ts.isIdentifier(native.object) && ['window', 'globalThis', 'self'].includes(native.object.text))) add('native-confirm', file, line, 'Use the shared confirmation lifecycle instead of the browser confirm dialog')
      const method = helperMethod(node.expression)
      if (!method || !['request', 'run'].includes(method.method)) return
      inventory.push({ file, line, helper: method.helper, kind: method.kind, method: method.method, context: method.context })
      if (method.method === 'request') stats.requests++
      else stats.runs++
      if (method.kind === 'useConfirmedAction') {
        if (method.method !== 'run') add('action-method', file, line, 'A direct-dialog action must execute through run')
        if (!resolve(node.arguments[0]) || ![ts.SyntaxKind.ArrowFunction, ts.SyntaxKind.FunctionExpression, ts.SyntaxKind.FunctionDeclaration].includes(resolve(node.arguments[0]).kind)) add('action-callback', file, line, 'Action run requires an inspectable callback')
        return
      }
      const options = resolve(node.arguments[0])
      if (!options || !ts.isObjectLiteralExpression(options) || options.properties.some(ts.isSpreadAssignment)) { add('confirmation-options', file, line, 'Use an explicit options object with danger: true or danger: false'); return }
      const value = name => { const item = options.properties.find(item => propName(item.name) === name); return item && ts.isPropertyAssignment(item) ? resolve(item.initializer) : undefined }
      const danger = value('danger')
      if (!danger || ![ts.SyntaxKind.TrueKeyword, ts.SyntaxKind.FalseKeyword].includes(danger.kind)) add('explicit-intent', file, line, 'Declare danger explicitly as true or false; do not infer intent from copy')
      if (danger?.kind === ts.SyntaxKind.TrueKeyword) {
        if (method.method === 'request') add('destructive-request', file, line, 'Destructive mutations must run inside confirmation.run so the prompt owns the lock')
        for (const name of ['title', 'confirmText']) if (!value(name)) add('destructive-copy', file, line, 'Destructive confirmation requires explicit ' + name)
        if (!method.context) review.push({ code: 'implicit-context', file, line, message: 'Destructive confirmation relies on injected session/modal context; verify target ownership' })
      }
      if (method.method === 'run' && (!resolve(node.arguments[1]) || ![ts.SyntaxKind.ArrowFunction, ts.SyntaxKind.FunctionExpression, ts.SyntaxKind.FunctionDeclaration].includes(resolve(node.arguments[1]).kind))) add('action-callback', file, line, 'Confirmation run requires an action callback')
    })
    function inspectTemplate(node) {
      if (node.type === 1 && requireCustom) {
        for (const contract of customContracts.filter(item => item.file === file && item.tag === node.tag && textOf(directive(node, 'on', item.event)) === item.handler)) {
          customMatches.set(contract, (customMatches.get(contract) ?? 0) + 1)
          stats.inlineActions++
          const fn = functions.get(contract.handler)
          let owned = false
          if (fn) walk(fn, child => { if (ts.isCallExpression(child)) { const method = helperMethod(child.expression); if (method?.helper === contract.owner && method.method === 'run') owned = true } })
          if (!owned) add('custom-action', file, node.loc.start.line, contract.handler + ' must execute through ' + contract.owner + '.run')
          if (contract.disabled && textOf(directive(node, 'bind', 'disabled')) !== contract.disabled) add('custom-lock', file, node.loc.start.line, contract.handler + ' must retain its reviewed disabled binding: ' + contract.disabled)
          inventory.push({ file, line: node.loc.start.line, kind: 'inline-action', handler: contract.handler, owner: contract.owner })
        }
      }
      const customConfirm = node.type === 1 && !dialogNames.has(node.tag) && directive(node, 'on', 'confirm') && (/^[A-Z]/.test(node.tag) || node.tag.includes('-'))
      if (node.type === 1 && (dialogNames.has(node.tag) || customConfirm)) {
        if (customConfirm) {
          stats.customDialogs++
          for (const contract of customAdapterContracts.filter(item => item.file === file && item.component === (importedComponents.get(node.tag) ?? node.tag))) customMatches.set(contract, (customMatches.get(contract) ?? 0) + 1)
        }
        else stats.dialogs++
        const line = node.loc.start.line
        const busyProp = customConfirm ? customBusyProps[importedComponents.get(node.tag) ?? node.tag] ?? 'busy' : 'busy'
        const busy = textOf(directive(node, 'bind', busyProp))
        const handler = textOf(directive(node, 'on', 'confirm'))
        if (file === adapterFile) {
          stats.adapters++
          const expected = { busy: 'confirmation.busy.value', show: 'confirmation.show.value', danger: 'confirmation.danger.value' }
          for (const [name, text] of Object.entries(expected)) if (textOf(directive(node, 'bind', name)) !== text) add('adapter-contract', file, line, 'The prompt adapter must bind ' + name + ' to ' + text)
          if (handler !== 'confirmation.settle(true)' || textOf(directive(node, 'on', 'cancel')) !== 'confirmation.settle(false)') add('adapter-contract', file, line, 'Only the shared prompt adapter may forward settle(true/false)')
        } else {
          const busyOwners = input => {
            const value = resolve(input)
            // OR may add an independent lock, but AND/ternaries can release the
            // lifecycle lock and are deliberately not considered ownership.
            if (value && ts.isBinaryExpression(value) && value.operatorToken.kind === ts.SyntaxKind.BarBarToken) return [...busyOwners(value.left), ...busyOwners(value.right)]
            let item = member(value)
            if (item?.name === 'value') item = member(item.object)
            const identity = item?.name === 'busy' && helperIdentity(item.object)
            return identity && helpers.get(identity)?.kind === 'useConfirmedAction' ? [identity] : []
          }
          const owners = busyOwners(expression(busy))
          if (!owners.length) add('dialog-busy', file, line, 'Bind :busy to the useConfirmedAction instance that owns this confirmation')
          const fn = functions.get(handler)
          if (!fn) add('dialog-handler', file, line, 'Use a named confirm handler executed through useConfirmedAction.run')
          else {
            let owned = false
            walk(fn, child => { if (ts.isCallExpression(child)) { const method = helperMethod(child.expression); if (owners.includes(method?.helper) && method?.method === 'run') owned = true } })
            if (!owned) add('dialog-action', file, line, 'The named handler must call run on the same action that supplies :busy')
          }
        }
        inventory.push({ file, line, kind: 'dialog', busy, handler })
      }
      for (const child of node.children ?? []) inspectTemplate(child)
    }
    if (vue?.descriptor.template?.ast) inspectTemplate(vue.descriptor.template.ast)
  }
  await scanDirectory(path.join(root, 'src'))
  if (requireCustom) for (const contract of customAdapterContracts) if (customMatches.get(contract) !== 1) add('custom-count', contract.file, 1, 'Expected exactly one reviewed ' + contract.component + ' adapter; found ' + (customMatches.get(contract) ?? 0))
  if (requireCustom) for (const contract of customContracts) if (customMatches.get(contract) !== 1) add('custom-count', contract.file, 1, 'Expected one reviewed ' + contract.tag + ' @' + contract.event + '=' + contract.handler + '; found ' + (customMatches.get(contract) ?? 0))
  if (requireAdapter && stats.adapters !== 1) add('adapter-count', adapterFile, 1, 'Expected exactly one verified prompt adapter; found ' + stats.adapters)
  return { ok: errors.length === 0, stats, errors, review, inventory }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const at = process.argv.indexOf('--root')
  const result = await auditConfirmations({ root: at >= 0 ? process.argv[at + 1] : undefined })
  if (process.argv.includes('--json')) console.log(JSON.stringify(result, null, 2))
  else {
    console.log('Confirmation architecture: ' + (result.ok ? 'PASS' : 'FAIL') + '; ' + result.stats.dialogs + ' dialogs, ' + result.stats.customDialogs + ' custom adapters, ' + result.stats.inlineActions + ' inline actions, ' + result.stats.helpers + ' helpers, ' + result.stats.requests + ' warnings, ' + result.stats.runs + ' action runs')
    for (const error of result.errors) console.error(error.file + ':' + error.line + ' [' + error.code + '] ' + error.message)
    for (const item of result.review) console.log(item.file + ':' + item.line + ' [review:' + item.code + '] ' + item.message)
  }
  if (!result.ok) process.exitCode = 1
}
