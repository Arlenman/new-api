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
import { useCallback, useEffect, useRef, useState } from 'react'

import {
  createPlaygroundSessionRemote,
  deletePlaygroundSessionRemote,
  getPlaygroundSessions,
  importPlaygroundSessions,
  renamePlaygroundSessionRemote,
  savePlaygroundSessionMessages,
} from '../api'
import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../constants'
import {
  saveConfig,
  saveParameterEnabled,
  applyMessageStateUpdate,
  getInitialParameterEnabled,
  getInitialPlaygroundConfig,
  buildInitialPlaygroundSessions,
  createPlaygroundSession,
  DEFAULT_PLAYGROUND_SESSION_TITLE,
  deletePlaygroundSession,
  getSessionTitleFromMessage,
  loadActiveSessionId,
  loadMessages,
  loadSessions,
  renamePlaygroundSession,
  saveActiveSessionId,
  saveSessions,
  updatePlaygroundSessionMessages,
  type MessageStateUpdater,
} from '../lib'
import { imageGenerationTaskManager } from '../lib/image/image-generation-task-manager-singleton'
import type {
  Message,
  PlaygroundConfig,
  ParameterEnabled,
  ModelOption,
  GroupOption,
  PlaygroundSession,
} from '../types'

const MESSAGE_SAVE_DEBOUNCE_MS = 500

/**
 * Main state management hook for playground
 */
