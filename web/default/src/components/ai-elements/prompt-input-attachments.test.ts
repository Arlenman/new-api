import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const promptInputSource = readFileSync(
  new URL('./prompt-input.tsx', import.meta.url),
  'utf8'
)

describe('PromptInput attachment file input state', () => {
  test('resets the hidden file input after add, clear and successful submit', () => {
    assert.match(promptInputSource, /resetFileInputValue/)
    assert.match(
      promptInputSource,
      /handleChange[\s\S]+resetFileInputValue\(\)/
    )
    assert.match(promptInputSource, /clear[\s\S]+resetFileInputValue\(\)/)
    assert.match(promptInputSource, /clearAfterSuccessfulSubmit/)
  })

  test('preserves the original File object for parsers before blob URL conversion', () => {
    assert.match(promptInputSource, /file\?: File/)
    assert.match(promptInputSource, /file,\n/)
    assert.match(promptInputSource, /Omit<FileUIPart, 'file'>/)
  })
})
