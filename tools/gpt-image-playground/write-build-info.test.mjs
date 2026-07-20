import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { writeBuildInfo } from './write-build-info.mjs'

const upstreamCommit = 'ae8de0f192a22ec23513aad75ed6766f0245976c'

test('writes deterministic build info from a pinned commit marker without reading .git', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-build-info-'))
  const dist = path.join(root, 'dist')
  const commitFile = path.join(root, 'upstream.commit')
  await mkdir(dist)
  await writeFile(path.join(root, 'package.json'), JSON.stringify({ version: '0.7.0' }))
  await writeFile(commitFile, `${upstreamCommit}\n`)

  const info = await writeBuildInfo(root, dist, {
    commitFile,
    builtAt: '2026-07-20T00:00:00.000Z',
  })

  assert.deepEqual(info, {
    version: '0.7.0',
    commit: upstreamCommit,
    built_at: '2026-07-20T00:00:00.000Z',
  })
  assert.deepEqual(JSON.parse(await readFile(path.join(dist, 'build-info.json'), 'utf8')), info)
})

test('uses the integration commit marker by default', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-build-info-default-'))
  const dist = path.join(root, 'dist')
  await mkdir(dist)
  await writeFile(path.join(root, 'package.json'), JSON.stringify({ version: '0.7.0' }))

  const info = await writeBuildInfo(root, dist, {
    builtAt: '2026-07-20T00:00:00.000Z',
  })

  assert.equal(info.commit, (await readFile(new URL('./upstream.commit', import.meta.url), 'utf8')).trim())
})
