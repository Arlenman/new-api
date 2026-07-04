import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))

const readLocale = (name: string): Record<string, string> => {
  const locale = JSON.parse(readFileSync(join(currentDir, name), 'utf8')) as {
    translation: Record<string, string>
  }
  return locale.translation
}

describe('menu labels', () => {
  test('uses the current key data label for token tag analytics', () => {
    assert.equal(readLocale('zh.json')['Key Tag Analytics'], '密钥数据')
    assert.equal(readLocale('en.json')['Key Tag Analytics'], 'Key Data')
  })
})
