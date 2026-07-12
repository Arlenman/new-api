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
// Message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type MessageStatus = 'loading' | 'streaming' | 'complete' | 'error'

export type PlaygroundMessageLayoutMode = 'alternating' | 'left'

export type PlaygroundMessageMode = 'chat' | 'image'

export type ImageGenerationTaskStatus =
  | 'pending'
  | 'complete'
  | 'retryable'
  | 'error'
  | 'cancelled'

export interface MessageImageGenerationState {
  taskId: string
  prompt: string
  size: string
  status: ImageGenerationTaskStatus
  startedAt?: number
  completedAt?: number
  error?: string
}

export interface MessageVersion {
  id: string
  content: string
}

export interface Message {
  key: string
  from: MessageRole
  versions: MessageVersion[]
  attachments?: PlaygroundImageFile[]
  mode?: PlaygroundMessageMode
  imageGeneration?: MessageImageGenerationState
  createdAt?: number
  startedAt?: number
  completedAt?: number
  durationMs?: number
  sources?: { href: string; title: string }[]
  reasoning?: {
    content: string
    duration: number
    startedAt?: number
    completedAt?: number
    durationMs?: number
  }
  isReasoningStreaming?: boolean
  isReasoningComplete?: boolean
  isContentComplete?: boolean
  status?: MessageStatus
  errorCode?: string | null
}

export interface PlaygroundSession {
  id: string
  title: string
  messages: Message[]
  createdAt: number
  updatedAt: number
}

export interface PlaygroundSessionsResponse {
  sessions?: PlaygroundSession[]
}

export interface PlaygroundSessionMutationResponse extends PlaygroundSession {}

// API payload types
export interface ChatCompletionMessage {
  role: MessageRole
  content: string | ContentPart[]
}

export type ContentPart =
  | {
      type: 'text'
      text: string
    }
  | {
      type: 'image_url'
      image_url: {
        url: string
      }
    }
  | {
      type: 'file'
      file: {
        filename?: string
        file_data: string
      }
    }

export interface ChatCompletionRequest {
  model: string
  group?: string
  messages: ChatCompletionMessage[]
  stream: boolean
  temperature?: number
  top_p?: number
  max_tokens?: number
  frequency_penalty?: number
  presence_penalty?: number
  seed?: number
}

export interface ChatCompletionChunk {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    delta: {
      role?: MessageRole
      content?: string
      reasoning_content?: string
    }
    finish_reason: string | null
  }>
}

export interface ChatCompletionResponse {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    message: {
      role: MessageRole
      content: string
      reasoning_content?: string
    }
    finish_reason: string
  }>
  usage?: {
    prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
}

export interface ImageGenerationRequest {
  model: string
  prompt: string
  group?: string
  n?: number
  size?: string
  stream?: boolean
  partial_images?: number
  response_format?: 'url' | 'b64_json'
  session_id?: string
  message_key?: string
}

export interface ImageGenerationResponse {
  created?: number
  status?: 'pending' | 'complete' | 'error' | string
  data?: Array<{
    url?: string
    b64_json?: string
    revised_prompt?: string
  }>
}

export type PlaygroundAttachmentExtractionStatus =
  | 'pending'
  | 'complete'
  | 'empty'
  | 'unsupported'
  | 'error'

export interface PlaygroundImageFile {
  url?: string
  mediaType?: string
  filename?: string
  size?: number
  extractedText?: string
  extractionStatus?: PlaygroundAttachmentExtractionStatus
  error?: string
}

export interface PlaygroundSubmitPayload {
  text: string
  files?: (PlaygroundImageFile & { file?: Blob })[]
  imageSize?: string
}

// Configuration types
export interface PlaygroundConfig {
  model: string
  group: string
  imageSize: string
  temperature: number
  top_p: number
  max_tokens: number
  frequency_penalty: number
  presence_penalty: number
  seed: number | null
  stream: boolean
}

export interface ParameterEnabled {
  temperature: boolean
  top_p: boolean
  max_tokens: boolean
  frequency_penalty: boolean
  presence_penalty: boolean
  seed: boolean
}

// Model and group options
export interface ModelOption {
  label: string
  value: string
}

export interface GroupOption {
  label: string
  value: string
  ratio: number
  desc?: string
}

export interface PlaygroundImageSizeOption {
  label: string
  value: string
}
