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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  CodeBlock,
  CodeBlockCopyButton,
} from '@/components/ai-elements/code-block'
import { Loader } from '@/components/ai-elements/loader'
import { MessageContent } from '@/components/ai-elements/message'
import {
  Reasoning,
  ReasoningContent,
  ReasoningTrigger,
} from '@/components/ai-elements/reasoning'
import { Response } from '@/components/ai-elements/response'
import { Shimmer } from '@/components/ai-elements/shimmer'
import {
  Source,
  Sources,
  SourcesContent,
  SourcesTrigger,
} from '@/components/ai-elements/sources'
import { cn } from '@/lib/utils'

import { MESSAGE_STATUS } from '../../constants'
import {
  getMessageAlignmentClass,
  getMessageContentState,
  isErrorMessage,
  type MessageAlignment,
} from '../../lib'
import {
  extractMarkdownImageReferences,
  isImageOnlyMarkdownContent,
  stripMarkdownImageReferences,
} from '../../lib/image/playground-image-utils'
import { normalizeImageGenerationRetryableMessage } from '../../lib/message/image-generation-error-utils'
import { getMessageContentStyles } from '../../lib/message/message-styles'
import { getMessageContent } from '../../lib/message/message-utils'
import type { Message } from '../../types'
import { ImageGenerationProgress } from './image-generation-progress'
import { MessageError } from './message-error'
import { MessageMetadata } from './message-metadata'
import { PlaygroundFileImage } from './playground-file-image'
import { PlaygroundMessageAttachments } from './playground-message-attachments'

type PlaygroundMessageContentProps = {
  actions: ReactNode
  alignment: MessageAlignment
  errorActions?: ReactNode
  isSourceVisible?: boolean
  message: Message
  versionContent: string
}

export function PlaygroundMessageContent({
  actions,
  alignment,
  errorActions,
  isSourceVisible = false,
  message,
  versionContent,
}: PlaygroundMessageContentProps) {
  const { t } = useTranslation()
  const normalizedMessage = normalizeImageGenerationRetryableMessage(message)
  const normalizedVersionContent =
    normalizedMessage === message
      ? versionContent
      : getMessageContent(normalizedMessage)
  const {
    displayContent,
    hasReasoning,
    hasSources,
    reasoningContent,
    showLoader,
    showMessageContent,
    sources,
  } = getMessageContentState(normalizedMessage, normalizedVersionContent)
  const isError = isErrorMessage(normalizedMessage)
  const isImageLoading =
    normalizedMessage.mode === 'image' &&
    (normalizedMessage.status === MESSAGE_STATUS.LOADING ||
      normalizedMessage.status === MESSAGE_STATUS.STREAMING)
  const isMessageFinal =
    normalizedMessage.status !== MESSAGE_STATUS.LOADING &&
    normalizedMessage.status !== MESSAGE_STATUS.STREAMING
  const playgroundFileImages = extractMarkdownImageReferences(displayContent)
  const responseDisplayContent =
    playgroundFileImages.length > 0
      ? stripMarkdownImageReferences(displayContent)
      : displayContent
  const isImageOnlyMessage = isImageOnlyMarkdownContent(displayContent)
  const messageContent = (
    <MessageContent
      variant='flat'
      className={cn(
        getMessageContentStyles(),
        isImageOnlyMessage &&
          'gap-0 group-[.is-assistant]:!w-fit group-[.is-assistant]:!max-w-[15rem]'
      )}
    >
      {playgroundFileImages.map((image) => (
        <PlaygroundFileImage image={image} key={`${image.url}-${image.alt}`} />
      ))}
      {responseDisplayContent && (
        <Response final={isMessageFinal}>{responseDisplayContent}</Response>
      )}
      <PlaygroundMessageAttachments attachments={message.attachments} />
    </MessageContent>
  )

  return (
    <div
      className={cn(
        'flex w-full min-w-0 flex-col',
        getMessageAlignmentClass(alignment),
        isImageOnlyMessage && 'relative'
      )}
    >
      {hasSources && (
        <Sources>
          <SourcesTrigger count={sources.length} />
          <SourcesContent>
            {sources.map((source) => (
              <Source
                href={source.href}
                key={`${source.href}-${source.title}`}
                title={source.title}
              />
            ))}
          </SourcesContent>
        </Sources>
      )}

      {hasReasoning && (
        <Reasoning
          defaultOpen
          duration={normalizedMessage.reasoning?.duration}
          isStreaming={normalizedMessage.isReasoningStreaming}
        >
          <ReasoningTrigger />
          <ReasoningContent>{reasoningContent}</ReasoningContent>
        </Reasoning>
      )}

      {isImageLoading && (
        <ImageGenerationProgress message={normalizedMessage} />
      )}

      {showLoader && !isImageLoading && (
        <div className='flex items-center gap-2 py-2'>
          <Loader />
          <Shimmer className='text-sm' duration={1}>
            {t('Responding...')}
          </Shimmer>
        </div>
      )}

      {isError && (
        <>
          <MessageError message={normalizedMessage} className='mb-2' />
          <MessageMetadata alignment={alignment} message={normalizedMessage} />
          {errorActions}
        </>
      )}

      {!isError && showMessageContent && (
        <>
          {isSourceVisible && (
            <CodeBlock
              code={normalizedVersionContent}
              className='my-0 group-[.is-assistant]:w-full group-[.is-assistant]:max-w-[78ch]'
              collapsedLines={24}
              defaultCollapsed={false}
              language='markdown'
              maxExpandedLines={48}
              showLineNumbers
              showToolbar
              title={t('Raw response')}
            >
              <CodeBlockCopyButton />
            </CodeBlock>
          )}
          {!isSourceVisible && isImageOnlyMessage && (
            <div className='relative w-fit max-w-full'>
              {messageContent}
              {actions}
            </div>
          )}
          {!isSourceVisible && !isImageOnlyMessage && messageContent}
          <MessageMetadata
            alignment={alignment}
            compact={isImageOnlyMessage}
            message={normalizedMessage}
          />
          {(!isImageOnlyMessage || isSourceVisible) && actions}
        </>
      )}
    </div>
  )
}
