import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { auditTypography, headingRoles, textScales, isTypographyUtility } from './check-typography.mjs'

const emptyExceptions = { version: 1, rawHeadings: [], contentSelectors: [] }
const dimensions = ['family', 'size', 'weight', 'leading', 'tracking']
const tokens = headingRoles.flatMap(role => dimensions.map(dimension => `--ritel-type-${role}-${dimension}: ${dimension === 'family' ? 'var(--ritel-ui)' : dimension === 'size' ? '16px' : dimension === 'weight' ? '600' : dimension === 'leading' ? '1.4' : '0'};`)).join('\n')
const scaleTokens = textScales.map(scale => `--ritel-text-${scale}: 16px;`).join('\n')
const typography = `:root { ${tokens} ${scaleTokens} --ritel-leading-body: 1.5; }
.ritel-type[data-type-role] { font-family: var(--type-family); font-size: var(--type-size); font-weight: var(--type-weight); line-height: var(--type-leading); letter-spacing: var(--type-tracking); }
${headingRoles.map(role => `.ritel-type[data-type-role="${role}"] { ${dimensions.map(dimension => `--type-${dimension}: var(--ritel-type-${role}-${dimension});`).join(' ')} }`).join('\n')}`
const scaleMap = textScales.map(scale => `'${scale}': ['var(--ritel-text-${scale})', { lineHeight: 'var(--ritel-leading-body)' }],`).join('\n')
const tailwind = `export default { theme: { extend: { fontFamily: { sans: ['var(--ritel-ui)'], serif: ['var(--ritel-display)'], display: ['var(--ritel-display)'], heading: ['var(--ritel-heading)'], mono: ['var(--ritel-mono)'] }, fontSize: { ${scaleMap} } } } }`

async function fixture(t, files, options = {}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'ritel-type-guard-'))
  t.after(() => fs.rm(root, { recursive: true, force: true }))
  const entries = {
    'tailwind.config.js': tailwind,
    'public/brand/brand.css': '@import "./typography.css"; :root { --ritel-ui: "UI", sans-serif; --ritel-display: "Display", serif; --ritel-heading: "Heading", serif; --ritel-mono: "Mono", monospace; }',
    'public/brand/typography.css': typography,
    ...files,
  }
  for (const [file, content] of Object.entries(entries)) {
    await fs.mkdir(path.dirname(path.join(root, file)), { recursive: true })
    await fs.writeFile(path.join(root, file), content)
  }
  return auditTypography({ root, exceptions: options.exceptions ?? emptyExceptions })
}

test('allows semantic roles, layout/color classes and static conditional presentation', async t => {
  const result = await fixture(t, { 'src/Page.vue': `<template><AppHeading variant="page" as="h1" class="mb-3 text-red-500" :class="[{ 'text-green-600': ok }, extra]" :style="{ color: activeColor, marginTop: '2rem' }">Page</AppHeading></template><script setup>const extra = 'flex gap-2'</script><style scoped>.body-copy {font-size:12px;line-height:1.5}</style>` })
  assert.deepEqual(result.errors, [])
  assert.equal(result.stats.authoredHeadings, 1)
  assert.equal(result.review.length, 2)
})

for (const utility of ['font-display', 'font-semibold', 'md:text-2xl', 'text-2xs', 'text-3xl/8', '!leading-tight', 'dark:tracking-wide', '[font-family:Arial]', '[--type-size:90px]', 'text-[30px]', 'text-[length:var(--foo)]', 'text-[var(--foo)]', 'uppercase', 'sm:capitalize', '[text-transform:lowercase]']) {
  test(`rejects heading utility ${utility}`, async t => {
    const result = await fixture(t, { 'src/Page.vue': `<template><AppHeading variant="page" class="${utility}">Page</AppHeading></template>` })
    assert(result.errors.some(error => error.code === 'heading-utility'), JSON.stringify(result.errors))
  })
}

for (const utility of ['text-red-500', 'text-[#c00]', 'dark:text-[color:var(--fg)]', 'text-center', 'text-ellipsis', 'text-nowrap']) {
  test(`retains presentation utility ${utility}`, () => assert.equal(isTypographyUtility(utility), false))
}

