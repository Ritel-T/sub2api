import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { auditConfirmations, customContracts, customAdapterContracts } from './check-confirmations.mjs'

const adapter = '<template><ConfirmDialog :show="confirmation.show.value" :busy="confirmation.busy.value" :danger="confirmation.danger.value" @confirm="confirmation.settle(true)" @cancel="confirmation.settle(false)" /></template>'
const imports = "import { useConfirmation, useConfirmedAction } from '@/composables/useConfirmation';"
const page = (script, template = '<div/>') => '<template>' + template + '</template><script setup lang="ts">' + imports + script + '</script>'
async function fixture(t, files, options = {}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'ritel-confirm-guard-'))
  t.after(() => fs.rm(root, { recursive: true, force: true }))
  for (const [file, source] of Object.entries({ 'src/components/common/ConfirmationPrompt.vue': adapter, ...files })) {
    await fs.mkdir(path.dirname(path.join(root, file)), { recursive: true })
    await fs.writeFile(path.join(root, file), source)
  }
  return auditConfirmations({ root, requireCustom: false, ...options })
}
const errorCodes = result => result.errors.map(item => item.code)

test('accepts lifecycle-owned mutation and explicit ordinary warning', async t => {
  const result = await fixture(t, { 'src/Page.vue': page("const confirmation = useConfirmation(() => ({ active: true, identity: target.value?.id })); const requestConfirmation = confirmation.request; await requestConfirmation({ message: t('warning'), danger: false }); await confirmation.run({ message: t('delete'), title: t('delete'), confirmText: t('delete'), danger: true }, async isCurrent => { await remove(target); if (isCurrent()) done() });") })
  assert.deepEqual(result.errors, [])
  assert.deepEqual(result.review, [])
  assert.equal(result.stats.requests, 1)
  assert.equal(result.stats.runs, 1)
})

for (const [name, script, expected] of [
  ['string request', "await confirmation.request('delete?')", 'confirmation-options'],
  ['aliased string request', "const request = confirmation.request; await request('delete?')", 'confirmation-options'],
  ['missing intent', "await confirmation.request({message:'warning'})", 'explicit-intent'],
  ['dynamic intent', "await confirmation.request({message:'warning',danger:computedDanger})", 'explicit-intent'],
  ['destructive request', "await confirmation.request({message:'delete', title:'Delete',confirmText:'Delete',danger:true})", 'destructive-request'],
  ['missing destructive copy', "await confirmation.run({message:'delete',danger:true}, async()=>{})", 'destructive-copy'],
  ['opaque options', 'await confirmation.request(getOptions())', 'confirmation-options'],
  ['spread options', 'await confirmation.request({...options,danger:false})', 'confirmation-options'],
  ['missing callback', "await confirmation.run({message:'ok',danger:false})", 'action-callback'],
  ['nonfunction callback', "await confirmation.run({message:'ok',danger:false},true)", 'action-callback'],
]) test('rejects ' + name, async t => {
  const result = await fixture(t, { 'src/Page.vue': page('const confirmation=useConfirmation();' + script) })
  assert(errorCodes(result).includes(expected), JSON.stringify(result.errors))
})

test('resolves constant options and named callbacks; ignores business request methods', async t => {
  const result = await fixture(t, { 'src/Page.vue': page("const confirmation = useConfirmation(); const options = { message:'Continue?', danger:false }; function execute() {} await confirmation['run'](options, execute); api.request('not a dialog');") })
  assert.deepEqual(result.errors, [])
})

test('supports imported helper alias, renamed dialog and busy alias', async t => {
  const result = await fixture(t, { 'src/Page.vue': '<template><DeletePrompt :busy="pending" @confirm="remove" /></template><script setup>' +
    "import DeletePrompt from '@/components/common/ConfirmDialog.vue'; import { useConfirmedAction as actionFor } from '@/composables/useConfirmation'; const action = actionFor(() => ({active:true})); const pending = action.busy; async function remove() { await action.run(async () => {}) }</script>" })
  assert.deepEqual(result.errors, [])
  assert.equal(result.stats.dialogs, 2)
})

