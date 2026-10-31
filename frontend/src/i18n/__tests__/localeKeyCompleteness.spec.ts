// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'
import ts from 'typescript'
import { parse as parseSfc } from 'vue/compiler-sfc'

import en from '../locales/en'
import zh from '../locales/zh'
import zhHK from '../locales/zh-Hant-HK'

type LocaleValue = Record<string, unknown>

function flattenLeafKeys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return prefix ? [prefix] : []
  }

  return Object.entries(value as LocaleValue).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return flattenLeafKeys(child, path)
  })
}

function collectStaticSourceKeys(source: string): string[] {
  const keys = new Set<string>()

  // Covers useI18n().t(), the global $t(), and direct i18n.t() calls in both
  // TypeScript and Vue script/template source. Dynamic suffixes are checked by
  // the runtime type of the value and cannot be proven from source text alone.
  const translationCalls = /(?:\bi18n\.t|\$t|\bt)\s*\(\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(translationCalls)) {
    const key = match[2]
    if (!key.endsWith('.')) {
      keys.add(key)
    }
  }

  // Router metadata and i18n-t are key references without a t() call.
  const keyReferences = /(?:keypath|titleKey|descriptionKey)\s*[:=]\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(keyReferences)) {
    keys.add(match[2])
  }

  const i18nTKeypaths = /<i18n-t\b[^>]*\bkeypath\s*=\s*(['"])([^'"\r\n]+)\1/gi
  for (const match of source.matchAll(i18nTKeypaths)) {
    keys.add(match[2])
  }

  for (const match of source.matchAll(/['"]((?:consoleInline|consoleFields)\.[A-Za-z0-9_]+)['"]/g)) keys.add(match[1])
  return [...keys]
}

function inlineBilingualCopies(source: string, file: string): string[] {
  const fragments: string[] = []
  if (file.endsWith('.vue')) {
    const descriptor = parseSfc(source).descriptor
    for (const script of [descriptor.script, descriptor.scriptSetup]) if (script) fragments.push(script.content)
    const visit = (node: any) => {
      if (node.type === 5) fragments.push(node.content.content)
      for (const prop of node.props ?? []) if (prop.type === 7 && prop.exp) fragments.push(prop.exp.content)
      for (const child of node.children ?? []) visit(child)
    }
    if (descriptor.template?.ast) visit(descriptor.template.ast)
  } else fragments.push(source)
  const errors: string[] = []
  const leaves = (node: ts.Expression): ts.Expression[] => ts.isConditionalExpression(node) ? [...leaves(node.whenTrue), ...leaves(node.whenFalse)] : [node]
  for (const fragment of fragments) {
    const ast = ts.createSourceFile('copy.ts', fragment, ts.ScriptTarget.Latest, true)
    const visit = (node: ts.Node) => {
      if (ts.isConditionalExpression(node) && /\blocale\b/.test(node.condition.getText(ast))) {
        let root: ts.Expression = node
        while (ts.isConditionalExpression(root.parent)) root = root.parent
        const values = leaves(root)
        if (values.length === 2 && values.every(ts.isStringLiteralLike)) {
          const messages = values.map(value => (value as ts.StringLiteralLike).text)
          const protocol = messages.every(value => /^(?:en(?:-US)?|zh(?:-CN|-cn|-Hant-HK)?|cn|yue|https?:\/\/.*)$/.test(value))
          const translatedKeys = ts.isCallExpression(root.parent) && /^(?:t|\$t|i18n\.t)$/.test(root.parent.expression.getText(ast))
          if (!protocol && !translatedKeys && messages.some(value => /[\p{L}]/u.test(value))) errors.push(node.getText(ast))
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
  return errors
}

function sourceKeys(): string[] {
  const sourceFiles = import.meta.glob('../../**/*.{ts,vue}', {
    query: '?raw',
    import: 'default',
    eager: true
  }) as Record<string, string>

  return Object.entries(sourceFiles)
    .filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))
    .flatMap(([, source]) => collectStaticSourceKeys(source))
}

function missingKeys(usedKeys: string[], availableKeys: Set<string>): string[] {
  return usedKeys.filter((key) => !availableKeys.has(key)).sort()
}

describe('locale key completeness', () => {
  const enKeys = new Set(flattenLeafKeys(en))
  const zhKeys = new Set(flattenLeafKeys(zh))
  const zhHKKeys = new Set(flattenLeafKeys(zhHK))
  const usedKeys = [...new Set(sourceKeys())].sort()

  it('keeps English and Chinese locale schemas identical', () => {
    expect([...enKeys].filter((key) => !zhKeys.has(key)).sort()).toEqual([])
    expect([...zhKeys].filter((key) => !enKeys.has(key)).sort()).toEqual([])
    expect([...enKeys].filter((key) => !zhHKKeys.has(key)).sort()).toEqual([])
    expect([...zhHKKeys].filter((key) => !enKeys.has(key)).sort()).toEqual([])
  })

  it('contains a non-empty message for every locale leaf', () => {
    for (const [locale, messages] of Object.entries({ en, zh, zhHK })) {
      const emptyKeys = flattenLeafKeys(messages).filter((key) => {
        let current: unknown = messages
        for (const segment of key.split('.')) {
          current = (current as LocaleValue)[segment]
        }
        return typeof current !== 'string' || current.trim() === ''
      })
      expect(emptyKeys, `${locale} has empty or non-string messages`).toEqual([])
    }
  })

  it('preserves Hong Kong placeholders, HTML and protocol text', () => {
    const compare = (original: unknown, translated: unknown, path = ''): void => {
      if (typeof original === 'string') {
        expect(typeof translated, path).toBe('string')
        const ascii = (value: string) => [...value].filter((char) => char.charCodeAt(0) <= 0x7f).join('')
        expect(ascii(translated as string), path).toBe(ascii(original))
        return
      }
      for (const [key, child] of Object.entries(original as LocaleValue)) {
        compare(child, (translated as LocaleValue)[key], path ? `${path}.${key}` : key)
      }
    }
    compare(zh, zhHK)
  })

  it('compiles migrated messages in all three languages, including placeholders', () => {
    for (const [locale, messages] of Object.entries({ en, zh, zhHK })) {
      for (const group of ['consoleInline', 'consoleFields', 'consoleUi'] as const) {
        for (const key of flattenLeafKeys(messages[group])) {
          let message: unknown = messages[group]
          for (const segment of key.split('.')) message = (message as LocaleValue)[segment]
          const errors: string[] = []
          baseCompile(message as string, { onError: error => errors.push(error.message) })
          expect(errors, `${locale}.${group}.${key}`).toEqual([])
        }
      }
    }
  })

  it('has no legacy bilingual inline fallback in production sources', () => {
    const files = import.meta.glob('../../**/*.{ts,vue}', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
    for (const [path, source] of Object.entries(files).filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))) {
      expect(source, path).not.toMatch(/\blocalizeInlineText\s*\(|from\s+['"][^'"]*i18n\/inline['"]/)
    }
  })

  it('rejects new bilingual UI branches that bypass Hong Kong messages', () => {
    expect(inlineBilingualCopies("const name = locale.value.startsWith('zh') ? '状态' : 'Status'", 'fixture.ts')).toHaveLength(1)
    expect(inlineBilingualCopies("const name = locale.value === 'en' ? 'Status' : locale.value === 'zh' ? '状态' : '狀態'", 'fixture.ts')).toEqual([])
    const files = import.meta.glob('../../**/*.{ts,vue}', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
    for (const [path, source] of Object.entries(files).filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))) {
      expect(inlineBilingualCopies(source, path), path).toEqual([])
    }
  })

  it('contains every statically referenced production key', () => {
    expect(missingKeys(usedKeys, enKeys), 'English locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, zhKeys), 'Chinese locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, zhHKKeys), 'Hong Kong locale is missing referenced keys').toEqual([])
  })
})
