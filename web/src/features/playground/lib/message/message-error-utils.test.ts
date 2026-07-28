import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants.ts'
import type { Message } from '../../types.ts'
import {
  getMessageErrorState,
  RESPONSE_NOT_COMPLETED_TITLE,
  RETRYABLE_RESPONSE_ERROR_CONTENT,
  isErrorMessage,
} from './message-error-utils.ts'

describe('message error utilities', () => {
  test('does not render image generation messages through the error card', () => {
    const message: Message = {
      key: 'assistant-1',
      from: MESSAGE_ROLES.ASSISTANT,
      mode: 'image',
      status: MESSAGE_STATUS.ERROR,
      versions: [
        {
          id: 'version-1',
          content: 'Request error occurred: Request failed with status code 524',
        },
      ],
    }

    assert.equal(isErrorMessage(message), false)
  })

  test('does not render legacy 524 image failures through the error card', () => {
    const message: Message = {
      key: 'assistant-legacy',
      from: MESSAGE_ROLES.ASSISTANT,
      status: MESSAGE_STATUS.ERROR,
      versions: [
        {
          id: 'version-1',
          content: 'Request error occurred: Request failed with status code 524',
        },
      ],
    }

    assert.equal(isErrorMessage(message), false)
  })

  test('does not render legacy 524 failures with missing role through the error card', () => {
    const message = {
      key: 'assistant-missing-role',
      status: MESSAGE_STATUS.ERROR,
      versions: [
        {
          id: 'version-1',
          content: 'Request error occurred: Request failed with status code 524',
        },
      ],
    } as Message

    assert.equal(isErrorMessage(message), false)
  })

  test('keeps regular chat errors visible', () => {
    const message: Message = {
      key: 'assistant-1',
      from: MESSAGE_ROLES.ASSISTANT,
      status: MESSAGE_STATUS.ERROR,
      versions: [{ id: 'version-1', content: 'Request failed' }],
    }

    assert.equal(isErrorMessage(message), true)
  })

  test('uses a neutral retry presentation for generic playground errors', () => {
    const message: Message = {
      key: 'assistant-generic',
      from: MESSAGE_ROLES.ASSISTANT,
      status: MESSAGE_STATUS.ERROR,
      versions: [{ id: 'version-1', content: 'Model is unavailable' }],
    }

    const state = getMessageErrorState(message, false)

    assert.equal(state?.kind, 'generic')
    assert.equal(state?.tone, 'neutral')
    assert.equal(state?.title, RESPONSE_NOT_COMPLETED_TITLE)
    assert.equal(state?.content, 'Model is unavailable')
  })

  test('hides raw request status details in generic error content', () => {
    const message: Message = {
      key: 'assistant-request-error',
      from: MESSAGE_ROLES.ASSISTANT,
      status: MESSAGE_STATUS.ERROR,
      versions: [
        {
          id: 'version-1',
          content: 'AxiosError: Request failed with status code 400',
        },
      ],
    }

    const state = getMessageErrorState(message, false)

    assert.equal(state?.content, RETRYABLE_RESPONSE_ERROR_CONTENT)
    assert.doesNotMatch(state?.content ?? '', /Request failed|status code|400/)
  })
})