for (const [name, script, template, expected] of [
  ['missing busy', 'const action=useConfirmedAction();const remove=()=>action.run(async()=>{});', '<ConfirmDialog @confirm="remove"/>', 'dialog-busy'],
  ['manual busy', 'const action=useConfirmedAction();const pending=ref(false);const remove=()=>action.run(async()=>{});', '<ConfirmDialog :busy="pending" @confirm="remove"/>', 'dialog-busy'],
  ['wrong owner', 'const action=useConfirmedAction();const other=useConfirmedAction();const remove=()=>other.run(async()=>{});', '<ConfirmDialog :busy="action.busy.value" @confirm="remove"/>', 'dialog-action'],
  ['unwrapped handler', 'const action=useConfirmedAction();const remove=()=>api.remove();', '<ConfirmDialog :busy="action.busy.value" @confirm="remove"/>', 'dialog-action'],
  ['inline handler', 'const action=useConfirmedAction();', '<ConfirmDialog :busy="action.busy.value" @confirm="api.remove()"/>', 'dialog-handler'],
]) test('rejects direct dialog ' + name, async t => {
  const result = await fixture(t, { 'src/Page.vue': page(script, template) })
  assert(errorCodes(result).includes(expected), JSON.stringify(result.errors))
})

for (const call of ['confirm("Delete?")', 'window.confirm("Delete?")', 'globalThis["confirm"]("Delete?")', 'const ask = window.confirm; ask("Delete?")']) {
  test('rejects browser confirmation: ' + call, async t => {
    const result = await fixture(t, { 'src/Page.vue': page(call) })
    assert(errorCodes(result).includes('native-confirm'))
  })
}

test('ordinary object confirm method is not a browser dialog', async t => {
  const result = await fixture(t, { 'src/Page.vue': page('client.confirm("business method");function confirm(){};confirm()') })
  assert.deepEqual(result.errors, [])
})

test('implicit destructive context is surfaced for review', async t => {
  const result = await fixture(t, { 'src/Page.vue': page("const confirmation=useConfirmation();await confirmation.run({message:'Delete?',title:'Delete',confirmText:'Delete',danger:true},async()=>{});") })
  assert.deepEqual(result.errors, [])
  assert.equal(result.review[0].code, 'implicit-context')
})

test('exact adapter invariant rejects a removed adapter', async t => {
  const result = await fixture(t, { 'src/components/common/ConfirmationPrompt.vue': '<template><div/></template>' })
  assert(errorCodes(result).includes('adapter-count'))
})

test('adapter forwarding cannot become a general bypass', async t => {
  const result = await fixture(t, { 'src/Page.vue': adapter })
  assert(errorCodes(result).includes('dialog-busy'))
  assert(errorCodes(result).includes('dialog-handler'))
})

test('adapter still checks busy and settle ownership', async t => {
  const result = await fixture(t, { 'src/components/common/ConfirmationPrompt.vue': adapter.replace('confirmation.busy.value', 'false').replace('settle(true)', 'settle(false)') })
  assert(errorCodes(result).includes('adapter-contract'))
})

test('second adapter is rejected as stale architecture', async t => {
  const result = await fixture(t, { 'src/components/common/ConfirmationPrompt.vue': adapter.replace('</template>', '<ConfirmDialog/></template>') })
  assert(errorCodes(result).includes('adapter-count'))
})

test('runtime inventory excludes tests and fixtures', async t => {
  const result = await fixture(t, { 'src/__tests__/bad.vue': page('confirm()'), 'src/a.spec.ts': 'confirm()', 'src/__fixtures__/bad.ts': 'confirm()' })
  assert.deepEqual(result.errors, [])
  assert.equal(result.stats.files, 1)
})

test('helper instance aliases and destructured methods retain inspection', async t => {
  const result = await fixture(t, { 'src/Page.vue': page("const confirmation=useConfirmation(); const alias=confirmation; const {request:ask}=alias; ask('Delete?')") })
  assert(errorCodes(result).includes('confirmation-options'))
})
test('unowned destructured factory cannot bypass inspection', async t => {
  const result = await fixture(t, { 'src/Page.vue': page("const {request}=useConfirmation(); request('Delete?')") })
  assert(errorCodes(result).includes('helper-owner'))
})
test('factory rebinding cannot bypass inspection', async t => {
  const result = await fixture(t, { 'src/Page.vue': page('const make=useConfirmation; const confirmation=make();') })
  assert(errorCodes(result).includes('helper-alias'))
})
test('namespace factory import cannot bypass inspection', async t => {
  const result = await fixture(t, { 'src/Page.vue': '<script setup>import * as prompts from "@/composables/useConfirmation"; const confirmation=prompts.useConfirmation();</script><template><div/></template>' })
  assert(errorCodes(result).includes('helper-import'))
})

