import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants.ts'
import type { ImageGenerationRequest, PlaygroundSession } from '../../types.ts'
import { createImageGenerationTaskManager } from './image-generation-task-manager.ts'

describe('image generation task manager', () => {
  test('writes completed image result to the target session without subscribers', async () => {
    const sessions: PlaygroundSession[] = [
      {
        id: 'session-1',
        title: 'Image session',
        createdAt: 1000,
        updatedAt: 1000,
        messages: [
          {
            key: 'user-1',
            from: MESSAGE_ROLES.USER,
            mode: 'image',
            versions: [{ id: 'user-version', content: 'cute cat' }],
          },
          {
            key: 'assistant-1',
            from: MESSAGE_ROLES.ASSISTANT,
            mode: 'image',
            status: MESSAGE_STATUS.LOADING,
            versions: [{ id: 'assistant-version', content: '' }],
          },
        ],
      },
    ]
    const savedSessions: PlaygroundSession[][] = []
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000,
      getSessions: () => sessions,
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      requestImage: async () => ({
        data: [{ url: 'https://example.com/cat.png' }],
      }),
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      size: '1024x1024',
      files: [],
      sessionMessages: sessions[0].messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[1]
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'complete')
    assert.equal(finalMessage?.imageGeneration?.size, '1024x1024')
    assert.match(
      finalMessage?.versions[0].content ?? '',
      /!\[Generated image 1\]\(https:\/\/example.com\/cat.png\)/
    )
  })

  test('waits for the pending message to persist before requesting an image', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    let resolvePendingPersistence: (() => void) | undefined
    let requestCount = 0
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000,
      getSessions: () => [session],
      saveSessionMessages: () =>
        new Promise<void>((resolve) => {
          resolvePendingPersistence = resolve
        }),
      requestImage: async () => {
        requestCount += 1
        return { data: [{ url: 'https://example.com/cat.png' }] }
      },
    })

    const task = manager.start({
      sessionId: session.id,
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      sessions: [session],
      sessionMessages: session.messages,
    })

    await Promise.resolve()
    assert.equal(requestCount, 0)

    resolvePendingPersistence?.()
    await task.done

    assert.equal(requestCount, 1)
  })

  test('marks the image retryable without requesting it when pending persistence fails', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    let currentSessions = [session]
    let saveCount = 0
    let requestCount = 0
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000,
      getSessions: () => currentSessions,
      saveSessions: (sessions) => {
        currentSessions = sessions
      },
      saveSessionMessages: () => {
        saveCount += 1
        if (saveCount === 1) {
          return Promise.reject(new Error('pending persistence failed'))
        }
        return Promise.resolve()
      },
      requestImage: async () => {
        requestCount += 1
        return { data: [{ url: 'https://example.com/cat.png' }] }
      },
    })

    await manager.start({
      sessionId: session.id,
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      sessions: currentSessions,
      sessionMessages: session.messages,
    }).done

    assert.equal(requestCount, 0)
    assert.equal(
      currentSessions[0].messages[0].imageGeneration?.status,
      'retryable'
    )
  })

  test('runs image generation independently across multiple sessions', async () => {
    const firstSession: PlaygroundSession = {
      id: 'session-1',
      title: 'First image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version-1', content: '' }],
        },
      ],
    }
    const secondSession: PlaygroundSession = {
      id: 'session-2',
      title: 'Second image session',
      createdAt: 3000,
      updatedAt: 3000,
      messages: [
        {
          key: 'assistant-2',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version-2', content: '' }],
        },
      ],
    }
    let currentSessions = [firstSession]
    let taskSequence = 0
    const resolveRequests = new Map<
      string,
      (response: { data: Array<{ url: string }> }) => void
    >()
    const manager = createImageGenerationTaskManager({
      id: () => `task-${++taskSequence}`,
      now: () => 4000 + taskSequence,
      getSessions: () => currentSessions,
      saveSessions: (nextSessions) => {
        currentSessions = nextSessions
      },
      requestImage: ({ payload }) =>
        new Promise((resolve) => {
          assert.ok(payload.session_id)
          resolveRequests.set(payload.session_id, resolve)
        }),
    })

    const firstTask = manager.start({
      sessionId: firstSession.id,
      assistantMessageKey: 'assistant-1',
      prompt: 'first image',
      model: 'gpt-image-1',
      sessions: currentSessions,
      sessionMessages: firstSession.messages,
    })

    currentSessions = [secondSession, ...currentSessions]
    manager.setSessionsSnapshot(currentSessions)
    const secondTask = manager.start({
      sessionId: secondSession.id,
      assistantMessageKey: 'assistant-2',
      prompt: 'second image',
      model: 'gpt-image-1',
      sessions: currentSessions,
      sessionMessages: secondSession.messages,
    })

    assert.deepEqual(manager.getSnapshot().activeSessionIds.sort(), [
      'session-1',
      'session-2',
    ])

    resolveRequests.get('session-2')?.({
      data: [{ url: 'https://example.com/second.png' }],
    })
    await secondTask.done

    assert.deepEqual(manager.getSnapshot().activeSessionIds, ['session-1'])
    assert.match(
      currentSessions.find((session) => session.id === 'session-2')?.messages[0]
        .versions[0].content ?? '',
      /second\.png/
    )

    resolveRequests.get('session-1')?.({
      data: [{ url: 'https://example.com/first.png' }],
    })
    await firstTask.done

    assert.equal(manager.getSnapshot().activeSessionIds.length, 0)
    assert.equal(currentSessions.length, 2)
    assert.match(
      currentSessions.find((session) => session.id === 'session-1')?.messages[0]
        .versions[0].content ?? '',
      /first\.png/
    )
    assert.match(
      currentSessions.find((session) => session.id === 'session-2')?.messages[0]
        .versions[0].content ?? '',
      /second\.png/
    )
  })

  test('cancels image generation only for the selected session', async () => {
    const sessions: PlaygroundSession[] = ['session-1', 'session-2'].map(
      (sessionId, index) => ({
        id: sessionId,
        title: `Image session ${index + 1}`,
        createdAt: 1000 + index,
        updatedAt: 1000 + index,
        messages: [
          {
            key: `assistant-${index + 1}`,
            from: MESSAGE_ROLES.ASSISTANT,
            mode: 'image',
            status: MESSAGE_STATUS.LOADING,
            versions: [{ id: `assistant-version-${index + 1}`, content: '' }],
          },
        ],
      })
    )
    let currentSessions = sessions
    let taskSequence = 0
    const requestSignals = new Map<string, AbortSignal>()
    const resolveRequests = new Map<
      string,
      (response: { data: Array<{ url: string }> }) => void
    >()
    const manager = createImageGenerationTaskManager({
      id: () => `task-${++taskSequence}`,
      now: () => 2000 + taskSequence,
      getSessions: () => currentSessions,
      saveSessions: (nextSessions) => {
        currentSessions = nextSessions
      },
      requestImage: ({ payload, signal }) =>
        new Promise((resolve) => {
          assert.ok(payload.session_id)
          requestSignals.set(payload.session_id, signal)
          resolveRequests.set(payload.session_id, resolve)
        }),
    })

    const firstTask = manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'first image',
      model: 'gpt-image-1',
      sessions: currentSessions,
      sessionMessages: sessions[0].messages,
    })
    const secondTask = manager.start({
      sessionId: 'session-2',
      assistantMessageKey: 'assistant-2',
      prompt: 'second image',
      model: 'gpt-image-1',
      sessions: currentSessions,
      sessionMessages: sessions[1].messages,
    })

    manager.cancelSession('session-1')
    await firstTask.done

    assert.equal(requestSignals.get('session-1')?.aborted, true)
    assert.equal(requestSignals.get('session-2')?.aborted, false)
    assert.equal(manager.hasActiveTaskForSession('session-1'), false)
    assert.equal(manager.hasActiveTaskForSession('session-2'), true)

    resolveRequests.get('session-2')?.({
      data: [{ url: 'https://example.com/second.png' }],
    })
    await secondTask.done

    assert.equal(
      currentSessions.find((session) => session.id === 'session-1')?.messages[0]
        .imageGeneration?.status,
      'cancelled'
    )
    assert.equal(
      currentSessions.find((session) => session.id === 'session-2')?.messages[0]
        .imageGeneration?.status,
      'complete'
    )
  })

  test('omits auto size and sends explicit image size', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const payloads: ImageGenerationRequest[] = []
    const manager = createImageGenerationTaskManager({
      id: () => `task-${payloads.length + 1}`,
      now: () => 2000,
      getSessions: () => [session],
      saveSessions: () => undefined,
      requestImage: async ({ payload }) => {
        payloads.push(payload)
        return { data: [{ url: 'https://example.com/image.png' }] }
      },
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'auto size',
      model: 'gpt-image-1',
      group: 'default',
      size: 'auto',
      sessionMessages: session.messages,
    }).done

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'square size',
      model: 'gpt-image-1',
      group: 'default',
      size: '1024x1024',
      sessionMessages: session.messages,
    }).done

    assert.equal(payloads[0].size, undefined)
    assert.equal(payloads[1].size, '1024x1024')
    assert.equal(payloads[0].stream, true)
    assert.equal(payloads[0].partial_images, 1)
    assert.equal(payloads[1].stream, true)
    assert.equal(payloads[1].partial_images, 1)
  })

  test('keeps response errors out of the image error UI state', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const savedSessions: PlaygroundSession[][] = []
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000,
      getSessions: () => [session],
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      requestImage: async () => {
        const error = new Error('Network Error') as Error & {
          response?: {
            status?: number
            data?: { error?: { message?: string }; message?: string }
          }
        }
        error.response = {
          status: 524,
          data: {
            error: {
              message:
                'The origin web server did not return a complete response',
            },
          },
        }
        throw error
      },
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      sessionMessages: session.messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[0]
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'retryable')
    assert.match(
      finalMessage?.versions[0].content ?? '',
      /Image generation did not finish\. You can retry\./
    )
    assert.doesNotMatch(
      finalMessage?.versions[0].content ?? '',
      /Request error occurred|status code 524|HTTP 524/
    )
    assert.doesNotMatch(
      finalMessage?.versions[0].content ?? '',
      /Network Error/
    )
    assert.equal(finalMessage?.imageGeneration?.error, undefined)
  })

  test('recovers completed image from persisted session after network error', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const savedSessions: PlaygroundSession[][] = []
    let recoverAttempts = 0
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000 + recoverAttempts,
      getSessions: () => [session],
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      recoveryPollIntervalMs: 0,
      recoveryTimeoutMs: 10,
      requestImage: async () => {
        throw new Error('Network Error')
      },
      recoverImageMessage: async () => {
        recoverAttempts += 1
        if (recoverAttempts === 1) {
          return null
        }
        return {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.COMPLETE,
          imageGeneration: {
            taskId: 'server-task',
            prompt: 'cute cat',
            size: 'auto',
            status: 'complete',
            startedAt: 1000,
            completedAt: 3000,
          },
          versions: [
            {
              id: 'server-version',
              content:
                '![Generated image 1](/api/playground/files/pgf_server/content)',
            },
          ],
        }
      },
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      sessionMessages: session.messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[0]
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'complete')
    assert.match(
      finalMessage?.versions[0].content ?? '',
      /\/api\/playground\/files\/pgf_server\/content/
    )
    assert.doesNotMatch(
      finalMessage?.versions[0].content ?? '',
      /Network Error/
    )
  })

  test('polls persisted session after async image task is accepted', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const savedSessions: PlaygroundSession[][] = []
    let recoverAttempts = 0
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000 + recoverAttempts,
      getSessions: () => [session],
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      recoveryPollIntervalMs: 0,
      recoveryTimeoutMs: 10,
      requestImage: async () => ({
        status: 'pending',
      }),
      recoverImageMessage: async () => {
        recoverAttempts += 1
        if (recoverAttempts === 1) {
          return {
            key: 'assistant-1',
            from: MESSAGE_ROLES.ASSISTANT,
            mode: 'image',
            status: MESSAGE_STATUS.LOADING,
            imageGeneration: {
              taskId: 'server-task',
              prompt: 'cute cat',
              size: 'auto',
              status: 'pending',
            },
            versions: [{ id: 'server-version', content: '' }],
          }
        }
        return {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.COMPLETE,
          imageGeneration: {
            taskId: 'server-task',
            prompt: 'cute cat',
            size: 'auto',
            status: 'complete',
          },
          versions: [
            {
              id: 'server-version',
              content:
                '![Generated image 1](/api/playground/files/pgf_async/content)',
            },
          ],
        }
      },
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      sessionMessages: session.messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[0]
    assert.equal(recoverAttempts, 2)
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'complete')
    assert.match(
      finalMessage?.versions[0].content ?? '',
      /\/api\/playground\/files\/pgf_async\/content/
    )
  })

  test('keeps polling accepted async tasks for the backend timeout window by default', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const savedSessions: PlaygroundSession[][] = []
    let recoverAttempts = 0
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000 + recoverAttempts * 240_000,
      getSessions: () => [session],
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      recoveryPollIntervalMs: 0,
      requestImage: async () => ({
        status: 'pending',
      }),
      recoverImageMessage: async () => {
        recoverAttempts += 1
        if (recoverAttempts < 3) {
          return {
            key: 'assistant-1',
            from: MESSAGE_ROLES.ASSISTANT,
            mode: 'image',
            status: MESSAGE_STATUS.LOADING,
            imageGeneration: {
              taskId: 'server-task',
              prompt: 'cute cat',
              size: 'auto',
              status: 'pending',
            },
            versions: [{ id: 'server-version', content: '' }],
          }
        }
        return {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.COMPLETE,
          imageGeneration: {
            taskId: 'server-task',
            prompt: 'cute cat',
            size: 'auto',
            status: 'complete',
          },
          versions: [
            {
              id: 'server-version',
              content:
                '![Generated image 1](/api/playground/files/pgf_long/content)',
            },
          ],
        }
      },
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      sessionMessages: session.messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[0]
    assert.equal(recoverAttempts, 3)
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'complete')
    assert.match(
      finalMessage?.versions[0].content ?? '',
      /\/api\/playground\/files\/pgf_long\/content/
    )
  })

  test('marks an image request as failed when the total task time expires', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const savedSessions: PlaygroundSession[][] = []
    let requestSignal: AbortSignal | null = null
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000,
      getSessions: () => [session],
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      taskTimeoutMs: 0,
      requestImage: ({ signal }) => {
        requestSignal = signal
        return new Promise(() => undefined)
      },
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      sessionMessages: session.messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[0]
    assert.equal((requestSignal as AbortSignal | null)?.aborted, true)
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'retryable')
    assert.match(
      finalMessage?.versions[0].content ?? '',
      /Image generation timed out\. You can retry\./
    )
  })

  test('keeps retryable server image failures out of the error UI state', async () => {
    const session: PlaygroundSession = {
      id: 'session-1',
      title: 'Image session',
      createdAt: 1000,
      updatedAt: 1000,
      messages: [
        {
          key: 'assistant-1',
          from: MESSAGE_ROLES.ASSISTANT,
          mode: 'image',
          status: MESSAGE_STATUS.LOADING,
          versions: [{ id: 'assistant-version', content: '' }],
        },
      ],
    }
    const savedSessions: PlaygroundSession[][] = []
    const manager = createImageGenerationTaskManager({
      id: () => 'task-1',
      now: () => 2000,
      getSessions: () => [session],
      saveSessions: (nextSessions) => {
        savedSessions.push(nextSessions)
      },
      recoveryPollIntervalMs: 0,
      recoveryTimeoutMs: 10,
      requestImage: async () => ({
        status: 'pending',
      }),
      recoverImageMessage: async () => ({
        key: 'assistant-1',
        from: MESSAGE_ROLES.ASSISTANT,
        mode: 'image',
        status: MESSAGE_STATUS.COMPLETE,
        imageGeneration: {
          taskId: 'server-task',
          prompt: 'cute cat',
          size: 'auto',
          status: 'retryable',
        },
        versions: [
          {
            id: 'server-version',
            content: '图片生成未完成，可以点击重新生成。',
          },
        ],
      }),
    })

    await manager.start({
      sessionId: 'session-1',
      assistantMessageKey: 'assistant-1',
      prompt: 'cute cat',
      model: 'gpt-image-1',
      group: 'default',
      sessionMessages: session.messages,
    }).done

    const finalMessage = savedSessions.at(-1)?.[0].messages[0]
    assert.equal(finalMessage?.status, MESSAGE_STATUS.COMPLETE)
    assert.equal(finalMessage?.imageGeneration?.status, 'retryable')
    assert.match(finalMessage?.versions[0].content ?? '', /图片生成未完成/)
    assert.doesNotMatch(
      finalMessage?.versions[0].content ?? '',
      /Request error occurred|status code 524/
    )
  })
})
