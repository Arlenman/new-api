import assert from 'node:assert/strict'
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawn } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const scriptPath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'build-latest.sh')
const upstreamCommit = 'a10477581b3d43ac98d39777e4445625a9db113d'

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

test('resolves the latest branch to an immutable commit before building', async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-latest-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const binDir = path.join(root, 'bin')
  const callsPath = path.join(root, 'docker-args')
  const attemptsPath = path.join(root, 'git-attempts')
  await mkdir(binDir)

  await writeExecutable(
    path.join(binDir, 'git'),
    `#!/bin/sh
attempts=0
if [ -f "$TEST_GIT_ATTEMPTS" ]; then attempts="$(cat "$TEST_GIT_ATTEMPTS")"; fi
attempts=$((attempts + 1))
printf '%s' "$attempts" > "$TEST_GIT_ATTEMPTS"
if [ "$attempts" -eq 1 ]; then exit 1; fi
printf '%s\\trefs/heads/main\\n' "${upstreamCommit}"
`
  )
  await writeExecutable(
    path.join(binDir, 'docker'),
    `#!/bin/sh
printf '%s\\n' "$@" > "$TEST_DOCKER_ARGS"
`
  )
  await writeExecutable(path.join(binDir, 'sleep'), '#!/bin/sh\nexit 0\n')

  const result = await runScript(['-t', 'new-api:latest-test'], {
    ...process.env,
    PATH: `${binDir}:${process.env.PATH}`,
    TEST_DOCKER_ARGS: callsPath,
    TEST_GIT_ATTEMPTS: attemptsPath,
  })

  assert.equal(result.code, 0, result.stderr)
  assert.match(result.stdout, new RegExp(`main at ${upstreamCommit}`))
  assert.equal(await readFile(attemptsPath, 'utf8'), '2')
  assert.deepEqual((await readFile(callsPath, 'utf8')).trim().split('\n'), [
    'build',
    '--build-arg',
    `GPT_IMAGE_PLAYGROUND_REF=${upstreamCommit}`,
    '-t',
    'new-api:latest-test',
    '.',
  ])
})

test('fails without invoking docker when the upstream branch cannot be resolved', async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), 'gpt-image-playground-latest-failure-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const binDir = path.join(root, 'bin')
  const dockerCalledPath = path.join(root, 'docker-called')
  await mkdir(binDir)

  await writeExecutable(path.join(binDir, 'git'), '#!/bin/sh\nexit 1\n')
  await writeExecutable(
    path.join(binDir, 'docker'),
    `#!/bin/sh
printf called > "$TEST_DOCKER_CALLED"
`
  )
  await writeExecutable(path.join(binDir, 'sleep'), '#!/bin/sh\nexit 0\n')

  const result = await runScript([], {
    ...process.env,
    PATH: `${binDir}:${process.env.PATH}`,
    TEST_DOCKER_CALLED: dockerCalledPath,
  })

  assert.equal(result.code, 1)
  assert.match(result.stderr, /Unable to resolve upstream branch: main/)
  await assert.rejects(readFile(dockerCalledPath, 'utf8'), { code: 'ENOENT' })
})
