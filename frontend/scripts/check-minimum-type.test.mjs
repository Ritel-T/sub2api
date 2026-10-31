import { test } from 'node:test'
import assert from 'node:assert/strict'
import { minimumTypeErrors } from './check-minimum-type.mjs'
test('rejects small utility, scoped, inline, token and chart font sizes', () => {
  for (const source of ['<span class="md:text-[9px]">x</span>', '<style scoped>.badge{font-size:11px}</style>', '<b style="font-size:.6rem">x</b>', '<span class="[font-size:10px]">x</span>', '--ritel-text-2xs: 11px;', '<script>const options={font:{size:10}}</script>', '<script>const options={titleFont:{size:11}}</script>']) assert.ok(minimumTypeErrors(source).length, source)
})
test('keeps 12px text and switch/icon geometry', () => {
  assert.deepEqual(minimumTypeErrors('<button class="h-5 w-9 text-2xs"/><script>const x={size:10,page_size:9,font:{size:12}}</script><style>.label{font-size:.75rem}</style>'), [])
})
