import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES } from '../../constants.ts'
import type { Message, PlaygroundSession } from '../../types.ts'
import {
  buildInitialPlaygroundSessions,
  createPlaygroundSession,
  deletePlaygroundSession,
  renamePlaygroundSession,
  updatePlaygroundSessionMessages,
} from './playground-session-utils.ts'

function message(content: string): Message {
  return {
    key: `message-${content}`,
    from: MESSAGE_ROLES.USER,
    versions: [{ id: `version-${content}`, content }],
    createdAt: 1000,
  }
}

describe('playground sessions', () => {
  test('migrates old messages into a default session', () => {
    const oldMessages = [message('hello')]
    const result = buildInitialPlaygroundSessions({
      storedSessions: null,
      activeSessionId: null,
      legacyMessages: oldMessages,
      now: 1700000000000,
      id: () => 'session-1',
    })

    assert.equal(result.activeSessionId, 'session-1')
    assert.equal(result.sessions.length, 1)
    assert.equal(result.sessions[0].title, 'New conversation')
    assert.deepEqual(result.sessions[0].messages, oldMessages)
  })

  test('keeps a valid active session and sorts sessions by update time', () => {
    const older: PlaygroundSession = {
      id: 'older',
      title: 'Older',
      messages: [],
      createdAt: 1000,
      updatedAt: 1000,
    }
    const newer: PlaygroundSession = {
      id: 'newer',
      title: 'Newer',
      messages: [],
      createdAt: 2000,
      updatedAt: 3000,
    }

    const result = buildInitialPlaygroundSessions({
      storedSessions: [older, newer],
      activeSessionId: 'older',
      legacyMessages: [],
      now: 4000,
      id: () => 'unused',
    })

    assert.equal(result.activeSessionId, 'older')
    assert.deepEqual(
      result.sessions.map((session) => session.id),
      ['newer', 'older']
    )
  })

  test('creates, renames, updates, and deletes sessions predictably', () => {
    const initial = createPlaygroundSession({
      id: 'initial',
      now: 1000,
      title: 'Initial',
    })
    const next = createPlaygroundSession({
      id: 'next',
      now: 2000,
      title: 'Next',
    })

    const renamed = renamePlaygroundSession([initial, next], 'initial', '  ')
    assert.equal(
      renamed.find((session) => session.id === 'initial')?.title,
      'Initial'
    )

    const renamedWithValue = renamePlaygroundSession(
      [initial, next],
      'initial',
      'Renamed'
    )
    assert.equal(
      renamedWithValue.find((session) => session.id === 'initial')?.title,
      'Renamed'
    )

    const updated = updatePlaygroundSessionMessages(
      renamedWithValue,
      'initial',
      [message('updated')],
      3000
    )
    assert.equal(updated[0].id, 'initial')
    assert.equal(updated[0].updatedAt, 3000)
    assert.equal(updated[0].messages.length, 1)

    const deleted = deletePlaygroundSession(updated, 'initial', {
      id: () => 'fallback',
      now: 4000,
    })
    assert.equal(deleted.activeSessionId, 'next')
    assert.deepEqual(
      deleted.sessions.map((session) => session.id),
      ['next']
    )

    const fallback = deletePlaygroundSession(deleted.sessions, 'next', {
      id: () => 'fallback',
      now: 5000,
    })
    assert.equal(fallback.activeSessionId, 'fallback')
    assert.equal(fallback.sessions[0].title, 'New conversation')
  })
})
