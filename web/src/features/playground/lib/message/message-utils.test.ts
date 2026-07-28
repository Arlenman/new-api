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

  test('sends scanned PDFs as raw file content parts without extracted text', () => {
    const messages = appendUserMessagePair([], '总结扫描文档', [
      {
        url: 'data:application/pdf;base64,c2Nhbm5lZA==',
        mediaType: 'application/pdf',
        filename: 'scanned.pdf',
        extractionStatus: 'empty',
        error: '未提取到可读文本',
      },
    ])

    assert.equal(isValidMessage(messages[0]), true)
    assert.deepEqual(formatMessageForAPI(messages[0]).content, [
      { type: 'text', text: '总结扫描文档' },
      {
        type: 'file',
        file: {
          filename: 'scanned.pdf',
          file_data: 'data:application/pdf;base64,c2Nhbm5lZA==',
        },
      },
    ])
  })

  test('sends both extracted PDF text and the original PDF file', () => {
    const apiMessage = formatMessageForAPI(
      userMessage([
        {
          url: 'data:application/pdf;base64,cGRm',
          mediaType: 'application/pdf',
          filename: 'contract.pdf',
          extractedText: 'contract text',
          extractionStatus: 'complete',
        },
      ])
    )

    assert.ok(Array.isArray(apiMessage.content))
    assert.equal(apiMessage.content[0].type, 'text')
    if (apiMessage.content[0].type !== 'text') assert.fail('missing text part')
    assert.match(apiMessage.content[0].text, /contract text/)
    assert.deepEqual(apiMessage.content[1], {
      type: 'file',
      file: {
        filename: 'contract.pdf',
        file_data: 'data:application/pdf;base64,cGRm',
      },
    })
  })

  test('does not treat unsupported binary attachments as valid file content', () => {
    const messages = appendUserMessagePair([], '', [
      {
        url: 'data:application/octet-stream;base64,YmluYXJ5',
        mediaType: 'application/octet-stream',
        filename: 'archive.bin',
        extractionStatus: 'unsupported',
      },
    ])

    assert.equal(isValidMessage(messages[0]), false)
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
    const content = formatMessageForAPI(messages[0]).content
    assert.ok(Array.isArray(content))
    assert.equal(content[0].type, 'text')
    if (content[0].type !== 'text') assert.fail('missing text part')
    assert.match(content[0].text, /contract text/)
    assert.equal(content[1].type, 'file')
  })
})
