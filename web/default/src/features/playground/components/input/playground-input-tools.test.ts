import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const playgroundInputToolsSource = readFileSync(
  new URL('./playground-input-tools.tsx', import.meta.url),
  'utf8'
)

describe('PlaygroundInputTools attachment entry', () => {
  test('keeps file upload on the paperclip menu instead of a duplicate image button', () => {
    assert.doesNotMatch(playgroundInputToolsSource, /ImageIcon/)
    assert.doesNotMatch(
      playgroundInputToolsSource,
      /PromptInputActionAddAttachments/
    )
    assert.match(playgroundInputToolsSource, /usePromptInputAttachments/)
    assert.match(playgroundInputToolsSource, /attachments\.openFileDialog\(\)/)
    assert.match(playgroundInputToolsSource, /upload-photo/)
  })
})