test('inspects conditional/dynamic class string values and referenced local bindings', async t => {
  const result = await fixture(t, { 'src/Page.vue': `<template><AppHeading variant="section" :class="[titleClass, { 'md:font-bold': active }, ok ? 'text-lg' : 'mb-2']">Page</AppHeading></template><script setup>const titleClass = 'font-' + 'serif'</script>` })
  assert.equal(result.errors.filter(error => error.code === 'heading-utility').length, 3)
})

test('fails closed on uninspectable classes, style objects and attribute spreads', async t => {
  const result = await fixture(t, { 'src/Page.vue': '<template><AppHeading variant="section" :class="externalClasses" :style="externalStyle" v-bind="attrs" /></template>' })
  assert.deepEqual(result.errors.map(error => error.code).sort(), ['heading-opaque-attributes', 'heading-opaque-class', 'heading-opaque-style'])
})

test('inspects static and bound typography styles including role variables', async t => {
  const result = await fixture(t, { 'src/Page.vue': `<template><AppHeading variant="section" style="font-size:20px" :style="{ fontFamily: family, lineHeight: 2, '--type-weight': weight }" /></template>` })
  assert.equal(result.errors.filter(error => error.code === 'heading-inline-type').length, 4)
})

test('rejects raw, aria and dynamic render headings', async t => {
  const result = await fixture(t, {
    'src/Page.vue': `<template><h2 class="sr-only">Unregistered</h2><div role="heading">Bypass</div><component :is="'h3'">Bypass</component></template>`,
    'src/render.ts': `import {h} from 'vue'; export const title = () => h('h2', 'Bypass')`,
  })
  assert.equal(result.errors.filter(error => error.code === 'raw-heading').length, 3)
  assert(result.errors.some(error => error.code === 'raw-render-heading'))
})

test('requires static supported role and prohibits data attribute override', async t => {
  const result = await fixture(t, { 'src/Page.vue': `<template><AppHeading :variant="kind" data-type-role="hero" /><AppHeading variant="jumbo" /></template>` })
  assert.equal(result.errors.filter(error => error.code === 'heading-role').length, 2)
  assert(result.errors.some(error => error.code === 'heading-role-override'))
})

test('detects local class, tag, :is, :deep and @apply selectors', async t => {
  const result = await fixture(t, { 'src/Page.vue': `<template><AppHeading variant="section" class="local-title" /></template><style scoped>.local-title {font-size:12px} h2 {font-weight:900} .parent :is(h2,h3) {line-height:2} :deep(h4) {@apply font-serif text-xl} .local-title span {font-size:12px}</style>` })
  assert.equal(result.errors.filter(error => error.code === 'heading-css-type').length, 3)
  assert.equal(result.errors.filter(error => error.code === 'heading-css-apply').length, 1)
  assert(result.review.some(item => item.selector === '.local-title span'))
})

test('detects shared global class and custom role variable overrides', async t => {
  const result = await fixture(t, {
    'src/Page.vue': '<template><AppHeading variant="section" class="shared-title" /></template>',
    'src/styles.css': '.shared-title {font-size:11px} :root {--ritel-type-page-size:20px;--ritel-text-xl:40px} .ritel-type {--type-size:30px}',
  })
  assert(result.errors.some(error => error.code === 'heading-css-type'))
  assert.equal(result.errors.filter(error => error.code === 'type-token-duplicate').length, 3)
})

test('detects heading ID selectors and copied inline body font families', async t => {
  const result = await fixture(t, {
    'src/Page.vue': `<template><AppHeading id="page-title" variant="page"/><span style="font-family: Arial">Label</span><span :style="{ fontFamily: 'Copied Font' }">Label</span></template><style scoped>#page-title {font-size:20px}</style>`,
  })
  assert(result.errors.some(error => error.code === 'heading-css-type'))
  assert.equal(result.errors.filter(error => error.code === 'font-family-literal').length, 2)
})

test('prevents copied CSS, SVG, JS and Tailwind font families', async t => {
  const result = await fixture(t, {
    'src/Page.vue': '<template><svg><text font-family="Arial">Label</text></svg></template><style scoped>.body {font-family:"Arial"}</style>',
    'src/chart.ts': 'export const options = {font: {family: "Copied Font"}, style: {fontFamily:"Copied Font"}}',
    'tailwind.config.js': tailwind.replace("display: ['var(--ritel-display)']", "display: ['Cinzel', 'serif']"),
  })
  assert.equal(result.errors.filter(error => error.code === 'font-family-literal').length, 4)
  assert(result.errors.some(error => error.code === 'tailwind-family'))
})

