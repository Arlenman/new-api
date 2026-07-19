import { execFile } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'
import { pathToFileURL } from 'node:url'

const execFileAsync = promisify(execFile)

export async function writeBuildInfo(upstreamRoot, distRoot, options = {}) {
  const packageInfo = JSON.parse(await readFile(path.join(upstreamRoot, 'package.json'), 'utf8'))
  const commit = options.commit ?? (await execFileAsync('git', ['-C', upstreamRoot, 'rev-parse', 'HEAD'])).stdout.trim()
  const info = {
    version: String(packageInfo.version ?? ''),
    commit,
    built_at: options.builtAt ?? new Date().toISOString(),
  }
  await writeFile(path.join(distRoot, 'build-info.json'), `${JSON.stringify(info, null, 2)}\n`)
  return info
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const upstreamRoot = process.argv[2]
  const distRoot = process.argv[3]
  if (!upstreamRoot || !distRoot) {
    throw new Error('usage: node write-build-info.mjs <upstream-root> <dist-root>')
  }
  await writeBuildInfo(path.resolve(upstreamRoot), path.resolve(distRoot))
}
