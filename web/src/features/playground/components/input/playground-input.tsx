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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  PromptInput,
  PromptInputAttachment,
  PromptInputAttachments,
  PromptInputFooter,
  PromptInputHeader,
  PromptInputTextarea,
  type PromptInputMessage,
} from '@/components/ai-elements/prompt-input'

import { MAX_PLAYGROUND_ATTACHMENT_FILE_SIZE_BYTES } from '../../constants'
import { getSubmittableInputText } from '../../lib'
import {
  extractPlaygroundAttachmentText,
  isImageAttachment,
  isPdfAttachment,
  stripTransientAttachmentFields,
} from '../../lib/attachment/playground-attachment-text'
import { getAttachmentOnlySubmitText } from '../../lib/image/playground-image-utils'
import type {
  ModelOption,
  GroupOption,
  PlaygroundImageFile,
  PlaygroundSubmitPayload,
  ParameterEnabled,
  PlaygroundConfig,
} from '../../types'
import { PlaygroundInputControls } from './playground-input-controls'
import { PlaygroundInputTools } from './playground-input-tools'

interface PlaygroundInputProps {
  onSubmit: (payload: PlaygroundSubmitPayload) => void
  config: PlaygroundConfig
  onStop?: () => void
  disabled?: boolean
  isGenerating?: boolean
  models: ModelOption[]
  modelValue: string
  onModelChange: (value: string) => void
  isModelLoading?: boolean
  groups: GroupOption[]
  groupValue: string
  onGroupChange: (value: string) => void
  imageSizeValue: string
  onImageSizeChange: (value: string) => void
  hasMessages?: boolean
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onClearMessages?: () => void
  onParameterEnabledChange: (
    key: keyof ParameterEnabled,
    value: boolean
  ) => void
  parameterEnabled: ParameterEnabled
}

export function PlaygroundInput({
  config,
  onSubmit,
  onStop,
  disabled,
  isGenerating,
  models,
  modelValue,
  onModelChange,
  isModelLoading = false,
  groups,
  groupValue,
  onGroupChange,
  imageSizeValue,
  onImageSizeChange,
  hasMessages = false,
  onConfigChange,
  onClearMessages,
  onParameterEnabledChange,
  parameterEnabled,
}: PlaygroundInputProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')

  const parseFilesForSubmit = async (
    files: NonNullable<PromptInputMessage['files']>
  ): Promise<PlaygroundImageFile[]> => {
    const parsedFiles: PlaygroundImageFile[] = []

    for (const file of files) {
      if (isImageAttachment(file)) {
        parsedFiles.push(stripTransientAttachmentFields(file))
        continue
      }

      const parsedFile = await extractPlaygroundAttachmentText(file)
      if (
        parsedFile.extractionStatus !== 'complete' &&
        !isPdfAttachment(parsedFile)
      ) {
        const filename = parsedFile.filename || t('Attachment')
        const error = parsedFile.error || t('Failed to read attachment')
        toast.error(t('Attachment cannot be sent'), {
          description: `${filename}：${error}`,
        })
        throw new Error(error)
      }

      parsedFiles.push(parsedFile)
    }

    return parsedFiles
  }

  const handleSubmit = async (message: PromptInputMessage) => {
    if (disabled) {
      return
    }

    const submittableText = getSubmittableInputText(message)
    const files = message.files ?? []

    if (!submittableText && files.length === 0) return
    const attachmentOnlyText = getAttachmentOnlySubmitText(
      modelValue,
      t('Generate an image from this reference')
    )

    const parsedFiles = await parseFilesForSubmit(files)

    onSubmit({
      text: submittableText ?? attachmentOnlyText,
      files: parsedFiles,
      imageSize: imageSizeValue,
    })
    setText('')
  }

  return (
    <div className='grid shrink-0 gap-4'>
      <PromptInput
        className='relative'
        groupClassName='bg-background/95 dark:bg-background/80 border-border/70 shadow-[0_18px_60px_-32px_rgba(0,0,0,0.65)] ring-1 ring-foreground/5 rounded-xl overflow-hidden transition-all duration-200 focus-within:border-primary/45 focus-within:ring-primary/15 focus-within:shadow-[0_22px_70px_-34px_rgba(0,0,0,0.75)]'
        multiple
        maxFileSize={MAX_PLAYGROUND_ATTACHMENT_FILE_SIZE_BYTES}
        onError={({ message }) => toast.error(message)}
        onSubmit={handleSubmit}
      >
        <PromptInputTextarea
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          className='min-h-20 px-5 pt-4 pb-3 leading-7 md:min-h-24 md:text-base'
          disabled={disabled}
          onChange={(event) => setText(event.target.value)}
          placeholder={t('Ask anything')}
          value={text}
        />

        <PromptInputHeader className='px-3 pt-3'>
          <PromptInputAttachments>
            {(attachment) => <PromptInputAttachment data={attachment} />}
          </PromptInputAttachments>
        </PromptInputHeader>

        <PromptInputFooter className='border-border/60 bg-muted/20 dark:bg-muted/10 border-t px-3 py-2.5 backdrop-blur'>
          <PlaygroundInputControls
            disabled={disabled}
            groups={groups}
            groupValue={groupValue}
            isGenerating={isGenerating}
            isModelLoading={isModelLoading}
            models={models}
            modelValue={modelValue}
            onGroupChange={onGroupChange}
            imageSizeValue={imageSizeValue}
            onImageSizeChange={onImageSizeChange}
            onModelChange={onModelChange}
            onStop={onStop}
            text={text}
            tools={
              <PlaygroundInputTools
                config={config}
                disabled={disabled}
                hasMessages={hasMessages}
                onConfigChange={onConfigChange}
                onClearMessages={onClearMessages}
                onParameterEnabledChange={onParameterEnabledChange}
                parameterEnabled={parameterEnabled}
              />
            }
          />
        </PromptInputFooter>
      </PromptInput>
    </div>
  )
}
