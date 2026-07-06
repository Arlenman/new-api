import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES } from '../../constants.ts'
import type { Message } from '../../types.ts'
import { appendUserMessagePair } from './conversation-message-utils.ts'
import { formatMessageForAPI, isValidMessage } from './message-utils.ts'

function userMessage(attachments: Message['attachments']): Message {
  return {
    key: 'user-1',
    from: MESSAGE_ROLES.USER,
    versions: [{ id: 'version-1', content: '提取图片里的文字' }],
    attachments,
  }
}

describe('message utils', () => {
  test('formats image attachments as chat image content parts', () => {
    const apiMessage = formatMessageForAPI(
      userMessage([
        {
          url: 'data:image/png;base64,c2NyZWVuc2hvdA==',
          mediaType: 'image/png',
          filename: 'screenshot.png',
        },
      ])
    )

    assert.deepEqual(apiMessage, {
      role: 'user',
      content: [
        {
          type: 'text',
          text: '提取图片里的文字',
        },
        {
          type: 'image_url',
          image_url: {
            url: 'data:image/png;base64,c2NyZWVuc2hvdA==',
          },
        },
      ],
    })
  })

  test('injects extracted non-image attachment text into the chat text part', () => {
    const apiMessage = formatMessageForAPI(
      userMessage([
        {
          url: 'data:text/plain;base64,aGVsbG8=',
          mediaType: 'text/plain',
          filename: 'notes.txt',
          size: 128,
          extractedText: 'hello from attachment',
          extractionStatus: 'complete',
        },
      ])
    )

    assert.equal(apiMessage.role, 'user')
    assert.equal(typeof apiMessage.content, 'string')
    assert.match(apiMessage.content as string, /提取图片里的文字/)
    assert.match(apiMessage.content as string, /附件内容/)
    assert.match(apiMessage.content as string, /notes\.txt/)
    assert.match(apiMessage.content as string, /hello from attachment/)
  })

  test('keeps attachment-only user messages valid for chat submission', () => {
    const messages = appendUserMessagePair([], '', [
      {
        url: 'data:image/png;base64,c2NyZWVuc2hvdA==',
        mediaType: 'image/png',
        filename: 'screenshot.png',
      },
    ])

    assert.deepEqual(messages[0].attachments, [
      {
        url: 'data:image/png;base64,c2NyZWVuc2hvdA==',
        mediaType: 'image/png',
        filename: 'screenshot.png',
      },
    ])
    assert.equal(isValidMessage(messages[0]), true)
  })

  test('keeps attachment-only document messages valid for chat submission', () => {
    const messages = appendUserMessagePair([], '', [
      {
        url: 'data:application/pdf;base64,cGRm',
        mediaType: 'application/pdf',
        filename: 'contract.pdf',
        size: 1024,
        extractedText: 'contract text',
        extractionStatus: 'complete',
      },
    ])

    assert.equal(isValidMessage(messages[0]), true)
    assert.match(
      formatMessageForAPI(messages[0]).content as string,
      /contract text/
    )
  })
})
