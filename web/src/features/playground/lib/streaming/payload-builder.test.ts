import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES } from '../../constants.ts'
import type {
  Message,
  ParameterEnabled,
  PlaygroundConfig,
} from '../../types.ts'
import { buildChatCompletionPayload } from './payload-builder.ts'

const config: PlaygroundConfig = {
  model: 'gpt-5.4',
  group: 'default',
  imageSize: 'auto',
  temperature: 0.7,
  top_p: 1,
  max_tokens: 4096,
  frequency_penalty: 0,
  presence_penalty: 0,
  seed: null,
  stream: true,
  imageStream: true,
}

const parameterEnabled: ParameterEnabled = {
  temperature: false,
  top_p: false,
  max_tokens: false,
  frequency_penalty: false,
  presence_penalty: false,
  seed: false,
}

function userMessage(message: Partial<Message>): Message {
  return {
    key: 'user-1',
    from: MESSAGE_ROLES.USER,
    versions: [{ id: 'version-1', content: '提取图片里的文字' }],
    ...message,
  }
}

describe('chat payload builder attachments', () => {
  test('sends uploaded images to GPT-5.4 as chat image content', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          attachments: [
            {
              url: 'data:image/png;base64,dXBsb2FkZWQ=',
              mediaType: 'image/png',
              filename: 'uploaded.png',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    assert.equal(payload.model, 'gpt-5.4')
    assert.deepEqual(payload.messages[0].content, [
      { type: 'text', text: '提取图片里的文字' },
      {
        type: 'image_url',
        image_url: { url: 'data:image/png;base64,dXBsb2FkZWQ=' },
      },
    ])
  })

  test('sends pasted screenshots to GPT-5.4 as chat image content', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          attachments: [
            {
              url: 'data:image/png;base64,c2NyZWVuc2hvdA==',
              mediaType: 'image/png',
              filename: 'screenshot.png',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    assert.deepEqual(payload.messages[0].content, [
      { type: 'text', text: '提取图片里的文字' },
      {
        type: 'image_url',
        image_url: { url: 'data:image/png;base64,c2NyZWVuc2hvdA==' },
      },
    ])
  })

  test('sends extracted PDF text on the chat payload', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          versions: [{ id: 'version-1', content: '总结文档内容' }],
          attachments: [
            {
              url: 'data:application/pdf;base64,cGRm',
              mediaType: 'application/pdf',
              filename: 'contract.pdf',
              size: 2048,
              extractedText: '这是一份合同，甲方为 A 公司。',
              extractionStatus: 'complete',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    const content = payload.messages[0].content
    assert.ok(Array.isArray(content))
    assert.equal(content[0].type, 'text')
    if (content[0].type !== 'text') assert.fail('missing text part')
    assert.match(content[0].text, /总结文档内容/)
    assert.match(content[0].text, /附件内容/)
    assert.match(content[0].text, /contract\.pdf/)
    assert.match(content[0].text, /甲方为 A 公司/)
    assert.deepEqual(content[1], {
      type: 'file',
      file: {
        filename: 'contract.pdf',
        file_data: 'data:application/pdf;base64,cGRm',
      },
    })
  })

  test('sends scanned PDFs as raw file parts on the chat payload', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          versions: [{ id: 'version-1', content: '总结扫描文档' }],
          attachments: [
            {
              url: 'data:application/pdf;base64,c2Nhbm5lZA==',
              mediaType: 'application/pdf',
              filename: 'scanned.pdf',
              extractionStatus: 'empty',
              error: '未提取到可读文本',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    assert.deepEqual(payload.messages[0].content, [
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

  test('sends multiple extracted files in one text block', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          versions: [{ id: 'version-1', content: '对比附件' }],
          attachments: [
            {
              url: 'data:text/plain;base64,b25l',
              mediaType: 'text/plain',
              filename: 'one.txt',
              extractedText: 'first file text',
              extractionStatus: 'complete',
            },
            {
              url: 'data:text/csv;base64,dHdv',
              mediaType: 'text/csv',
              filename: 'two.csv',
              extractedText: 'second,file,text',
              extractionStatus: 'complete',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    assert.match(payload.messages[0].content as string, /one\.txt/)
    assert.match(payload.messages[0].content as string, /first file text/)
    assert.match(payload.messages[0].content as string, /two\.csv/)
    assert.match(payload.messages[0].content as string, /second,file,text/)
  })

  test('allows attachment-only document chat payloads', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          versions: [{ id: 'version-1', content: '' }],
          attachments: [
            {
              url: 'data:text/plain;base64,b25seS1maWxl',
              mediaType: 'text/plain',
              filename: 'only.txt',
              extractedText: 'only attachment text',
              extractionStatus: 'complete',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    assert.equal(typeof payload.messages[0].content, 'string')
    assert.match(payload.messages[0].content as string, /附件内容/)
    assert.match(payload.messages[0].content as string, /only attachment text/)
  })

  test('allows attachment-only image chat payloads', () => {
    const payload = buildChatCompletionPayload(
      [
        userMessage({
          versions: [{ id: 'version-1', content: '' }],
          attachments: [
            {
              url: 'data:image/png;base64,b25seS1pbWFnZQ==',
              mediaType: 'image/png',
              filename: 'only-image.png',
            },
          ],
        }),
      ],
      config,
      parameterEnabled
    )

    assert.deepEqual(payload.messages[0].content, [
      { type: 'text', text: '' },
      {
        type: 'image_url',
        image_url: { url: 'data:image/png;base64,b25seS1pbWFnZQ==' },
      },
    ])
  })
})
