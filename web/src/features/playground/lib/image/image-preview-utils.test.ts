import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  DEFAULT_IMAGE_PREVIEW_ZOOM,
  MAX_IMAGE_PREVIEW_ZOOM,
  MIN_IMAGE_PREVIEW_ZOOM,
  getNextImagePreviewZoom,
  getPreviousImagePreviewZoom,
  getResetImagePreviewZoom,
} from './image-preview-utils.ts'

describe('image preview utils', () => {
  test('zooms in and clamps to the maximum value', () => {
    assert.equal(getNextImagePreviewZoom(1), 1.25)
    assert.equal(
      getNextImagePreviewZoom(MAX_IMAGE_PREVIEW_ZOOM),
      MAX_IMAGE_PREVIEW_ZOOM
    )
  })

  test('zooms out and clamps to the minimum value', () => {
    assert.equal(getPreviousImagePreviewZoom(1), 0.75)
    assert.equal(
      getPreviousImagePreviewZoom(MIN_IMAGE_PREVIEW_ZOOM),
      MIN_IMAGE_PREVIEW_ZOOM
    )
  })

  test('resets zoom to the default value', () => {
    assert.equal(getResetImagePreviewZoom(), DEFAULT_IMAGE_PREVIEW_ZOOM)
  })
})
