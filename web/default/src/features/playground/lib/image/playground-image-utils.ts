/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { DEFAULT_IMAGE_SIZE } from '../../constants.ts'
import type {
  ImageGenerationResponse,
  PlaygroundImageSizeOption,
} from '../../types.ts'

const IMAGE_MODEL_PATTERN =
  /(?:^|[-_/\s])(?:image|img|gpt[-_/\s]?image|gptimage|dall|dalle|imagen|flux|midjourney|jimeng|kling|wanx|stable[-_/\s]?diffusion|sdxl|seedream|recraft|playground[-_/\s]?v|sora|veo)(?:$|[-_/\s\d])/i

const IMAGE_MODEL_TEXT_PATTERN =
  /(生图|绘图|画图|图片生成|生成图片|图像生成|生成图像|文生图|图生图|出图|画面生成|海报生成)/i

const IMAGE_PROMPT_INTENT_PATTERN =
  /(生成|创建|画|绘制|做|出|generate|create|draw|make).{0,16}(图片|图像|图|画面|海报|image|picture|photo|poster)|(?:图片|图像|图|画面|海报|image|picture|photo|poster).{0,16}(生成|创建|绘制|generate|create|draw)|(?:画|绘制|draw)\s*(?:一|1)?(?:张|幅|个)?/i

export function isImageGenerationModel(model: string): boolean {
  return IMAGE_MODEL_PATTERN.test(model) || IMAGE_MODEL_TEXT_PATTERN.test(model)
}

export function shouldStreamPlaygroundImageGeneration(model: string): boolean {
  return /gpt[-_ ]?image/i.test(model) || IMAGE_MODEL_TEXT_PATTERN.test(model)
}

export function hasImageGenerationIntent(text: string): boolean {
  return IMAGE_PROMPT_INTENT_PATTERN.test(text)
}

export function getAttachmentOnlySubmitText(
  model: string,
  imageGenerationFallback: string
): string {
  return isImageGenerationModel(model) ? imageGenerationFallback : ''
}

export function shouldUseImageGenerationPath(
  model: string,
  _text: string
): boolean {
  return isImageGenerationModel(model)
}

export function shouldBlockImageActionForModel(
  mode: 'chat' | 'image',
  model: string
): boolean {
  return mode === 'image' && !isImageGenerationModel(model)
}

export function shouldBlockImageSubmissionForModel(
  _model: string,
  _text: string
): boolean {
  return false
}

const AUTO_SIZE_OPTION: PlaygroundImageSizeOption = {
  label: 'Auto',
  value: DEFAULT_IMAGE_SIZE,
}

function sizeOption(
  value: string,
  label: string = value
): PlaygroundImageSizeOption {
  return { label, value }
}

export function getImageSizeOptions(
  model: string
): PlaygroundImageSizeOption[] {
  const normalizedModel = model.toLowerCase()

  if (/dall[-_ ]?e[-_ ]?2/.test(normalizedModel)) {
    return [
      AUTO_SIZE_OPTION,
      sizeOption('256x256'),
      sizeOption('512x512'),
      sizeOption('1024x1024', 'Square'),
    ]
  }

  if (/dall[-_ ]?e[-_ ]?3/.test(normalizedModel)) {
    return [
      AUTO_SIZE_OPTION,
      sizeOption('1024x1024', 'Square'),
      sizeOption('1024x1792', 'Portrait'),
      sizeOption('1792x1024', 'Landscape'),
    ]
  }

  if (/gpt[-_ ]?image/.test(normalizedModel)) {
    return [
      AUTO_SIZE_OPTION,
      sizeOption('1024x1024', 'Square'),
      sizeOption('1024x1536', 'Portrait'),
      sizeOption('1536x1024', 'Landscape'),
    ]
  }

  return [AUTO_SIZE_OPTION, sizeOption('1024x1024', 'Square')]
}

export function getImageGenerationUrls(
  response: ImageGenerationResponse
): string[] {
  return (response.data ?? [])
    .map((item) => {
      if (item.url) {
        return item.url
      }

      return ''
    })
    .filter(Boolean)
}

export function buildImageAssistantContent(
  response: ImageGenerationResponse
): string {
  const urls = getImageGenerationUrls(response)
  const images = urls.map(
    (url, index) => `![Generated image ${index + 1}](${url})`
  )
  const revisedPrompts = (response.data ?? [])
    .map((item) => item.revised_prompt?.trim())
    .filter(Boolean)

  if (revisedPrompts.length === 0) {
    return images.join('\n\n')
  }

  if (images.length === 0) {
    return revisedPrompts.join('\n\n')
  }

  return `${images.join('\n\n')}\n\n${revisedPrompts.join('\n\n')}`
}

export type PlaygroundFileImageReference = {
  alt: string
  url: string
}

const PLAYGROUND_FILE_IMAGE_MARKDOWN_PATTERN =
  /!\[([^\]]*)\]\((\/api\/playground\/files\/[^)\s]+\/content)\)/g
const MARKDOWN_IMAGE_PATTERN = /!\[([^\]]*)\]\(([^)\s]+)\)/g

export function extractMarkdownImageReferences(
  content: string
): PlaygroundFileImageReference[] {
  return [...content.matchAll(MARKDOWN_IMAGE_PATTERN)].map((match) => ({
    alt: match[1] || 'Generated image',
    url: match[2],
  }))
}

export function extractPlaygroundFileImageReferences(
  content: string
): PlaygroundFileImageReference[] {
  return extractMarkdownImageReferences(content).filter((image) =>
    image.url.startsWith('/api/playground/files/')
  )
}

export function stripMarkdownImageReferences(content: string): string {
  return content
    .replaceAll(MARKDOWN_IMAGE_PATTERN, '')
    .replaceAll(/\n{3,}/g, '\n\n')
    .trim()
}

export function isImageOnlyMarkdownContent(content: string): boolean {
  return (
    extractMarkdownImageReferences(content).length > 0 &&
    !stripMarkdownImageReferences(content)
  )
}

export function stripPlaygroundFileImageMarkdown(content: string): string {
  return content
    .replaceAll(PLAYGROUND_FILE_IMAGE_MARKDOWN_PATTERN, '')
    .replaceAll(/\n{3,}/g, '\n\n')
    .trim()
}
