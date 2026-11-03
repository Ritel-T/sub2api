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


test('filter roles on controls, wrapping labels, ancestors and linked labels accept aria names', () => {
  for (const source of [
    '<input data-field-role="filter" aria-label="Search"/>',
    '<label data-field-role="filter"><input aria-label="Search"/></label>',
    '<section data-field-role="filter"><div><Select :aria-label="name"/></div></section>',
    '<label data-field-role="filter" for="search"></label><input id="search" aria-label="Search"/>',
    `<input :data-field-role="'filter'" aria-label="Search"/>`,
    '<textarea data-field-role="filter" aria-label="Notes"/>',
    '<ProxySelector data-field-role="filter" aria-label="Proxy"/>',
    '<Input data-field-role="filter" label="Search"/>',
    '<Select data-field-role="filter" :ariaLabel="name"/>'
  ]) {
    const field = inspect(source).fields.at(-1)
    assert.equal(field.ok, true, source)
    assert.equal(field.category, 'filter-accessible-name', source)
  }
})

test('filter labels can provide screen reader text without a visible caption', () => {
  for (const source of [
    '<label data-field-role="filter"><span class="sr-only">Search</span><input placeholder="Search"/></label>',
    '<label data-field-role="filter" class="sr-only" for="x">Search</label><input id="x"/>',
    '<div data-field-role="filter"><label><span class="sr-only">Status</span><Select/></label></div>',
    '<span id="name" class="sr-only">Status</span><Select data-field-role="filter" aria-labelledby="name"/>',
    '<span :id="`name-${index}`" class="sr-only">Status</span><Select data-field-role="filter" :aria-labelledby="`name-${index}`"/>'
  ]) assert.equal(inspect(source).fields.at(-1).ok, true, source)
})

test('filter role and placeholder alone never supply an accessible name', () => {
  for (const source of [
    '<input data-field-role="filter" placeholder="Search"/>',
    '<label data-field-role="filter"><input placeholder="Search"/></label>',
    '<div data-field-role="filter"><Select placeholder="Status"/></div>',
    '<label data-field-role="filter"><span class="sr-only"> </span><input placeholder="Search"/></label>',
    '<label data-field-role="filter" for="x"></label><input id="x" placeholder="Search"/>'
  ]) {
    const field = inspect(source).fields.at(-1)
    assert.equal(field.ok, false, source)
    assert.equal(field.category, 'missing-filter-name', source)
  }
})

test('filter names reject empty aria labels and conditional empty branches', () => {
  for (const name of ['aria-label=""', 'aria-label="  "', `:aria-label="'  '"`, ':aria-label="enabled ? name : undefined"', `:aria-label="enabled ? name : ''"`]) {
    const source = `<input data-field-role="filter" ${name} placeholder="Search"/>`
    assert.equal(inspect(source).fields[0].ok, false, source)
  }
  assert.equal(inspect('<input data-field-role="filter" :aria-label="enabled ? name : fallback"/>').fields[0].ok, true)
})

test('filter names require an actual label association and accessible text', () => {
  for (const source of [
    '<input data-field-role="filter" aria-labelledby="missing"/>',
    '<span id="name"> </span><input data-field-role="filter" aria-labelledby="name"/>',
    '<label data-field-role="filter"><span aria-hidden="true">Search</span><input/></label>',
    '<label data-field-role="filter"><span class="hidden">Search</span><input/></label>',
    '<label data-field-role="filter"><span class="sr-only hidden">Search</span><input/></label>',
    '<label data-field-role="filter"><span style="display:none">Search</span><input/></label>',
    '<div data-field-role="filter" aria-label="Search"><input placeholder="Search"/></div>',
    '<label data-field-role="filter" for="other">Search</label><input id="x"/>',
    '<label><span class="sr-only">Search</span><input :data-field-role="role"/></label>'
  ]) assert.equal(inspect(source).fields.at(-1).ok, false, source)
})

test('ordinary edit forms still require visible captions, including inside wrapping labels', () => {
  for (const source of [
    '<label><span class="sr-only">Email</span><input placeholder="Email"/></label>',
    '<label><span class="sr-only">Email</span><input aria-label="Email"/></label>',
    '<label><span class="hidden">Email</span><input/></label>',
    '<span id="name" class="sr-only">Email</span><input aria-labelledby="name"/>',
    '<input data-field-role="form" aria-label="Email"/>'
  ]) assert.equal(inspect(source).fields.at(-1).ok, false, source)
  assert.equal(inspect('<label>Email<span class="sr-only"> required</span><input/></label>').fields[0].ok, true)
})

test('SearchInput callers use its intrinsic localized accessible name without a visible label', () => {
  for (const source of ['<SearchInput/>', '<SearchInput placeholder="Search accounts"/>', '<SearchInput :label="name"/>']) {
    const field = inspect(source).fields[0]
    assert.equal(field.ok, true, source)
    assert.equal(field.category, 'filter-accessible-name', source)
  }
})

test('SearchInput implementation must still supply a nonempty accessible name', () => {
  const inspectSearch = template => inspectFieldTemplate(`<template>${template}</template>`, 'src/components/common/SearchInput.vue').fields[0]
  assert.equal(inspectSearch('<input :aria-label="labelText" placeholder="Search"/>').ok, true)
  assert.equal(inspectSearch('<input placeholder="Search"/>').ok, false)
  assert.equal(inspectSearch('<input aria-label=" "/>').ok, false)
  assert.equal(inspect('<input :aria-label="labelText" placeholder="Search"/>').fields[0].ok, false)
})


test('explicit aria-labelledby references can use hidden text while forms still require visibility', () => {
  const label = '<span id="name" hidden>Search</span>'
  assert.equal(inspect(`${label}<input data-field-role="filter" aria-labelledby="name"/>`).fields[0].ok, true)
  assert.equal(inspect(`${label}<input aria-labelledby="name"/>`).fields[0].ok, false)
})
