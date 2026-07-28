import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const conversationHookSource = readFileSync(
  new URL('./use-playground-conversation.ts', import.meta.url),
  'utf8'
)

test('validates the selected image model before adding regeneration loading state', () => {
  const regenerateStart = conversationHookSource.indexOf(
    'const handleRegenerateMessage'
  )
  const regenerateEnd = conversationHookSource.indexOf(
    'const canRegenerateMessage',
    regenerateStart
  )
  const regenerateSource = conversationHookSource.slice(
    regenerateStart,
    regenerateEnd
  )
  const modelValidationIndex = regenerateSource.indexOf(
    'shouldBlockImageActionForModel'
  )
  const messageUpdateIndex = regenerateSource.indexOf(
    'updateMessages(action.messages)'
  )

  assert.ok(modelValidationIndex >= 0)
  assert.ok(messageUpdateIndex >= 0)
  assert.ok(modelValidationIndex < messageUpdateIndex)
})

test('blocks retrying an unfinished image before adding regeneration loading state', () => {
  const regenerateStart = conversationHookSource.indexOf(
    'const handleRegenerateMessage'
  )
  const regenerateEnd = conversationHookSource.indexOf(
    'const canRegenerateMessage',
    regenerateStart
  )
  const regenerateSource = conversationHookSource.slice(
    regenerateStart,
    regenerateEnd
  )
  const pendingValidationIndex = regenerateSource.indexOf(
    'hasPendingImageGenerationForMessage'
  )
  const messageUpdateIndex = regenerateSource.indexOf(
    'updateMessages(action.messages)'
  )

  assert.ok(pendingValidationIndex >= 0)
  assert.ok(messageUpdateIndex >= 0)
  assert.ok(pendingValidationIndex < messageUpdateIndex)
})
