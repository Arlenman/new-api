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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'
import { getLoginAnnouncements } from './login-announcements.ts'

const loginAnnouncementsSource = readFileSync(
  new URL('../components/login-announcements.tsx', import.meta.url),
  'utf8'
)

const signInSource = readFileSync(new URL('../index.tsx', import.meta.url), {
  encoding: 'utf8',
})

describe('getLoginAnnouncements', () => {
  test('returns configured announcements when the panel is enabled', () => {
    const announcements = getLoginAnnouncements({
      announcements_enabled: true,
      announcements: [
        {
          id: 1,
          content: 'Maintenance starts at 22:00',
          publishDate: '2026-06-14T12:00:00Z',
          type: 'warning',
          extra: 'Expected duration: 15 minutes',
        },
      ],
    })

    assert.deepEqual(announcements, [
      {
        id: 1,
        content: 'Maintenance starts at 22:00',
        publishDate: '2026-06-14T12:00:00Z',
        type: 'warning',
        extra: 'Expected duration: 15 minutes',
      },
    ])
  })

  test('does not return announcements when the panel is disabled', () => {
    const announcements = getLoginAnnouncements({
      announcements_enabled: false,
      announcements: [{ content: 'Hidden announcement' }],
    })

    assert.deepEqual(announcements, [])
  })

  test('falls back to legacy notice when system announcements are disabled', () => {
    const announcements = getLoginAnnouncements({
      announcements_enabled: false,
      announcements: [{ content: 'Hidden announcement' }],
      notice: '<strong>Legacy notice</strong>',
    })

    assert.deepEqual(announcements, [
      {
        id: 0,
        content: '<strong>Legacy notice</strong>',
        type: 'default',
      },
    ])
  })

  test('ignores announcements without visible content', () => {
    const announcements = getLoginAnnouncements({
      announcements_enabled: true,
      announcements: [
        { content: '   ' },
        { extra: 'Missing content' },
        { content: 'Visible announcement' },
      ],
    })

    assert.deepEqual(announcements, [{ content: 'Visible announcement' }])
  })

  test('reads nested status data for cached status payloads', () => {
    const announcements = getLoginAnnouncements({
      data: {
        announcements_enabled: true,
        announcements: [{ content: 'Nested announcement' }],
      },
    })

    assert.deepEqual(announcements, [{ content: 'Nested announcement' }])
  })

  test('falls back to legacy notice content when no system announcements exist', () => {
    const announcements = getLoginAnnouncements({
      announcements_enabled: true,
      announcements: [],
      notice: '<strong>Legacy notice</strong>',
    })

    assert.deepEqual(announcements, [
      {
        id: 0,
        content: '<strong>Legacy notice</strong>',
        type: 'default',
      },
    ])
  })

  test('prefers system announcements over legacy notice content', () => {
    const announcements = getLoginAnnouncements({
      announcements_enabled: true,
      announcements: [{ content: 'System announcement' }],
      notice: 'Legacy notice',
    })

    assert.deepEqual(announcements, [{ content: 'System announcement' }])
  })

  test('login page places announcements beside the form on desktop', () => {
    assert.match(signInSource, /lg:grid-cols-\[/)
    assert.match(signInSource, /<LoginAnnouncements\s+className=/)
    assert.match(signInSource, /contentClassName=/)
  })

  test('login page vertically centers the announcement and form columns', () => {
    assert.equal(signInSource.includes('lg:items-start'), false)
    assert.match(signInSource, /lg:items-center/)
  })

  test('login announcements render without a heading row or leading status dot', () => {
    assert.equal(loginAnnouncementsSource.includes('AnnouncementDot'), false)
    assert.equal(loginAnnouncementsSource.includes('<Megaphone'), false)
    assert.equal(
      loginAnnouncementsSource.includes("<span>{t('Announcements')}</span>"),
      false
    )
  })
})
