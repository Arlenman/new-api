import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { writeBuildInfo } from './write-build-info.mjs'

test('writes upstream version, resolved commit, and build timestamp', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-build-info-'))
  const dist = path.join(root, 'dist')
  await mkdir(dist)
  await writeFile(path.join(root, 'package.json'), JSON.stringify({ version: '0.7.0' }))

  const info = await writeBuildInfo(root, dist, {
    commit: 'a10477581b3d43ac98d39777e4445625a9db113d',
    builtAt: '2026-07-17T00:00:00.000Z',
  })

  assert.deepEqual(info, {
    version: '0.7.0',
    commit: 'a10477581b3d43ac98d39777e4445625a9db113d',
    built_at: '2026-07-17T00:00:00.000Z',
  })
  assert.deepEqual(JSON.parse(await readFile(path.join(dist, 'build-info.json'), 'utf8')), info)
})
