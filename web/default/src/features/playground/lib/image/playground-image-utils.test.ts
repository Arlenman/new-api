import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  buildImageAssistantContent,
  extractMarkdownImageReferences,
  extractPlaygroundFileImageReferences,
  getImageGenerationUrls,
  hasImageGenerationIntent,
  isImageOnlyMarkdownContent,
  isImageGenerationModel,
  shouldStreamPlaygroundImageGeneration,
  shouldBlockImageActionForModel,
  shouldBlockImageSubmissionForModel,
  shouldUseImageGenerationPath,
  stripMarkdownImageReferences,
  stripPlaygroundFileImageMarkdown,
} from './playground-image-utils.ts'

describe('playground image utils', () => {
  test('only treats explicit image generation model names as image models', () => {
    assert.equal(isImageGenerationModel('gpt-image-2'), true)
    assert.equal(isImageGenerationModel('gptimage2'), true)
    assert.equal(isImageGenerationModel('dall-e-3'), true)
    assert.equal(isImageGenerationModel('flux-pro'), true)
    assert.equal(isImageGenerationModel('smallice-生图'), true)
    assert.equal(isImageGenerationModel('即梦绘图'), true)
    assert.equal(isImageGenerationModel('图片生成-pro'), true)

    assert.equal(isImageGenerationModel('gpt-5.4-mini'), false)
    assert.equal(isImageGenerationModel('gpt-5.5-fast'), false)
    assert.equal(isImageGenerationModel('claude-3.5-sonnet'), false)
  })

  test('detects explicit image generation prompt intent', () => {
    assert.equal(hasImageGenerationIntent('一只小猫，生成图片'), true)
    assert.equal(hasImageGenerationIntent('帮我画一张花园里的小猫'), true)
    assert.equal(hasImageGenerationIntent('generate an image of a cat'), true)
    assert.equal(hasImageGenerationIntent('写一段图片压缩代码'), false)
    assert.equal(hasImageGenerationIntent('解释一下图数据库'), false)
  })

  test('uses image generation path for model, prompt, or reference image', () => {
    assert.equal(
      shouldUseImageGenerationPath('gpt-5.4-mini', '一只小猫，生成图片', false),
      true
    )
    assert.equal(
      shouldUseImageGenerationPath('smallice-生图', '一只小猫', false),
      true
    )
    assert.equal(
      shouldUseImageGenerationPath('gpt-5.4-mini', '修改这张图', true),
      true
    )
    assert.equal(
      shouldUseImageGenerationPath('gpt-5.4-mini', '解释一下这段代码', false),
      false
    )
  })

  test('blocks image retry and edit actions when current model is not image capable', () => {
    assert.equal(
      shouldBlockImageActionForModel('image', 'gpt-5.4-mini'),
      true
    )
    assert.equal(
      shouldBlockImageActionForModel('image', 'gpt-image-2'),
      false
    )
    assert.equal(
      shouldBlockImageActionForModel('chat', 'gpt-5.4-mini'),
      false
    )
  })

  test('blocks image submissions when current model is not image capable', () => {
    assert.equal(
      shouldBlockImageSubmissionForModel('gpt-5.4-mini', '修改这张图', true),
      true
    )
    assert.equal(
      shouldBlockImageSubmissionForModel(
        'gpt-5.4-mini',
        '一只小猫，生成图片',
        false
      ),
      true
    )
    assert.equal(
      shouldBlockImageSubmissionForModel('gpt-image-2', '修改这张图', true),
      false
    )
    assert.equal(
      shouldBlockImageSubmissionForModel('gpt-5.4-mini', '解释一下代码', false),
      false
    )
  })

  test('enables playground image streaming for GPT Image models and image aliases', () => {
    assert.equal(shouldStreamPlaygroundImageGeneration('gpt-image-2'), true)
    assert.equal(shouldStreamPlaygroundImageGeneration('gpt_image_1'), true)
    assert.equal(shouldStreamPlaygroundImageGeneration('smallice-生图'), true)
    assert.equal(shouldStreamPlaygroundImageGeneration('即梦绘图'), true)
    assert.equal(shouldStreamPlaygroundImageGeneration('图片生成-pro'), true)
    assert.equal(shouldStreamPlaygroundImageGeneration('dall-e-3'), false)
    assert.equal(shouldStreamPlaygroundImageGeneration('flux-pro'), false)
    assert.equal(shouldStreamPlaygroundImageGeneration('gpt-5.4-mini'), false)
  })

  test('does not convert b64_json into data urls for markdown rendering', () => {
    const response = {
      data: [{ b64_json: 'aW1hZ2UtYnl0ZXM=', revised_prompt: 'revised' }],
    }

    assert.deepEqual(getImageGenerationUrls(response), [])
    assert.equal(buildImageAssistantContent(response), 'revised')
  })

  test('builds image markdown from persisted file urls', () => {
    const response = {
      data: [
        {
          url: '/api/playground/files/pgf_1/content',
          revised_prompt: 'revised',
        },
      ],
    }

    assert.equal(
      buildImageAssistantContent(response),
      '![Generated image 1](/api/playground/files/pgf_1/content)\n\nrevised'
    )
  })

  test('extracts persisted playground file image references', () => {
    const content =
      '![Generated image 1](/api/playground/files/pgf_1/content)\n\n![Second](/api/playground/files/pgf_2/content)\n\nrevised'

    assert.deepEqual(extractPlaygroundFileImageReferences(content), [
      {
        alt: 'Generated image 1',
        url: '/api/playground/files/pgf_1/content',
      },
      {
        alt: 'Second',
        url: '/api/playground/files/pgf_2/content',
      },
    ])
    assert.equal(stripPlaygroundFileImageMarkdown(content), 'revised')
  })

  test('extracts all markdown image references for preview rendering', () => {
    const content =
      'before\n\n![Local](/api/playground/files/pgf_1/content)\n\n![Remote](https://example.com/image.png)\n\nafter'

    assert.deepEqual(extractMarkdownImageReferences(content), [
      {
        alt: 'Local',
        url: '/api/playground/files/pgf_1/content',
      },
      {
        alt: 'Remote',
        url: 'https://example.com/image.png',
      },
    ])
    assert.equal(stripMarkdownImageReferences(content), 'before\n\nafter')
  })

  test('detects image-only markdown without treating mixed text as image-only', () => {
    assert.equal(
      isImageOnlyMarkdownContent(
        '![Generated image 1](/api/playground/files/pgf_1/content)'
      ),
      true
    )
    assert.equal(
      isImageOnlyMarkdownContent(
        '![Generated image 1](/api/playground/files/pgf_1/content)\n\nrevised prompt'
      ),
      false
    )
    assert.equal(isImageOnlyMarkdownContent('plain text only'), false)
  })
})
