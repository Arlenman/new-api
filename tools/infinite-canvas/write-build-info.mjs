import { mkdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const integrationRoot = path.dirname(fileURLToPath(import.meta.url))
const defaultCommitFile = path.join(integrationRoot, 'upstream.commit')
const source = Object.freeze({
  type: 'git-submodule',
  repository: 'https://github.com/Arlenman/infinite-canvas.git',
  path: 'third_party/infinite-canvas',
})

function buildTimestamp(options) {
  if (options.builtAt) return options.builtAt
  const sourceDateEpoch = process.env.SOURCE_DATE_EPOCH
  if (sourceDateEpoch === undefined) return new Date().toISOString()
  if (!/^[0-9]+$/.test(sourceDateEpoch)) {
    throw new Error('SOURCE_DATE_EPOCH must be a non-negative integer')
  }
  return new Date(Number(sourceDateEpoch) * 1000).toISOString()
}

export async function writeBuildInfo(upstreamRoot, distRoot, options = {}) {
  const root = path.resolve(upstreamRoot)
  const version = (await readFile(path.join(root, 'VERSION'), 'utf8')).trim().replace(/^v(?=\d)/, '')
  const commitFile = options.commitFile ? path.resolve(options.commitFile) : defaultCommitFile
  const commit = String(options.commit ?? (await readFile(commitFile, 'utf8'))).trim()

  if (!version) throw new Error('upstream version is empty')
  if (!/^[0-9a-f]{40}$/.test(commit)) {
    throw new Error('upstream commit marker must be a 40-character lowercase SHA')
  }

  const info = {
    version,
    commit,
    source,
    built_at: buildTimestamp(options),
  }
  const outputRoot = path.resolve(distRoot)
  await mkdir(outputRoot, { recursive: true })
  await writeFile(path.join(outputRoot, 'build-info.json'), `${JSON.stringify(info, null, 2)}\n`)
  return info
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const upstreamRoot = process.argv[2]
  const distRoot = process.argv[3]
  const commitFile = process.argv[4]
  if (!upstreamRoot || !distRoot) {
    throw new Error('usage: node write-build-info.mjs <upstream-root> <dist-root> [commit-file]')
  }
  await writeBuildInfo(upstreamRoot, distRoot, commitFile ? { commitFile } : undefined)
}
