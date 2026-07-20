import assert from 'node:assert/strict'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import test from 'node:test'

import { writeBuildInfo } from './write-build-info.mjs'

const pinnedCommit = 'c81bb8b651b403eb60d04fd84ea57276f2f2b86c'
const publicSource = {
  type: 'git-submodule',
  repository: 'https://github.com/Arlenman/infinite-canvas.git',
  path: 'third_party/infinite-canvas',
}

async function createFixture(t, commit = pinnedCommit) {
  const root = await mkdtemp(path.join(tmpdir(), 'infinite-canvas-build-info-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const dist = path.join(root, 'dist')
  const commitFile = path.join(root, 'upstream.commit')
  await mkdir(path.join(root, '.git'), { recursive: true })
  await writeFile(path.join(root, 'VERSION'), 'v0.9.0\n')
  await writeFile(path.join(root, '.git', 'config'), 'url = https://user:secret-token@example.invalid/private.git\n')
  await writeFile(commitFile, `${commit}\n`)
  return { root, dist, commitFile }
}

test('writes deterministic public Submodule provenance without reading Git remotes', async (t) => {
  const fixture = await createFixture(t)
  const info = await writeBuildInfo(fixture.root, fixture.dist, {
    commitFile: fixture.commitFile,
    builtAt: '2026-07-20T00:00:00.000Z',
  })

  assert.deepEqual(info, {
    version: '0.9.0',
    commit: pinnedCommit,
    source: publicSource,
    built_at: '2026-07-20T00:00:00.000Z',
  })
  const output = await readFile(path.join(fixture.dist, 'build-info.json'), 'utf8')
  assert.deepEqual(JSON.parse(output), info)
  assert.doesNotMatch(output, /secret-token|example\.invalid/)
})

test('uses the integration commit marker by default', async (t) => {
  const fixture = await createFixture(t)
  const info = await writeBuildInfo(fixture.root, fixture.dist, {
    builtAt: '2026-07-20T00:00:00.000Z',
  })

  assert.equal(info.commit, (await readFile(new URL('./upstream.commit', import.meta.url), 'utf8')).trim())
  assert.deepEqual(info.source, publicSource)
})

test('rejects a non-SHA commit marker instead of publishing ambiguous provenance', async (t) => {
  const fixture = await createFixture(t, 'main')

  await assert.rejects(
    writeBuildInfo(fixture.root, fixture.dist, {
      commitFile: fixture.commitFile,
      builtAt: '2026-07-20T00:00:00.000Z',
    }),
    /40-character lowercase SHA/,
  )
  await assert.rejects(readFile(path.join(fixture.dist, 'build-info.json'), 'utf8'), { code: 'ENOENT' })
})
