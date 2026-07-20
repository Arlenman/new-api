import assert from 'node:assert/strict'
import { chmod, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawn } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const scriptPath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'build-latest.sh')
const pinnedCommit = 'c81bb8b651b403eb60d04fd84ea57276f2f2b86c'
const otherCommit = '1111111111111111111111111111111111111111'

async function writeExecutable(filePath, source) {
  await writeFile(filePath, source)
  await chmod(filePath, 0o755)
}

function runScript(args, env) {
  return new Promise((resolve, reject) => {
    const child = spawn('/bin/sh', [scriptPath, ...args], { env })
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (chunk) => {
      stdout += chunk
    })
    child.stderr.on('data', (chunk) => {
      stderr += chunk
    })
    child.on('error', reject)
    child.on('close', (code) => resolve({ code, stdout, stderr }))
  })
}

async function createFixture(
  t,
  { checkedOutCommit = pinnedCommit, dirty = false, patchExitCode = 0, withGitMetadata = true } = {},
) {
  const root = await mkdtemp(path.join(tmpdir(), 'infinite-canvas-build-manager-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const binDir = path.join(root, 'bin')
  const sourceRoot = path.join(root, 'third_party', 'infinite-canvas')
  const webRoot = path.join(sourceRoot, 'web')
  const commitFile = path.join(root, 'upstream.commit')
  const outputDir = path.join(root, 'artifact', 'dist')
  const commandsPath = path.join(root, 'commands')
  const tempRoot = path.join(root, 'tmp')
  await mkdir(binDir, { recursive: true })
  await mkdir(path.join(webRoot, 'node_modules'), { recursive: true })
  await mkdir(path.join(webRoot, 'dist'), { recursive: true })
  await mkdir(tempRoot, { recursive: true })
  if (withGitMetadata) await writeFile(path.join(sourceRoot, '.git'), 'gitdir: fixture\n')
  await writeFile(path.join(sourceRoot, 'VERSION'), 'v0.9.0\n')
  await writeFile(path.join(webRoot, 'package.json'), '{"scripts":{"build":"vite build"}}\n')
  await writeFile(path.join(webRoot, 'source.txt'), 'original source\n')
  await writeFile(path.join(webRoot, 'node_modules', 'sentinel'), 'source dependency\n')
  await writeFile(path.join(webRoot, 'dist', 'sentinel'), 'stale source output\n')
  await writeFile(commitFile, `${pinnedCommit}\n`)

  await writeExecutable(
    path.join(binDir, 'git'),
    `#!/bin/sh
printf 'git %s\n' "$*" >> "$TEST_COMMANDS"
if [ "$1" != '-C' ]; then exit 1; fi
case "$3" in
  rev-parse) printf '%s\n' "$TEST_CHECKED_OUT_COMMIT" ;;
  status) if [ "$TEST_DIRTY" = true ]; then printf ' M web/source.txt\n'; fi ;;
  *) exit 1 ;;
esac
`,
  )
  await writeExecutable(
    path.join(binDir, 'node'),
    `#!/bin/sh
printf 'node %s\n' "$*" >> "$TEST_COMMANDS"
case "$1" in
  */patch-upstream.mjs)
    test "$2" != "$TEST_SOURCE_ROOT"
    test ! -e "$2/.git"
    test ! -e "$2/web/node_modules"
    test ! -e "$2/web/dist"
    printf 'patched temporary source\n' > "$2/.new-api-patched"
    exit "$TEST_PATCH_EXIT_CODE"
    ;;
  */write-build-info.mjs)
    exec "$TEST_REAL_NODE" "$@"
    ;;
esac
exit 1
`,
  )
  await writeExecutable(
    path.join(binDir, 'bun'),
    `#!/bin/sh
printf 'bun cwd=%s vite_base=%s args=%s\n' "$PWD" "\${VITE_BASE-}" "$*" >> "$TEST_COMMANDS"
test -f ../.new-api-patched
case "$1 $2" in
  'install ')
    mkdir -p node_modules
    printf 'temporary dependency\n' > node_modules/sentinel
    ;;
  'run build')
    mkdir -p dist
    cat ../.new-api-patched > dist/index.html
    ;;
  *) exit 1 ;;
esac
`,
  )

  return {
    root,
    sourceRoot,
    outputDir,
    commandsPath,
    tempRoot,
    env: {
      ...process.env,
      PATH: `${binDir}:${process.env.PATH}`,
      TMPDIR: tempRoot,
      SOURCE_DATE_EPOCH: '1784505600',
      INFINITE_CANVAS_SOURCE_ROOT: sourceRoot,
      INFINITE_CANVAS_COMMIT_FILE: commitFile,
      TEST_COMMANDS: commandsPath,
      TEST_SOURCE_ROOT: sourceRoot,
      TEST_CHECKED_OUT_COMMIT: checkedOutCommit,
      TEST_DIRTY: dirty ? 'true' : 'false',
      TEST_PATCH_EXIT_CODE: String(patchExitCode),
      TEST_REAL_NODE: process.execPath,
    },
  }
}

test('builds from a disposable Submodule copy and publishes auditable output without mutating the checkout', async (t) => {
  const fixture = await createFixture(t)
  const result = await runScript([fixture.outputDir], fixture.env)

  assert.equal(result.code, 0, result.stderr)
  assert.match(result.stdout, new RegExp(`Submodule ${pinnedCommit}`))
  assert.equal(await readFile(path.join(fixture.sourceRoot, 'web', 'source.txt'), 'utf8'), 'original source\n')
  assert.equal(
    await readFile(path.join(fixture.sourceRoot, 'web', 'node_modules', 'sentinel'), 'utf8'),
    'source dependency\n',
  )
  assert.equal(
    await readFile(path.join(fixture.sourceRoot, 'web', 'dist', 'sentinel'), 'utf8'),
    'stale source output\n',
  )
  assert.equal(await readFile(path.join(fixture.outputDir, 'index.html'), 'utf8'), 'patched temporary source\n')
  assert.deepEqual(await readdir(fixture.tempRoot), [])

  const buildInfo = JSON.parse(await readFile(path.join(fixture.outputDir, 'build-info.json'), 'utf8'))
  assert.deepEqual(buildInfo, {
    version: '0.9.0',
    commit: pinnedCommit,
    source: {
      type: 'git-submodule',
      repository: 'https://github.com/Arlenman/infinite-canvas.git',
      path: 'third_party/infinite-canvas',
    },
    built_at: '2026-07-20T00:00:00.000Z',
  })

  const commands = await readFile(fixture.commandsPath, 'utf8')
  assert.match(commands, new RegExp(`git -C ${fixture.sourceRoot} rev-parse HEAD`))
  assert.match(commands, new RegExp(`git -C ${fixture.sourceRoot} status --porcelain`))
  assert.match(commands, /node .*patch-upstream\.mjs .*new-api-infinite-canvas\.[^/]+\/source/)
  assert.match(commands, /bun cwd=.*\/source\/web vite_base= args=install/)
  assert.match(commands, /bun cwd=.*\/source\/web vite_base=\/_tools\/infinite-canvas\/ args=run build/)
  assert.match(commands, /node .*write-build-info\.mjs .*\/source .*\/source\/web\/dist .*upstream\.commit/)
})

test('supports Docker-style copied source without Git metadata', async (t) => {
  const fixture = await createFixture(t, { withGitMetadata: false })
  const result = await runScript([fixture.outputDir], fixture.env)

  assert.equal(result.code, 0, result.stderr)
  const commands = await readFile(fixture.commandsPath, 'utf8')
  assert.doesNotMatch(commands, /^git /m)
  assert.equal(JSON.parse(await readFile(path.join(fixture.outputDir, 'build-info.json'), 'utf8')).commit, pinnedCommit)
})

test('rejects a Submodule and commit-marker mismatch before patching or building', async (t) => {
  const fixture = await createFixture(t, { checkedOutCommit: otherCommit })
  const result = await runScript([fixture.outputDir], fixture.env)

  assert.equal(result.code, 1)
  assert.match(result.stderr, new RegExp(`expected ${pinnedCommit}, got ${otherCommit}`))
  assert.doesNotMatch(await readFile(fixture.commandsPath, 'utf8'), /^node |^bun /m)
  await assert.rejects(readFile(path.join(fixture.outputDir, 'index.html'), 'utf8'), { code: 'ENOENT' })
})

test('rejects a dirty Submodule before copying it', async (t) => {
  const fixture = await createFixture(t, { dirty: true })
  const result = await runScript([fixture.outputDir], fixture.env)

  assert.equal(result.code, 1)
  assert.match(result.stderr, /Submodule has uncommitted changes/)
  assert.doesNotMatch(await readFile(fixture.commandsPath, 'utf8'), /^node |^bun /m)
  assert.deepEqual(await readdir(fixture.tempRoot), [])
})

test('does not install, build, or publish output when the upstream patch fails', async (t) => {
  const fixture = await createFixture(t, { patchExitCode: 1 })
  const result = await runScript([fixture.outputDir], fixture.env)

  assert.equal(result.code, 1)
  const commands = await readFile(fixture.commandsPath, 'utf8')
  assert.match(commands, /^node .*patch-upstream\.mjs/m)
  assert.doesNotMatch(commands, /^bun /m)
  await assert.rejects(readFile(path.join(fixture.outputDir, 'index.html'), 'utf8'), { code: 'ENOENT' })
  assert.deepEqual(await readdir(fixture.tempRoot), [])
})