export function usePlaygroundState() {
  // Load initial state from localStorage
  const [config, setConfig] = useState<PlaygroundConfig>(
    getInitialPlaygroundConfig
  )

  const [parameterEnabled, setParameterEnabled] = useState<ParameterEnabled>(
    getInitialParameterEnabled
  )

  const [messages, setMessages] = useState<Message[]>([])
  const [sessions, setSessions] = useState<PlaygroundSession[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const [isLoadingMessages, setIsLoadingMessages] = useState(true)
  const messagesSaveTimerRef = useRef<number | null>(null)
  const latestMessagesRef = useRef<Message[]>(messages)
  const latestSessionsRef = useRef<PlaygroundSession[]>(sessions)
  const latestActiveSessionIdRef = useRef<string | null>(activeSessionId)
  const hasLoadedMessagesRef = useRef(false)
  const serverSyncEnabledRef = useRef(true)
  const serverSaveTimersRef = useRef<Map<string, number>>(new Map())
  const pendingServerMessagesRef = useRef<Map<string, Message[]>>(new Map())

  const [models, setModels] = useState<ModelOption[]>([])
  const [groups, setGroups] = useState<GroupOption[]>([])

  const syncSessionMessagesToServer = useCallback(
    (sessionId: string, messagesToSave: Message[]) => {
      if (!serverSyncEnabledRef.current) {
        return
      }

      pendingServerMessagesRef.current.set(sessionId, messagesToSave)
      const previousTimer = serverSaveTimersRef.current.get(sessionId)
      if (previousTimer !== undefined) {
        window.clearTimeout(previousTimer)
      }

      const timer = window.setTimeout(() => {
        serverSaveTimersRef.current.delete(sessionId)
        const pendingMessages = pendingServerMessagesRef.current.get(sessionId)
        pendingServerMessagesRef.current.delete(sessionId)
        if (!pendingMessages) {
          return
        }
        void savePlaygroundSessionMessages(sessionId, pendingMessages).catch(
          async () => {
            const session = latestSessionsRef.current.find(
              (item) => item.id === sessionId
            )
            if (!session) {
              return
            }
            try {
              await createPlaygroundSessionRemote(session)
              await savePlaygroundSessionMessages(sessionId, pendingMessages)
            } catch {
              /* empty */
            }
          }
        )
      }, MESSAGE_SAVE_DEBOUNCE_MS)
      serverSaveTimersRef.current.set(sessionId, timer)
    },
    []
  )

  const persistSessions = useCallback((sessionsToSave: PlaygroundSession[]) => {
    latestSessionsRef.current = sessionsToSave

    if (!hasLoadedMessagesRef.current) {
      return
    }

    if (messagesSaveTimerRef.current !== null) {
      window.clearTimeout(messagesSaveTimerRef.current)
    }

    messagesSaveTimerRef.current = window.setTimeout(() => {
      messagesSaveTimerRef.current = null
      saveSessions(latestSessionsRef.current)
    }, MESSAGE_SAVE_DEBOUNCE_MS)
  }, [])

  const persistMessages = useCallback(
    (messagesToSave: Message[]) => {
      latestMessagesRef.current = messagesToSave
      const currentSessionId = latestActiveSessionIdRef.current

      if (!hasLoadedMessagesRef.current) {
        return
      }

      if (!currentSessionId) {
        return
      }

      setSessions((prevSessions) => {
        const updatedSessions = updatePlaygroundSessionMessages(
          prevSessions,
          currentSessionId,
          messagesToSave
        )
        latestSessionsRef.current = updatedSessions
        persistSessions(updatedSessions)
        syncSessionMessagesToServer(currentSessionId, messagesToSave)
        return updatedSessions
      })
    },
    [persistSessions, syncSessionMessagesToServer]
  )

  useEffect(() => {
    let cancelled = false

    window.setTimeout(async () => {
      const storedSessions = loadSessions()
      const storedActiveSessionId = loadActiveSessionId()
      const legacyMessages = loadMessages() ?? []
      let sourceSessions = storedSessions
      let shouldCreateInitialRemoteSession = false

      try {
        const serverSessions = await getPlaygroundSessions()
        if (serverSessions.length > 0) {
          sourceSessions = serverSessions
        } else if (storedSessions?.length) {
          sourceSessions = await importPlaygroundSessions(storedSessions)
        } else if (legacyMessages.length > 0) {
          const legacyInitial = buildInitialPlaygroundSessions({
            storedSessions: null,
            activeSessionId: null,
            legacyMessages,
          })
          sourceSessions = await importPlaygroundSessions(
            legacyInitial.sessions
          )
        } else {
          shouldCreateInitialRemoteSession = true
        }
      } catch {
        serverSyncEnabledRef.current = false
      }

      const initial = buildInitialPlaygroundSessions({
        storedSessions: sourceSessions,
        activeSessionId: storedActiveSessionId,
        legacyMessages,
      })
      if (cancelled) {
        return
      }

      const activeSession =
        initial.sessions.find(
          (session) => session.id === initial.activeSessionId
        ) ?? initial.sessions[0]

      latestSessionsRef.current = initial.sessions
      latestActiveSessionIdRef.current = activeSession.id
      latestMessagesRef.current = activeSession.messages
      hasLoadedMessagesRef.current = true
      setSessions(initial.sessions)
      setActiveSessionId(activeSession.id)
      setMessages(activeSession.messages)
      saveSessions(initial.sessions)
      saveActiveSessionId(activeSession.id)
      setIsLoadingMessages(false)
      if (
        shouldCreateInitialRemoteSession &&
        serverSyncEnabledRef.current
      ) {
        void createPlaygroundSessionRemote(activeSession).catch(
          () => undefined
        )
      }
    }, 0)

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(
    () => () => {
      if (messagesSaveTimerRef.current !== null) {
        window.clearTimeout(messagesSaveTimerRef.current)
        saveSessions(latestSessionsRef.current)
      }
      serverSaveTimersRef.current.forEach((timer, sessionId) => {
        window.clearTimeout(timer)
        const pendingMessages = pendingServerMessagesRef.current.get(sessionId)
        if (pendingMessages) {
          void savePlaygroundSessionMessages(sessionId, pendingMessages).catch(
            () => undefined
          )
        }
      })
      serverSaveTimersRef.current.clear()
      pendingServerMessagesRef.current.clear()
    },
    []
  )

  useEffect(
    () =>
      imageGenerationTaskManager.subscribe((snapshot) => {
        if (!snapshot.sessions) {
          return
        }

        latestSessionsRef.current = snapshot.sessions
        setSessions(snapshot.sessions)

        const currentSessionId = latestActiveSessionIdRef.current
        if (!currentSessionId) {
          return
        }
        const activeSession = snapshot.sessions.find(
          (session) => session.id === currentSessionId
        )
        if (!activeSession) {
          return
        }

        latestMessagesRef.current = activeSession.messages
        setMessages(activeSession.messages)
        syncSessionMessagesToServer(currentSessionId, activeSession.messages)
      }),
    [syncSessionMessagesToServer]
  )

  // Update config with automatic save
  const updateConfig = useCallback(
    <K extends keyof PlaygroundConfig>(key: K, value: PlaygroundConfig[K]) => {
      setConfig((prev) => {
        const updated = { ...prev, [key]: value }
        saveConfig(updated)
        return updated
      })
    },
    []
  )

  // Update parameter enabled with automatic save
  const updateParameterEnabled = useCallback(
    (key: keyof ParameterEnabled, value: boolean) => {
      setParameterEnabled((prev) => {
        const updated = { ...prev, [key]: value }
        saveParameterEnabled(updated)
        return updated
      })
    },
    []
  )

  // Update messages with automatic save
  const updateMessages = useCallback(
    (updater: MessageStateUpdater) => {
      setMessages((prev) => {
        const newMessages = applyMessageStateUpdate(prev, updater)
        persistMessages(newMessages)
        return newMessages
      })
    },
    [persistMessages]
  )

  const commitActiveSessionMessages = useCallback(
    (messagesToSave: Message[], titleContent?: string) => {
      const currentSessionId = latestActiveSessionIdRef.current
      if (!currentSessionId) {
        return null
      }

      let baseSessions = latestSessionsRef.current
      const currentSession = baseSessions.find(
        (session) => session.id === currentSessionId
      )

      if (
        titleContent &&
        currentSession?.title === DEFAULT_PLAYGROUND_SESSION_TITLE &&
        currentSession.messages.length === 0
      ) {
        baseSessions = renamePlaygroundSession(
          baseSessions,
          currentSessionId,
          getSessionTitleFromMessage(titleContent)
        )
      }

      const updatedSessions = updatePlaygroundSessionMessages(
        baseSessions,
        currentSessionId,
        messagesToSave
      )

      latestMessagesRef.current = messagesToSave
      latestSessionsRef.current = updatedSessions
      setMessages(messagesToSave)
      setSessions(updatedSessions)
      persistSessions(updatedSessions)
      syncSessionMessagesToServer(currentSessionId, messagesToSave)

      return {
        sessionId: currentSessionId,
        sessions: updatedSessions,
      }
    },
    [persistSessions, syncSessionMessagesToServer]
  )

  // Clear all messages
  const clearMessages = useCallback(() => {
    updateMessages([])
  }, [updateMessages])

  const createSession = useCallback(() => {
    const session = createPlaygroundSession()
    setSessions((prevSessions) => {
      const updatedSessions = [session, ...prevSessions]
      latestSessionsRef.current = updatedSessions
      persistSessions(updatedSessions)
      return updatedSessions
    })
    latestActiveSessionIdRef.current = session.id
    latestMessagesRef.current = []
    setActiveSessionId(session.id)
    setMessages([])
    saveActiveSessionId(session.id)
    if (serverSyncEnabledRef.current) {
      void createPlaygroundSessionRemote(session).catch(() => undefined)
    }
  }, [persistSessions])

  const selectSession = useCallback((sessionId: string) => {
    const session = latestSessionsRef.current.find(
      (item) => item.id === sessionId
    )
    if (!session) {
      return
    }

    latestActiveSessionIdRef.current = sessionId
    latestMessagesRef.current = session.messages
    setActiveSessionId(sessionId)
    setMessages(session.messages)
    saveActiveSessionId(sessionId)
  }, [])

  const renameSession = useCallback(
    (sessionId: string, title: string) => {
      setSessions((prevSessions) => {
        const updatedSessions = renamePlaygroundSession(
          prevSessions,
          sessionId,
          title
        )
        latestSessionsRef.current = updatedSessions
        persistSessions(updatedSessions)
        return updatedSessions
      })
      if (serverSyncEnabledRef.current) {
        void renamePlaygroundSessionRemote(sessionId, title).catch(
          () => undefined
        )
      }
    },
    [persistSessions]
  )

  const deleteSession = useCallback(
    (sessionId: string) => {
      const result = deletePlaygroundSession(latestSessionsRef.current, sessionId)
      const activeSession = result.sessions.find(
        (session) => session.id === result.activeSessionId
      )

      latestSessionsRef.current = result.sessions
      latestActiveSessionIdRef.current = result.activeSessionId
      latestMessagesRef.current = activeSession?.messages ?? []
      setSessions(result.sessions)
      setActiveSessionId(result.activeSessionId)
      setMessages(activeSession?.messages ?? [])
      saveActiveSessionId(result.activeSessionId)
      persistSessions(result.sessions)
      if (serverSyncEnabledRef.current) {
        void deletePlaygroundSessionRemote(sessionId).catch(() => undefined)
      }
    },
    [persistSessions]
  )

  const renameActiveSessionFromMessage = useCallback((content: string) => {
    const currentSessionId = latestActiveSessionIdRef.current
    if (!currentSessionId) {
      return
    }

    setSessions((prevSessions) => {
      const currentSession = prevSessions.find(
        (session) => session.id === currentSessionId
      )
      if (
        !currentSession ||
        currentSession.title !== DEFAULT_PLAYGROUND_SESSION_TITLE ||
        currentSession.messages.length > 0
      ) {
        return prevSessions
      }

      const updatedSessions = renamePlaygroundSession(
        prevSessions,
        currentSessionId,
        getSessionTitleFromMessage(content)
      )
      latestSessionsRef.current = updatedSessions
      persistSessions(updatedSessions)
      return updatedSessions
    })
  }, [persistSessions])

  // Reset config to defaults
  const resetConfig = useCallback(() => {
    setConfig(DEFAULT_CONFIG)
    setParameterEnabled(DEFAULT_PARAMETER_ENABLED)
    saveConfig(DEFAULT_CONFIG)
    saveParameterEnabled(DEFAULT_PARAMETER_ENABLED)
  }, [])

  return {
    // State
    config,
    parameterEnabled,
    messages,
    sessions,
    activeSessionId,
    isLoadingMessages,
    models,
    groups,

    // Setters
    setModels,
    setGroups,

    // Actions
    updateConfig,
    updateParameterEnabled,
    updateMessages,
    commitActiveSessionMessages,
    clearMessages,
    createSession,
    selectSession,
    renameSession,
    deleteSession,
    renameActiveSessionFromMessage,
    resetConfig,
  }
}