test('an OR lock preserves lifecycle busy even with a separate business lock', async t => {
  const result = await fixture(t, { 'src/Page.vue': page('const action=useConfirmedAction();const remove=()=>action.run(async()=>{});', '<ConfirmDialog :busy="action.busy.value || businessBusy" @confirm="remove"/>') })
  assert.deepEqual(result.errors, [])
})
for (const busy of ['action.busy.value && businessBusy', 'businessBusy ? action.busy.value : false', 'other.busy.value || businessBusy']) {
  test('rejects a busy expression that may release the owning lock: ' + busy, async t => {
    const result = await fixture(t, { 'src/Page.vue': page('const action=useConfirmedAction();const other=useConfirmedAction();const remove=()=>action.run(async()=>{});', '<ConfirmDialog :busy="' + busy + '" @confirm="remove"/>') })
    assert(result.errors.some(item => ['dialog-busy', 'dialog-action'].includes(item.code)))
  })
}

test('new custom confirm components cannot escape handler and busy ownership', async t => {
  const result = await fixture(t, { 'src/Page.vue': page('const remove=()=>api.remove();', '<NewDeleteDialog @confirm="remove"/>') })
  assert(errorCodes(result).includes('dialog-busy'))
  assert(errorCodes(result).includes('dialog-action'))
})
for (const [component, prop] of [['AdminRefundDialog', 'submitting'], ['FilterDeleteDialog', 'deleting'], ['FutureDialog', 'busy']]) {
  test('custom adapter retains the owning lock: ' + component, async t => {
    const result = await fixture(t, { 'src/Page.vue': page('const action=useConfirmedAction();const remove=()=>action.run(async()=>{});', '<' + component + ' :' + prop + '="action.busy.value" @confirm="remove"/>') })
    assert.deepEqual(result.errors, [])
    assert.equal(result.stats.customDialogs, 1)
  })
}
function customFiles() {
  const files = {}
  for (const file of new Set(customContracts.map(item => item.file))) {
    const contracts = customContracts.filter(item => item.file === file)
    const owners = [...new Set(contracts.map(item => item.owner))]
    const handlers = [...new Set(contracts.map(item => item.handler))]
    const script = owners.map(name => 'const ' + name + '=useConfirmedAction();').join('') +
      handlers.map(name => 'const ' + name + '=()=>'+contracts.find(item => item.handler === name).owner+'.run(async()=>{});').join('')
    const template = contracts.map(item => '<'+item.tag+' @'+item.event+'="'+item.handler+'"'+(item.disabled?' :disabled="'+item.disabled+'"':'')+'/>').join('')
    files[file] = page(script, template)
  }
  for (const contract of customAdapterContracts) files[contract.file] = page('const action=useConfirmedAction();const remove=()=>action.run(async()=>{});', '<'+contract.component+' :'+(contract.component === 'AdminRefundDialog' ? 'submitting' : 'deleting')+'="action.busy.value" @confirm="remove"/>')
  return files
}
test('precise inline action contracts validate reviewed handlers and locks', async t => {
  const result = await fixture(t, customFiles(), { requireCustom: true })
  assert.deepEqual(result.errors, [])
  assert.equal(result.stats.inlineActions, 4)
})
test('removed inline confirmation contract fails rather than silently becoming stale', async t => {
  const files = customFiles()
  files[customContracts[0].file] = '<template><div/></template>'
  const result = await fixture(t, files, { requireCustom: true })
  assert(errorCodes(result).includes('custom-count'))
})
test('inline mutation cannot lose its reviewed busy lock', async t => {
  const files = customFiles()
  const contract = customContracts[0]
  files[contract.file] = files[contract.file].replace(contract.disabled, 'false')
  const result = await fixture(t, files, { requireCustom: true })
  assert(errorCodes(result).includes('custom-lock'))
})
test('inline mutation cannot abandon lifecycle action', async t => {
  const files = customFiles()
  const contract = customContracts[0]
  files[contract.file] = files[contract.file].replace(contract.owner+'.run(async()=>{})', 'api.remove()')
  const result = await fixture(t, files, { requireCustom: true })
  assert(errorCodes(result).includes('custom-action'))
})
test('custom adapter removal fails the exact reviewed inventory', async t => {
  const files = customFiles()
  files[customAdapterContracts[0].file] = '<template><div/></template>'
  const result = await fixture(t, files, { requireCustom: true })
  assert(errorCodes(result).includes('custom-count'))
})
