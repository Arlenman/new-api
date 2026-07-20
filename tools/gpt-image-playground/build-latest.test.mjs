import assert from 'node:assert/strict'
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawn } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const scriptPath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'build-latest.sh')
const upstreamCommit = 'ae8de0f192a22ec23513aad75ed6766f0245976c'

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

function fakeGitSource({ failResolve = false } = {}) {
  return `#!/bin/sh
printf 'git %s\\n' "$*" >> "$TEST_COMMANDS"
if [ "$1" = 'ls-remote' ]; then
  if [ "${failResolve ? 'true' : 'false'}" = true ]; then exit 1; fi
  printf '%s\\trefs/heads/main\\n' "$TEST_UPSTREAM_COMMIT"
  exit 0
fi
if [ "$1" = '-C' ]; then
  case "$3" in
    submodule|fetch|checkout) exit 0 ;;
    rev-parse) printf '%s\\n' "$TEST_UPSTREAM_COMMIT"; exit 0 ;;
  esac
fi
exit 1
`
}

async function createFixture(t, { failResolve = false, nodeExitCode = 0 } = {}) {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-latest-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const binDir = path.join(root, 'bin')
  const toolsDir = path.join(root, 'tools', 'gpt-image-playground')
  const submoduleDir = path.join(root, 'third_party', 'gpt-image-playground')
  const commandsPath = path.join(root, 'commands')
  const dockerArgsPath = path.join(root, 'docker-args')
  const commitPath = path.join(toolsDir, 'upstream.commit')
  await mkdir(binDir, { recursive: true })
  await mkdir(toolsDir, { recursive: true })
  await mkdir(submoduleDir, { recursive: true })
  await writeFile(path.join(toolsDir, 'patch-upstream.test.mjs'), '')
  await writeFile(commitPath, 'old-commit\n')

  await writeExecutable(path.join(binDir, 'git'), fakeGitSource({ failResolve }))
  await writeExecutable(
    path.join(binDir, 'node'),
    `#!/bin/sh
printf 'node %s\\n' "$*" >> "$TEST_COMMANDS"
exit ${nodeExitCode}
`,
  )
  await writeExecutable(
    path.join(binDir, 'docker'),
    `#!/bin/sh
printf '%s\\n' "$@" > "$TEST_DOCKER_ARGS"
`,
  )
  await writeExecutable(path.join(binDir, 'sleep'), '#!/bin/sh\nexit 0\n')

  return {
    root,
    binDir,
    commandsPath,
    dockerArgsPath,
    commitPath,
    env: {
      ...process.env,
      PATH: `${binDir}:${process.env.PATH}`,
      GPT_IMAGE_PLAYGROUND_REPO_ROOT: root,
      TEST_COMMANDS: commandsPath,
      TEST_DOCKER_ARGS: dockerArgsPath,
      TEST_UPSTREAM_COMMIT: upstreamCommit,
    },
  }
}

test('resolves the Fork branch, updates the detached submodule, runs compatibility tests, and builds', async (t) => {
  const fixture = await createFixture(t)
  const result = await runScript(['-t', 'new-api:latest-test'], fixture.env)

  assert.equal(result.code, 0, result.stderr)
  assert.match(result.stdout, new RegExp(`detached ${upstreamCommit}`))
  assert.match(result.stdout, new RegExp(`Fork branch main at ${upstreamCommit}`))
  assert.equal(await readFile(fixture.commitPath, 'utf8'), `${upstreamCommit}\n`)
  assert.deepEqual(await readFile(fixture.commandsPath, 'utf8'), [
    `git ls-remote https://github.com/Arlenman/gpt_image_playground.git refs/heads/main`,
    `git -C ${fixture.root} submodule update --init -- third_party/gpt-image-playground`,
    `git -C ${fixture.root}/third_party/gpt-image-playground fetch --depth 1 https://github.com/Arlenman/gpt_image_playground.git ${upstreamCommit}`,
    `git -C ${fixture.root}/third_party/gpt-image-playground checkout --detach ${upstreamCommit}`,
    `git -C ${fixture.root}/third_party/gpt-image-playground rev-parse HEAD`,
    'node --test tools/gpt-image-playground/patch-upstream.test.mjs',
  ].join('\n') + '\n')
  assert.deepEqual((await readFile(fixture.dockerArgsPath, 'utf8')).trim().split('\n'), [
    'build',
    '--build-arg',
    `GPT_IMAGE_PLAYGROUND_REF=${upstreamCommit}`,
    '-t',
    'new-api:latest-test',
    '.',
  ])
})

test('fails without touching the submodule or invoking compatibility tests and docker when the Fork branch cannot be resolved', async (t) => {
  const fixture = await createFixture(t, { failResolve: true })
  const result = await runScript([], fixture.env)

  assert.equal(result.code, 1)
  assert.match(result.stderr, /Unable to resolve Fork branch: main/)
  assert.equal(await readFile(fixture.commandsPath, 'utf8'), [
    `git ls-remote https://github.com/Arlenman/gpt_image_playground.git refs/heads/main`,
    `git ls-remote https://github.com/Arlenman/gpt_image_playground.git refs/heads/main`,
    `git ls-remote https://github.com/Arlenman/gpt_image_playground.git refs/heads/main`,
  ].join('\n') + '\n')
  assert.equal(await readFile(fixture.commitPath, 'utf8'), 'old-commit\n')
  await assert.rejects(readFile(fixture.dockerArgsPath, 'utf8'), { code: 'ENOENT' })
})

test('does not build when compatibility tests fail after the submodule is pinned', async (t) => {
  const fixture = await createFixture(t, { nodeExitCode: 1 })
  const result = await runScript([], fixture.env)

  assert.equal(result.code, 1)
  assert.equal(await readFile(fixture.commitPath, 'utf8'), `${upstreamCommit}\n`)
  assert.match(await readFile(fixture.commandsPath, 'utf8'), /node --test tools\/gpt-image-playground\/patch-upstream\.test\.mjs/)
  await assert.rejects(readFile(fixture.dockerArgsPath, 'utf8'), { code: 'ENOENT' })
})
