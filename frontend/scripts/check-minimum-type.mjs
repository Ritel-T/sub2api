import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const excluded = new Set(['node_modules', 'dist', 'build', '__tests__', '__fixtures__', 'tests'])
export function minimumTypeErrors(source, file = 'fixture.vue') {
  const errors = []
  const add = (offset, value) => errors.push({ file, line: source.slice(0, offset).split('\n').length, value })
  const number = '(\\d*\\.?\\d+)'
  const pattern = new RegExp(`text-\\[${number}(px|rem|em)\\]|(?:font-size|\\[font-size)\\s*:\\s*${number}(px|rem|em)|--ritel-text-[\\w-]+\\s*:\\s*${number}(px|rem|em)`, 'g')
  for (const match of source.matchAll(pattern)) {
    const size = Number(match[1] ?? match[3] ?? match[5])
    const unit = match[2] ?? match[4] ?? match[6]
    if (size * (unit === 'px' ? 1 : 16) < 12) add(match.index, match[0])
  }
  // Font properties in Chart.js options and inline style objects, not unrelated
  // icon geometry, pagination sizes, or the shared 20 x 36 switch.
  const scripts = file.endsWith('.vue') ? [...source.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/g)].map(m => ({ text: m[1], offset: m.index + m[0].indexOf(m[1]) })) : [{ text: source, offset: 0 }]
  for (const script of scripts) {
    const ast = ts.createSourceFile(file + '.ts', script.text, ts.ScriptTarget.Latest, true)
    const visit = node => {
      if (ts.isPropertyAssignment(node) && ts.isNumericLiteral(node.initializer)) {
        const name = node.name.getText(ast).replace(/['"]/g, '')
        const fontSize = name === 'fontSize' || name === 'size' && ts.isPropertyAssignment(node.parent.parent) && /^(?:font|titleFont|bodyFont|footerFont)$/.test(node.parent.parent.name.getText(ast))
        if (fontSize && Number(node.initializer.text) < 12) add(script.offset + node.getStart(ast), node.getText(ast))
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
  return errors
}
export async function auditMinimumType(base = root) {
  const errors = []
  async function walk(dir) {
    for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
      if (entry.isDirectory() && !excluded.has(entry.name)) await walk(path.join(dir, entry.name))
      else if (entry.isFile() && /\.(?:vue|css|[cm]?[jt]sx?)$/.test(entry.name) && !/\.(spec|test)\./.test(entry.name)) {
        const file = path.join(dir, entry.name)
        errors.push(...minimumTypeErrors(await fs.readFile(file, 'utf8'), path.relative(base, file)))
      }
    }
  }
  await walk(path.join(base, 'src'))
  // Read shared tokens, but their assets belong to the brand workstream.
  errors.push(...minimumTypeErrors(await fs.readFile(path.join(base, 'public/brand/typography.css'), 'utf8'), 'public/brand/typography.css'))
  return errors
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const errors = await auditMinimumType()
  console.log(`Minimum 12px typography: ${errors.length ? 'FAIL' : 'PASS'} (${errors.length} violations)`)
  for (const error of errors) console.error(`${error.file}:${error.line} ${error.value}`)
  if (errors.length) process.exitCode = 1
}
