import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  getEnabledCustomNavMenusForPlacement,
  getCustomNavMenusForPlacement,
  parseCustomNavMenus,
} from './nav-modules.ts'

describe('custom nav menus', () => {
  test('parses enabled menu items with placement flags', () => {
    const menus = parseCustomNavMenus(
      JSON.stringify([
        {
          id: 'docs',
          title: 'Docs',
          url: 'https://example.com/docs',
          enabled: true,
          placement: 'both',
          openInNewTab: true,
          requireAuth: false,
        },
        {
          id: 'support',
          title: 'Support',
          url: '/support',
          enabled: false,
          placement: 'sidebar',
          openInNewTab: false,
          requireAuth: true,
        },
      ])
    )

    assert.deepEqual(menus, [
      {
        id: 'docs',
        title: 'Docs',
        url: 'https://example.com/docs',
        enabled: true,
        placement: 'both',
        openInNewTab: true,
        requireAuth: false,
      },
      {
        id: 'support',
        title: 'Support',
        url: '/support',
        enabled: false,
        placement: 'sidebar',
        openInNewTab: false,
        requireAuth: true,
      },
    ])
  })

  test('filters menus by placement and authentication state', () => {
    const raw = JSON.stringify([
      {
        id: 'top',
        title: 'Top',
        url: '/top',
        enabled: true,
        placement: 'top',
      },
      {
        id: 'sidebar',
        title: 'Sidebar',
        url: '/sidebar',
        enabled: true,
        placement: 'sidebar',
      },
      {
        id: 'both',
        title: 'Both',
        url: '/both',
        enabled: true,
        placement: 'both',
        requireAuth: true,
      },
    ])

    assert.deepEqual(
      getCustomNavMenusForPlacement(raw, 'top', false).map((item) => item.id),
      ['top']
    )
    assert.deepEqual(
      getCustomNavMenusForPlacement(raw, 'top', true).map((item) => item.id),
      ['top', 'both']
    )
    assert.deepEqual(
      getCustomNavMenusForPlacement(raw, 'sidebar', true).map(
        (item) => item.id
      ),
      ['sidebar', 'both']
    )
  })

  test('keeps login-gated menus when only filtering by placement', () => {
    const raw = JSON.stringify([
      {
        id: 'image-workbench',
        title: '生图工作台',
        url: 'http://localhost:3030/',
        enabled: true,
        placement: 'sidebar',
        requireAuth: true,
      },
    ])

    assert.deepEqual(
      getEnabledCustomNavMenusForPlacement(raw, 'sidebar').map(
        (item) => item.id
      ),
      ['image-workbench']
    )
  })

  test('keeps external URLs independent from the new-tab option', () => {
    const menus = getCustomNavMenusForPlacement(
      JSON.stringify([
        {
          id: 'external',
          title: 'External',
          url: 'https://example.com',
          enabled: true,
          placement: 'top',
          openInNewTab: false,
        },
      ]),
      'top',
      false
    )

    assert.equal(menus[0]?.url, 'https://example.com')
    assert.equal(menus[0]?.openInNewTab, false)
  })

  test('parses localhost URLs for self-hosted tools', () => {
    const menus = parseCustomNavMenus(
      JSON.stringify([
        {
          id: 'image-workbench',
          title: '生图工作台',
          url: 'http://localhost:3030/',
          enabled: true,
          placement: 'sidebar',
          openInNewTab: true,
          requireAuth: true,
        },
      ])
    )

    assert.deepEqual(menus, [
      {
        id: 'image-workbench',
        title: '生图工作台',
        url: 'http://localhost:3030/',
        enabled: true,
        placement: 'sidebar',
        openInNewTab: true,
        requireAuth: true,
      },
    ])
  })

  test('ignores invalid records', () => {
    const menus = parseCustomNavMenus(
      JSON.stringify([
        {
          id: 'valid',
          title: 'Valid',
          url: '/valid',
          enabled: true,
          placement: 'top',
        },
        {
          id: '',
          title: 'Missing ID',
          url: '/missing-id',
          enabled: true,
          placement: 'top',
        },
        {
          id: 'bad-url',
          title: 'Bad URL',
          url: 'javascript:alert(1)',
          enabled: true,
          placement: 'top',
        },
        {
          id: 'bad-placement',
          title: 'Bad Placement',
          url: '/bad-placement',
          enabled: true,
          placement: 'footer',
        },
      ])
    )

    assert.deepEqual(
      menus.map((item) => ({
        id: item.id,
        title: item.title,
        url: item.url,
        placement: item.placement,
      })),
      [
        {
          id: 'valid',
          title: 'Valid',
          url: '/valid',
          placement: 'top',
        },
      ]
    )
  })
})
