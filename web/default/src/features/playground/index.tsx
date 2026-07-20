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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PlaygroundChat } from './components/chat/playground-chat'
import { PlaygroundInput } from './components/input/playground-input'
import { PlaygroundSessionSidebar } from './components/session/playground-session-sidebar'
import {
  useChatHandler,
  usePlaygroundConversation,
  usePlaygroundOptions,
  usePlaygroundState,
} from './hooks'

export function Playground() {
  const { t } = useTranslation()
  const {
    config,
    parameterEnabled,
    messages,
    sessions,
    activeSessionId,
    isLoadingMessages,
    models,
    groups,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    updateParameterEnabled,
    clearMessages,
    commitActiveSessionMessages,
    createSession,
    selectSession,
    renameSession,
    deleteSession,
    renameActiveSessionFromMessage,
  } = usePlaygroundState()

  const {
    sendChat,
    sendImage,
    stopGeneration,
    activeImageSessionIds,
    isGenerating,
    isSessionNavigationDisabled,
  } = useChatHandler({
    activeSessionId,
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
  })

  const {
    editingMessageKey,
    canRegenerateMessage,
    handleSendMessage,
    handleRegenerateMessage,
    handleEditMessage,
    handleEditOpenChange,
    applyEdit,
    handleDeleteMessage,
  } = usePlaygroundConversation({
    messages,
    model: config.model,
    imageSize: config.imageSize,
    updateMessages,
    commitActiveSessionMessages,
    sendChat,
    sendImage,
    onFirstMessage: renameActiveSessionFromMessage,
    onInvalidImageModel: () =>
      toast.error(t('Switch to an image model to generate images')),
    onPendingImageGeneration: () =>
      toast.warning(t('Please wait for the current generation to complete')),
  })

  const handleClearMessages = () => {
    handleEditOpenChange(false)
    clearMessages()
  }

  const { isLoadingModels } = usePlaygroundOptions({
    currentGroup: config.group,
    currentModel: config.model,
    setGroups,
    setModels,
    updateConfig,
  })

  return (
    <div className='relative flex size-full min-h-0 flex-col overflow-hidden md:flex-row'>
      <PlaygroundSessionSidebar
        activeSessionId={activeSessionId}
        disabled={isSessionNavigationDisabled}
        generatingSessionIds={activeImageSessionIds}
        sessions={sessions}
        onCreateSession={createSession}
        onDeleteSession={deleteSession}
        onRenameSession={renameSession}
        onSelectSession={selectSession}
      />

      <div className='relative flex min-h-0 flex-1 flex-col overflow-hidden'>
        {/* Full-width scroll container: scrolling works even over side whitespace */}
        <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
          <PlaygroundChat
            messages={messages}
            isLoadingMessages={isLoadingMessages}
            canRegenerateMessage={canRegenerateMessage}
            onRegenerateMessage={handleRegenerateMessage}
            onEditMessage={handleEditMessage}
            onDeleteMessage={handleDeleteMessage}
            onSelectPrompt={(prompt) => handleSendMessage(prompt)}
            isGenerating={isGenerating}
            editingKey={editingMessageKey}
            onCancelEdit={handleEditOpenChange}
            onSaveEdit={(newContent) => applyEdit(newContent, false)}
            onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
          />
        </div>

        {/* Keep the input aligned with the readable conversation column. */}
        <div className='mx-auto w-full max-w-[88rem] px-4 pb-3 md:px-6 lg:px-8 xl:px-10'>
          <PlaygroundInput
            config={config}
            disabled={isGenerating}
            groups={groups}
            groupValue={config.group}
            isGenerating={isGenerating}
            isModelLoading={isLoadingModels}
            modelValue={config.model}
            models={models}
            onGroupChange={(value) => updateConfig('group', value)}
            imageSizeValue={config.imageSize}
            onConfigChange={updateConfig}
            onClearMessages={handleClearMessages}
            onModelChange={(value) => updateConfig('model', value)}
            onImageSizeChange={(value) => updateConfig('imageSize', value)}
            onParameterEnabledChange={updateParameterEnabled}
            onStop={stopGeneration}
            onSubmit={handleSendMessage}
            parameterEnabled={parameterEnabled}
            hasMessages={messages.length > 0}
          />
        </div>
      </div>
    </div>
  )
}