test('allows existing central family references and @font-face source definitions', async t => {
  const result = await fixture(t, { 'src/styles.css': '.body{font-family:var(--ritel-ui)} code{font-family:inherit}', 'public/brand/fonts/test.css': '@font-face {font-family:"Actual Font";src:url(./font.woff2);font-weight:400 700}' })
  assert.deepEqual(result.errors, [])
})

test('exact sr-only exception cannot admit a visible/second heading', async t => {
  const exceptions = { ...emptyExceptions, rawHeadings: [{ file: 'src/Page.vue', tag: 'h2', id: 'screen-label', class: 'sr-only', expectedCount: 1, reason: 'Accessible region name.' }] }
  const accepted = await fixture(t, { 'src/Page.vue': '<template><h2 id="screen-label" class="sr-only">Label</h2></template>' }, { exceptions })
  assert.deepEqual(accepted.errors, [])
  const visible = await fixture(t, { 'src/Page.vue': '<template><h2 id="screen-label">Label</h2></template>' }, { exceptions })
  assert(visible.errors.some(error => error.code === 'raw-heading'))
  assert(visible.errors.some(error => error.code === 'stale-exception'))
  const duplicate = await fixture(t, { 'src/Page.vue': '<template><h2 id="screen-label" class="sr-only"/><h2 id="screen-label" class="sr-only"/></template>' }, { exceptions })
  assert(duplicate.errors.some(error => error.code === 'stale-exception'))
})

test('exact document selector exceptions cannot admit other page selectors', async t => {
  const exceptions = { ...emptyExceptions, contentSelectors: [{ file: 'src/Page.vue', selector: '.user-content h2', expectedCount: 1, reason: 'Rendered user document.' }] }
  const result = await fixture(t, { 'src/Page.vue': '<template><AppHeading variant="page" /></template><style>.user-content h2 {@apply text-xl font-bold} .other h2 {font-size:20px}</style>' }, { exceptions })
  assert.equal(result.accepted.length, 1)
  assert.equal(result.errors.filter(error => error.code === 'heading-css-type').length, 1)
})

test('detects central import omission and missing/rebound role tokens', async t => {
  const result = await fixture(t, {
    'public/brand/brand.css': ':root{--ritel-ui:Arial}',
    'public/brand/typography.css': typography.replace('--ritel-type-page-size: 16px;', '').replace('--type-size: var(--ritel-type-dialog-size);', '--type-size: 20px;'),
  })
  assert(result.errors.some(error => error.code === 'central-role-import'))
  assert.equal(result.errors.filter(error => error.code === 'central-role-contract').length, 2)
  assert.equal(result.errors.filter(error => error.code === 'central-family-contract').length, 3)
})

for (const syntax of ['"./typography.css"', "'./typography.css'", "url('./typography.css')", 'url("./typography.css")', 'url(./typography.css)']) {
  test(`accepts canonical CSS import syntax ${syntax}`, async t => {
    const result = await fixture(t, { 'public/brand/brand.css': `@import ${syntax}; :root { --ritel-ui: "UI", sans-serif; --ritel-display: "Display", serif; --ritel-heading: "Heading", serif; --ritel-mono: "Mono", monospace; }` })
    assert.deepEqual(result.errors, [])
  })
}

test('fixture/test source is not counted as runtime coverage', async t => {
  const result = await fixture(t, { 'src/__tests__/Fixture.vue': '<template><h1>Fixture</h1></template>', 'src/Other.spec.ts': 'const x = {fontFamily:"Arial"}' })
  assert.deepEqual(result.errors, [])
  assert.equal(result.stats.vue, 0)
})

for (const heading of ['<AppHeading variant="page" />', '<AppHeading variant="auth" />', '<AppHeading variant="hero" />', '<AppHeading variant="section" as="h1" />']) {
  test(`rejects implicit layout fallback with local ${heading}`, async t => {
    const result = await fixture(t, { 'src/Page.vue': `<template><AppLayout><section>${heading}</section></AppLayout></template>` })
    assert.equal(result.errors.filter(error => error.code === 'page-title-ownership').length, 1)
  })
}

