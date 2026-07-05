import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const imagePreviewDialogSource = readFileSync(
  new URL('./image-preview-dialog.tsx', import.meta.url),
  'utf8'
)

const playgroundImagePreviewDialogSource = readFileSync(
  new URL(
    '../../features/playground/components/message/playground-image-preview-dialog.tsx',
    import.meta.url
  ),
  'utf8'
)

const playgroundFileImageSource = readFileSync(
  new URL(
    '../../features/playground/components/message/playground-file-image.tsx',
    import.meta.url
  ),
  'utf8'
)

describe('ImagePreviewDialog viewer layout', () => {
  test('uses a full-window image viewer layout instead of a narrow document panel', () => {
    assert.match(imagePreviewDialogSource, /showCloseButton=\{false\}/)
    assert.match(imagePreviewDialogSource, /bg-black\/90/)
    assert.match(imagePreviewDialogSource, /maxHeight: `calc\(\(100svh - 8rem\) \* \$\{zoom\}\)`/)
    assert.match(imagePreviewDialogSource, /maxWidth: `calc\(\(100vw - 4rem\) \* \$\{zoom\}\)`/)
    assert.doesNotMatch(imagePreviewDialogSource, /grid-rows-\[auto_minmax/)
    assert.doesNotMatch(imagePreviewDialogSource, /RotateCcw/)
    assert.doesNotMatch(imagePreviewDialogSource, /scale\(\$\{zoom\}\)/)
    assert.doesNotMatch(imagePreviewDialogSource, /width: `\$\{zoom \* 100\}%`/)
  })

  test('closes from the empty viewer backdrop but not from image controls', () => {
    assert.match(imagePreviewDialogSource, /handleBackdropPointerDown/)
    assert.match(imagePreviewDialogSource, /event\.target !== event\.currentTarget/)
    assert.match(imagePreviewDialogSource, /onOpenChange\(false\)/)
    assert.match(imagePreviewDialogSource, /onPointerDown=\{handleBackdropPointerDown\}/)
    assert.match(imagePreviewDialogSource, /pointer-events-auto/)
  })

  test('uses explicit zoom out, reset percentage, and zoom in controls', () => {
    assert.match(imagePreviewDialogSource, /Zoom out/)
    assert.match(imagePreviewDialogSource, /Reset zoom to 100%/)
    assert.match(imagePreviewDialogSource, /Zoom in/)
    assert.match(imagePreviewDialogSource, /100%/)
    assert.match(imagePreviewDialogSource, /disabled=\{zoom === DEFAULT_IMAGE_PREVIEW_ZOOM\}/)
  })

  test('reuses the shared image preview dialog for playground file images', () => {
    assert.match(playgroundImagePreviewDialogSource, /ImagePreviewDialog/)
    assert.doesNotMatch(playgroundImagePreviewDialogSource, /DialogContent/)
    assert.doesNotMatch(
      playgroundImagePreviewDialogSource,
      /grid-rows-\[auto_minmax/
    )
    assert.doesNotMatch(playgroundFileImageSource, /mx-auto/)
    assert.match(playgroundFileImageSource, /max-h-\[min\(30svh,260px\)\]/)
    assert.match(playgroundFileImageSource, /max-w-\[min\(100%,15rem\)\]/)
    assert.match(playgroundFileImageSource, /h-auto/)
    assert.match(playgroundFileImageSource, /w-auto/)
    assert.match(playgroundFileImageSource, /object-contain/)
  })
})
