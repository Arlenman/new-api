import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { Message } from '../../types.ts'
import { sanitizePlaygroundMessagesForPersistence } from './playground-attachment-persistence.ts'

describe('playground attachment persistence', () => {
  test('removes attachment data URLs without mutating the live message', () => {
    const messages: Message[] = [
      {
        key: 'user-pdf',
        from: 'user',
        versions: [{ id: 'v1', content: '分析扫描件' }],
        attachments: [
          {
            url: 'data:application/pdf;base64,c2Nhbm5lZA==',
            mediaType: 'application/pdf',
            filename: 'scanned.pdf',
            size: 12,
            extractedText: '',
            extractionStatus: 'empty',
          },
        ],
      },
    ]

    const sanitized = sanitizePlaygroundMessagesForPersistence(messages)

    assert.equal(sanitized[0].attachments?.[0].url, undefined)
    assert.equal(
      messages[0].attachments?.[0].url,
      'data:application/pdf;base64,c2Nhbm5lZA=='
    )
    assert.equal(sanitized[0].attachments?.[0].filename, 'scanned.pdf')
    assert.equal(sanitized[0].attachments?.[0].extractionStatus, 'empty')
  })

  test('removes image data URLs and preserves non-data attachment URLs', () => {
    const messages: Message[] = [
      {
        key: 'user-files',
        from: 'user',
        versions: [{ id: 'v1', content: '分析附件' }],
        attachments: [
          {
            url: 'data:image/png;base64,aW1hZ2U=',
            mediaType: 'image/png',
            filename: 'image.png',
          },
          {
            url: '/api/playground/files/pdf-1/content',
            mediaType: 'application/pdf',
            filename: 'stored.pdf',
          },
        ],
      },
    ]

    const sanitized = sanitizePlaygroundMessagesForPersistence(messages)

    assert.equal(sanitized[0].attachments?.[0].url, undefined)
    assert.equal(
      sanitized[0].attachments?.[1].url,
      '/api/playground/files/pdf-1/content'
    )
    assert.equal(
      messages[0].attachments?.[0].url,
      'data:image/png;base64,aW1hZ2U='
    )
  })
})