for (const policy of [':show-page-title="false"', ':show-page-title="!showLocalTitle"', ':showPageTitle="loading"']) {
  test(`accepts explicit local page-title ownership ${policy}`, async t => {
    const result = await fixture(t, { 'src/Page.vue': `<template><AppLayout ${policy}><AppHeading v-if="showLocalTitle" variant="page" /></AppLayout></template>` })
    assert.deepEqual(result.errors, [])
  })
}

for (const policy of [':show-page-title="true"', 'show-page-title="false"', 'show-page-title', ':show-page-title="\'false\'"', 'v-bind="layoutOptions"']) {
  test(`rejects ineffective ownership policy ${policy}`, async t => {
    const result = await fixture(t, { 'src/Page.vue': `<template><AppLayout ${policy}><AppHeading variant="page" /></AppLayout></template>` })
    assert.equal(result.errors.filter(error => error.code === 'page-title-ownership').length, 1)
  })
}

test('section headings and headings outside the layout do not require fallback ownership', async t => {
  const result = await fixture(t, { 'src/Page.vue': '<template><AppHeading variant="page"/><AppLayout><AppHeading variant="section"/><AppHeading variant="minor"/><AppHeading variant="page" as="h2"/></AppLayout></template>' })
  assert.deepEqual(result.errors, [])
})

for (const [name, replacement, code] of [
  ['literal size', "'sm': ['14px', { lineHeight: 'var(--ritel-leading-body)' }],", 'tailwind-size'],
  ['wrong scale token', "'sm': ['var(--ritel-text-lg)', { lineHeight: 'var(--ritel-leading-body)' }],", 'tailwind-size'],
  ['literal leading', "'sm': ['var(--ritel-text-sm)', { lineHeight: '1.5' }],", 'tailwind-leading'],
  ['wrong leading token', "'sm': ['var(--ritel-text-sm)', { lineHeight: 'var(--local-leading)' }],", 'tailwind-leading'],
  ['hidden weight override', "'sm': ['var(--ritel-text-sm)', { lineHeight: 'var(--ritel-leading-body)', fontWeight: '900' }],", 'tailwind-leading'],
  ['missing scale', '', 'tailwind-size'],
]) {
  test(`rejects Tailwind text-scale drift: ${name}`, async t => {
    const config = tailwind.replace("'sm': ['var(--ritel-text-sm)', { lineHeight: 'var(--ritel-leading-body)' }],", replacement)
    const result = await fixture(t, { 'tailwind.config.js': config })
    assert(result.errors.some(error => error.code === code), JSON.stringify(result.errors))
  })
}

test('rejects removed fontSize mapping and unregistered scale additions', async t => {
  const missing = await fixture(t, { 'tailwind.config.js': tailwind.replace(`, fontSize: { ${scaleMap} }`, '') })
  assert(missing.errors.some(error => error.code === 'tailwind-size' && error.message.includes('found 0')))
  const added = await fixture(t, { 'tailwind.config.js': tailwind.replace('fontSize: {', "fontSize: { custom: ['30px', {lineHeight:'1'}],") })
  assert(added.errors.some(error => error.code === 'tailwind-size' && error.message.includes('new text scales')))
})

test('requires all shared scale and leading CSS variables', async t => {
  const result = await fixture(t, { 'public/brand/typography.css': typography.replace('--ritel-text-sm: 16px;', '').replace('--ritel-leading-body: 1.5;', '') })
  assert.equal(result.errors.filter(error => error.code === 'central-scale-contract').length, 2)
})

test('unscoped styles in another SFC cannot override heading classes', async t => {
  const result = await fixture(t, {
    'src/Page.vue': '<template><AppHeading variant="page" class="foreign-title" /></template>',
    'src/Other.vue': '<template><p>Other page</p></template><style>.foreign-title {font-size:70px}</style>',
  })
  assert(result.errors.some(error => error.code === 'heading-css-type' && error.file === 'src/Other.vue'))
})

test('scoped non-heading styles in another SFC stay an informational review entry', async t => {
  const result = await fixture(t, {
    'src/Page.vue': '<template><AppHeading variant="page" class="foreign-title" /></template>',
    'src/Other.vue': '<template><p class="foreign-title">Other page</p></template><style scoped>.foreign-title {font-size:70px}</style>',
  })
  assert.deepEqual(result.errors, [])
  assert(result.review.some(item => item.file === 'src/Other.vue' && item.selector === '.foreign-title'))
})
