import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const playgroundSource = readFileSync(
  new URL('../../index.tsx', import.meta.url),
  'utf8'
)

const playgroundChatSource = readFileSync(
  new URL('./playground-chat.tsx', import.meta.url),
  'utf8'
)

const playgroundInputSource = readFileSync(
  new URL('../input/playground-input.tsx', import.meta.url),
  'utf8'
)

const playgroundFileImageSource = readFileSync(
  new URL('../message/playground-file-image.tsx', import.meta.url),
  'utf8'
)

const playgroundMessageContentSource = readFileSync(
  new URL('../message/playground-message-content.tsx', import.meta.url),
  'utf8'
)

const imageGenerationProgressSource = readFileSync(
  new URL('../message/image-generation-progress.tsx', import.meta.url),
  'utf8'
)

const messageActionsSource = readFileSync(
  new URL('../message/message-actions.tsx', import.meta.url),
  'utf8'
)

const messageMetadataSource = readFileSync(
  new URL('../message/message-metadata.tsx', import.meta.url),
  'utf8'
)

const messageActionUtilsSource = readFileSync(
  new URL('../../lib/message/message-action-utils.ts', import.meta.url),
  'utf8'
)

const messageLayoutUtilsSource = readFileSync(
  new URL('../../lib/message/message-layout-utils.ts', import.meta.url),
  'utf8'
)

const messageStylesSource = readFileSync(
  new URL('../../lib/message/message-styles.ts', import.meta.url),
  'utf8'
)

describe('Playground wide layout', () => {
  test('uses a shared wide readable column instead of a narrow or unlimited column', () => {
    assert.doesNotMatch(playgroundSource, /max-w-4xl/)
    assert.doesNotMatch(playgroundChatSource, /max-w-4xl/)
    assert.doesNotMatch(playgroundInputSource, /md:pb-4/)
    assert.match(playgroundSource, /mx-auto w-full max-w-\[88rem\]/)
    assert.match(playgroundChatSource, /mx-auto w-full max-w-\[88rem\]/)
  })

  test('keeps generated images as contained left-aligned previews', () => {
    assert.match(playgroundChatSource, /isImageOnlyMarkdownContent/)
    assert.match(playgroundChatSource, /useCompactImageActions/)
    assert.match(playgroundChatSource, /alwaysShowActions && !useCompactImageActions/)
    assert.match(playgroundChatSource, /compactFloating=\{useCompactImageActions\}/)
    assert.match(playgroundChatSource, /py-1\.5/)
    assert.match(playgroundChatSource, /useCompactImageActions \? 'mt-0' : 'mt-1\.5'/)
    assert.match(messageActionsSource, /compactFloating/)
    assert.match(messageActionsSource, /!\s*compactFloating &&/)
    assert.match(messageActionsSource, /compactFloating \? '' : 'md:hidden'/)
    assert.match(messageActionsSource, /absolute top-1 right-1/)
    assert.match(messageActionsSource, /bg-background\/80 size-8 shadow-sm backdrop-blur/)
    assert.match(messageActionUtilsSource, /pointer-events-none opacity-0/)
    assert.match(messageActionUtilsSource, /group-hover:pointer-events-auto/)
    assert.match(messageMetadataSource, /compact\?: boolean/)
    assert.match(messageMetadataSource, /mt-0\.5 min-h-3 text-\[10px\]/)
    assert.doesNotMatch(playgroundMessageContentSource, /80rem/)
    assert.doesNotMatch(
      playgroundMessageContentSource,
      /group-\[\.is-assistant\]:!max-w-full/
    )
    assert.match(playgroundMessageContentSource, /group-\[\.is-assistant\]:!w-fit/)
    assert.doesNotMatch(playgroundMessageContentSource, /group-\[\.is-assistant\]:!mx-auto/)
    assert.match(playgroundMessageContentSource, /isImageOnlyMessage/)
    assert.match(playgroundMessageContentSource, /isImageOnlyMessage && 'relative'/)
    assert.match(playgroundMessageContentSource, /relative w-fit max-w-full/)
    assert.match(playgroundMessageContentSource, /compact=\{isImageOnlyMessage\}/)
    assert.doesNotMatch(playgroundMessageContentSource, /!items-center text-center/)
    assert.match(
      playgroundMessageContentSource,
      /gap-0 group-\[\.is-assistant\]:!w-fit group-\[\.is-assistant\]:!max-w-\[15rem\]/
    )
    assert.doesNotMatch(playgroundFileImageSource, /76vh/)
    assert.doesNotMatch(playgroundFileImageSource, /58svh/)
    assert.doesNotMatch(playgroundFileImageSource, /680px/)
    assert.doesNotMatch(playgroundFileImageSource, /h-\[min\(36svh,420px\)\]/)
    assert.doesNotMatch(playgroundFileImageSource, /max-w-\[min\(100%,30rem\)\]/)
    assert.doesNotMatch(playgroundFileImageSource, /14rem/)
    assert.doesNotMatch(playgroundFileImageSource, /34svh/)
    assert.doesNotMatch(playgroundFileImageSource, /22svh/)
    assert.doesNotMatch(playgroundFileImageSource, /220px/)
    assert.doesNotMatch(playgroundFileImageSource, /46svh/)
    assert.doesNotMatch(playgroundFileImageSource, /420px/)
    assert.doesNotMatch(playgroundFileImageSource, /36svh/)
    assert.doesNotMatch(playgroundFileImageSource, /340px/)
    assert.match(playgroundFileImageSource, /max-h-\[min\(30svh,260px\)\]/)
    assert.match(playgroundFileImageSource, /max-w-\[min\(100%,15rem\)\]/)
    assert.match(playgroundFileImageSource, /h-auto/)
    assert.match(playgroundFileImageSource, /w-auto/)
    assert.match(playgroundFileImageSource, /inline-flex/)
    assert.doesNotMatch(playgroundFileImageSource, /mx-auto/)
    assert.match(playgroundFileImageSource, /overflow-hidden/)
    assert.match(playgroundFileImageSource, /object-contain/)
    assert.doesNotMatch(imageGenerationProgressSource, /max-w-\[28rem\]/)
    assert.doesNotMatch(imageGenerationProgressSource, /max-w-\[20rem\]/)
    assert.match(imageGenerationProgressSource, /max-w-\[15rem\]/)
  })

  test('keeps user messages as right-aligned compact bubbles', () => {
    assert.match(messageLayoutUtilsSource, /items-end text-right/)
    assert.match(messageStylesSource, /group-\[\.is-user\]:w-fit/)
    assert.match(messageStylesSource, /group-\[\.is-user\]:max-w-\[85%\]/)
  })
})
