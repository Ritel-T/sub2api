import { test } from 'node:test'
import assert from 'node:assert/strict'
import { inspectFieldTemplate } from './check-field-labels.mjs'
const inspect = template => inspectFieldTemplate(`<template>${template}</template>`)
test('placeholder and aria-label alone do not count as visible captions', () => {
  for (const source of ['<input placeholder="Email"/>', '<textarea aria-label="Notes"/>', '<Select :aria-label="name"/>', '<label class="sr-only" for="x">Email</label><input id="x"/>']) assert.equal(inspect(source).fields.at(-1).ok, false, source)
})
test('linked, wrapped, dynamic repeated and responsive visible captions pass', () => {
  for (const source of ['<label for="x">Email</label><input id="x"/>', '<label>Email<input/></label>', '<span :id="`name-${index}`">Name</span><input :aria-labelledby="`name-${index}`"/>', '<div class="hidden sm:flex"><span id="p">Page</span><input aria-labelledby="p"/></div>', '<Input :label="name"/>']) assert.equal(inspect(source).fields.at(-1).ok, true, source)
})
test('hidden uploads and noninteractive OTP helpers are classified separately', () => {
  assert.equal(inspect('<input type="file" class="hidden"/>').fields[0].category, 'hidden-upload-or-value')
  assert.equal(inspect('<input aria-hidden="true" tabindex="-1"/>').fields[0].category, 'hidden-upload-or-value')
})
test('icon-only buttons need readable names; switch names remain the control gate responsibility', () => {
  assert.equal(inspect('<button><svg/></button>').buttons.length, 1)
  assert.equal(inspect('<button :aria-label="name"><svg/></button>').buttons.length, 0)
})

test('rejects icon names that disappear in a conditional branch', () => {
  assert.equal(inspect('<button :aria-label="isGrok ? name : undefined"><svg/></button>').buttons.length, 1)
  assert.equal(inspect('<button :aria-label="isGrok ? name : fallback"><svg/></button>').buttons.length, 0)
})


test('audits ProxySelector callers rather than just its dropdown search input', () => {
  assert.equal(inspect('<label>Proxy</label><ProxySelector/>').fields.at(-1)?.ok, false)
  assert.equal(inspect('<label id="proxy">Proxy</label><ProxySelector aria-labelledby="proxy"/>').fields.at(-1)?.ok, true)
})
