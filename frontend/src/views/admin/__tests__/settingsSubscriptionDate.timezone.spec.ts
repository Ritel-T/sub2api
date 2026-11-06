// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import ts from 'typescript'

const source = readFileSync('src/views/admin/SettingsView.vue', 'utf8')
const functions = source.slice(source.indexOf('function formatSubscribedAt('), source.indexOf('function quotaPercentage('))
const compiled = ts.transpileModule(functions, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
function inZone(zone: string) {
  const script = compiled + `
const original = Date.parse('2026-11-06T01:30:45Z') / 1000;
console.log(JSON.stringify({date: formatSubscribedAt(original), unchanged: parseSubscribedAt(formatSubscribedAt(original), original), changed: parseSubscribedAt('2026-11-08', original), invalid: parseSubscribedAt('2026-02-30', original)}));`
  return JSON.parse(execFileSync(process.execPath, ['-e', script], { encoding: 'utf8', env: { ...process.env, TZ: zone } }))
}
describe('web search subscription date follows viewer timezone', () => {
  it.each([
    ['America/Los_Angeles', '2026-11-05', '2026-11-08T08:00:00Z'],
    ['Asia/Tokyo', '2026-11-06', '2026-11-07T15:00:00Z'],
    ['Europe/Oslo', '2026-11-06', '2026-11-07T23:00:00Z'],
  ])('%s displays local days and preserves untouched instants', (zone, date, changed) => {
    expect(inZone(zone)).toEqual({ date, unchanged: Date.parse('2026-11-06T01:30:45Z') / 1000, changed: Date.parse(changed) / 1000, invalid: null })
  })
  it('passes the original instant when handling the date input', () => {
    expect(source).toMatch(/parseSubscribedAt\(\s*\(\$event.target as HTMLInputElement\).value,\s*provider.subscribed_at,/)
  })
})
