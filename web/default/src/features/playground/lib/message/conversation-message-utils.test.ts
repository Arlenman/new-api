import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES } from '../../constants.ts'
import type { Message } from '../../types.ts'
import {
  appendUserImageMessagePair,
  applyMessageEdit,
  createRegenerateMessageAction,
} from './conversation-message-utils.ts'

function message(
  key: string,
  from: Message['from'],
  content: string,
  mode?: Message['mode']
): Message {
  return {
    key,
    from,
    mode,
    versions: [{ id: `${key}-version`, content }],
  }
}

describe('conversation message utilities', () => {
  test('marks image generation message pairs with image mode', () => {
    const messages = appendUserImageMessagePair([], 'cute cat')

    assert.equal(messages[0].mode, 'image')
    assert.equal(messages[1].mode, 'image')
  })

  test('regenerates image assistant messages through image action', () => {
    const messages = [
      message('user-1', MESSAGE_ROLES.USER, 'cute cat', 'image'),
      message('assistant-1', MESSAGE_ROLES.ASSISTANT, 'Request error', 'image'),
    ]

    const action = createRegenerateMessageAction(messages, 'assistant-1')

    assert.equal(action?.mode, 'image')
    if (action?.mode !== 'image') {
      throw new Error('expected image regenerate action')
    }
    assert.equal(action.prompt, 'cute cat')
    assert.equal(action.messages.at(-1)?.from, MESSAGE_ROLES.ASSISTANT)
    assert.equal(action.messages.at(-1)?.mode, 'image')
  })

  test('keeps regular chat regeneration on chat action', () => {
    const messages = [
      message('user-1', MESSAGE_ROLES.USER, 'hello'),
      message('assistant-1', MESSAGE_ROLES.ASSISTANT, 'hi'),
    ]

    const action = createRegenerateMessageAction(messages, 'assistant-1')

    assert.equal(action?.mode, 'chat')
  })

  test('regenerates legacy messages through image action when forced by current model', () => {
    const messages = [
      message('user-1', MESSAGE_ROLES.USER, 'cute cat'),
      message('assistant-1', MESSAGE_ROLES.ASSISTANT, 'Request error'),
    ]

    const action = createRegenerateMessageAction(messages, 'assistant-1', {
      forceImage: true,
    })

    assert.equal(action?.mode, 'image')
    if (action?.mode !== 'image') {
      throw new Error('expected image regenerate action')
    }
    assert.equal(action.prompt, 'cute cat')
    assert.equal(action.messages.at(-1)?.mode, 'image')
  })

  test('keeps image edits on image action when submitted again', () => {
    const messages = [
      message('user-1', MESSAGE_ROLES.USER, 'cute cat', 'image'),
      message('assistant-1', MESSAGE_ROLES.ASSISTANT, 'Request error', 'image'),
    ]

    const result = applyMessageEdit(messages, 'user-1', 'cute dog', true)

    assert.equal(result?.shouldSend, true)
    assert.equal(result?.mode, 'image')
    if (result?.mode !== 'image') {
      throw new Error('expected image edit action')
    }
    assert.equal(result.prompt, 'cute dog')
    assert.equal(result.messages.at(-1)?.from, MESSAGE_ROLES.ASSISTANT)
    assert.equal(result.messages.at(-1)?.mode, 'image')
  })

  test('keeps legacy edits on image action when forced by current model', () => {
    const messages = [
      message('user-1', MESSAGE_ROLES.USER, 'cute cat'),
      message('assistant-1', MESSAGE_ROLES.ASSISTANT, 'Request error'),
    ]

    const result = applyMessageEdit(messages, 'user-1', 'cute dog', true, {
      forceImage: true,
    })

    assert.equal(result?.shouldSend, true)
    assert.equal(result?.mode, 'image')
    if (result?.mode !== 'image') {
      throw new Error('expected image edit action')
    }
    assert.equal(result.prompt, 'cute dog')
    assert.equal(result.messages.at(-1)?.mode, 'image')
  })
})
