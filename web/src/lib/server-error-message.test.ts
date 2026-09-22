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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { createInstance } from 'i18next'

import en from '../i18n/locales/en.json'
import fr from '../i18n/locales/fr.json'
import ja from '../i18n/locales/ja.json'
import ru from '../i18n/locales/ru.json'
import vi from '../i18n/locales/vi.json'
import zhTW from '../i18n/locales/zh-TW.json'
import zhCN from '../i18n/locales/zh.json'

import { getServerErrorMessageKey } from './server-error-message'

describe('server error message mapping', () => {
  test('maps the active-session limit to recovery instructions', () => {
    const message = getServerErrorMessageKey({ code: 'AUTH_SESSION_LIMIT' })

    assert.match(message ?? '', /Sign out other sessions/)
    assert.match(message ?? '', /reset your password/)
  })

  test('maps an Axios-shaped issuance limit to rolling-window guidance', () => {
    const message = getServerErrorMessageKey({
      response: { data: { code: 'AUTH_SESSION_ISSUANCE_LIMIT' } },
    })

    assert.match(message ?? '', /rolling window/)
    assert.equal(getServerErrorMessageKey({ code: 'UNKNOWN_CODE' }), null)
  })

  test('maps token tag timeouts in business and Axios errors to a translation key', () => {
    const message =
      'token tag analytics timed out, please narrow the time range'
    const expected =
      'Token tag analytics timed out. Please narrow the time range.'

    for (const payload of [
      { success: false, message },
      { response: { data: { message } } },
      { code: 'UNKNOWN_CODE', message },
    ]) {
      assert.equal(getServerErrorMessageKey(payload), expected)
    }
  })

  test('keeps known error codes authoritative when the timeout message is present', () => {
    assert.equal(
      getServerErrorMessageKey({
        code: 'TELEGRAM_BIND_DISABLED',
        message: 'token tag analytics timed out, please narrow the time range',
      }),
      'Telegram binding is disabled.'
    )
  })

  test('leaves unknown messages and malformed payloads unmapped', () => {
    for (const payload of [
      null,
      {},
      { message: 123 },
      { response: { data: null } },
      { message: 'another analytics request timed out' },
      {
        message:
          'token tag analytics timed out, please narrow the time range: raw detail',
      },
    ]) {
      assert.equal(getServerErrorMessageKey(payload), null)
    }
  })

  test('translates token tag timeout guidance in every supported language without fallback', async () => {
    const messageKey =
      'Token tag analytics timed out. Please narrow the time range.'
    const cases = [
      ['en', en, messageKey],
      ['zhCN', zhCN, '令牌标签统计超时，请缩小时间范围。'],
      ['zhTW', zhTW, '令牌標籤統計逾時，請縮小時間範圍。'],
      [
        'fr',
        fr,
        'L’analyse des balises de jetons a expiré. Veuillez réduire la plage de temps.',
      ],
      [
        'ru',
        ru,
        'Превышено время ожидания анализа тегов токенов. Пожалуйста, сократите временной диапазон.',
      ],
      [
        'ja',
        ja,
        'トークンタグの分析がタイムアウトしました。期間を絞り込んでください。',
      ],
      [
        'vi',
        vi,
        'Phân tích thẻ mã thông báo đã hết thời gian chờ. Vui lòng thu hẹp khoảng thời gian.',
      ],
    ] as const

    for (const [language, resource, expected] of cases) {
      const i18n = createInstance()
      await i18n.init({
        lng: language,
        fallbackLng: false,
        resources: { [language]: resource },
      })

      assert.ok(i18n.exists(messageKey), language)
      assert.equal(i18n.t(messageKey), expected, language)
    }
  })

  test('maps stable Telegram bind errors without exposing server text', () => {
    const expected = {
      TELEGRAM_BIND_DISABLED: 'Telegram binding is disabled.',
      TELEGRAM_BIND_INVALID_REQUEST:
        'The Telegram authorization request is invalid or expired.',
      TELEGRAM_BIND_FLOW_INVALID:
        'This Telegram binding request has expired or has already been used.',
      TELEGRAM_BIND_SESSION_INVALID:
        'The login session that started this Telegram binding is no longer valid.',
      TELEGRAM_BIND_ALREADY_BOUND: 'This Telegram account is already bound.',
      TELEGRAM_BIND_USER_DELETED: 'This user account no longer exists.',
      TELEGRAM_BIND_USER_DISABLED: 'This user account is disabled.',
      TELEGRAM_BIND_INTERNAL_ERROR:
        'Telegram binding failed. Please try again.',
    }

    for (const [code, message] of Object.entries(expected)) {
      assert.equal(getServerErrorMessageKey({ code }), message)
    }

    assert.equal(
      getServerErrorMessageKey({
        response: {
          data: { code: 'TELEGRAM_BIND_INTERNAL_ERROR', message: 'raw detail' },
        },
      }),
      expected.TELEGRAM_BIND_INTERNAL_ERROR
    )
  })
})
