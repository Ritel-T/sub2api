import { test } from 'node:test'
import assert from 'node:assert/strict'
import { auditControlTemplate } from './check-controls.mjs'

const check = (template, options) => auditControlTemplate(`<template>${template}</template>`, options)
test('accepts translated and dynamic switch names', () => {
  assert.deepEqual(check(`<Toggle :aria-label="t('setting.enabled')"/><Toggle :aria-labelledby="fieldId"/><ToggleSwitch :label="row.name"/>`).errors, [])
})
test('rejects unnamed and empty switches', () => {
  for (const attributes of ['', 'aria-label=""', ':aria-label="\'\'"', ':aria-labelledby="undefined"']) assert.equal(check(`<Toggle ${attributes}/>`).errors[0].code, 'control-name')
})
test('a label prop only names the payment wrapper that implements it', () => {
  assert.equal(check('<Toggle label="Setting"/>').errors.length, 1)
  assert.equal(check('<Select label="Setting"/>').errors.length, 1)
  assert.equal(check('<Toggle ariaLabel="Setting"/>').errors.length, 1)
  assert.equal(check('<ToggleSwitch aria-label="Setting"/>').errors.length, 1)
  assert.equal(check('<ToggleSwitch label="Setting"/>').errors.length, 0)
})
test('does not infer a switch name from neighboring text or an enclosing label', () => {
  assert.equal(check('<label>Setting<Toggle/></label>').errors.length, 1)
})
test('accepts Select names in Vue prop and ARIA spellings', () => {
  assert.deepEqual(check(`<Select aria-label="Model"/><Select :ariaLabel="modelLabel"/><Select aria-labelledby="model-label"/>`).errors, [])
})
test('accepts Select labels only when for and id match', () => {
  assert.equal(check('<label for="model">Model</label><Select id="model"/>').errors.length, 0)
  assert.equal(check('<label for="other">Model</label><Select id="model"/>').errors.length, 1)
})
test('compares dynamic for/id expressions without treating them as literals', () => {
  assert.equal(check('<label :for="fieldId">Model</label><Select :id="fieldId"/>').errors.length, 0)
  assert.equal(check('<label for="fieldId">Model</label><Select :id="fieldId"/>').errors.length, 1)
})
test('empty labels cannot name a Select', () => {
  assert.equal(check('<label for="model"> </label><Select id="model"/>').errors.length, 1)
})
test('attribute spreading alone cannot bypass the name contract', () => {
  assert.equal(check('<Toggle v-bind="attrs"/>').errors.length, 1)
})
test('rejects hand-rendered switches including bound literal roles', () => {
  assert.equal(check('<button role="switch" aria-label="Enabled"/>').errors[0].code, 'shared-switch')
  assert.equal(check(`<input :role="'switch'"/>`).errors[0].code, 'shared-switch')
})
test('keeps the one shared switch renderer legal', () => {
  assert.deepEqual(check('<button role="switch"/>', { file: 'src/components/common/Toggle.vue' }).errors, [])
})
test('checks imported aliases and leaves ordinary selection checkboxes alone', () => {
  const result = auditControlTemplate(`<script setup>import EnabledControl from '@/components/common/Toggle.vue'; import ModelPicker from '@/components/common/Select.vue'</script><template><EnabledControl/><enabled-control/><model-picker/><input type="checkbox"/><select/></template>`)
  assert.equal(result.errors.length, 3)
  assert.ok(result.errors.every(error => error.code === 'control-name'))
})
test('checks Vue kebab and camel spellings without mistaking native select', () => {
  assert.equal(check('<toggle/><toggle-switch/><toggleSwitch/><select/>').errors.length, 3)
  assert.equal(check('<toggle aria-label="Setting"/><toggle-switch label="Setting"/>').errors.length, 0)
})
