import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const integrationRoot = path.dirname(fileURLToPath(import.meta.url))
const defaultCommitFile = path.join(integrationRoot, 'upstream.commit')

export async function writeBuildInfo(upstreamRoot, distRoot, options = {}) {
  const root = path.resolve(upstreamRoot)
  const packageInfo = JSON.parse(await readFile(path.join(root, 'package.json'), 'utf8'))
  const commit = String(
    options.commit ??
      (await readFile(options.commitFile ? path.resolve(options.commitFile) : defaultCommitFile, 'utf8')),
  ).trim()
  if (!commit) throw new Error('upstream commit marker is empty')
  const info = {
    version: String(packageInfo.version ?? ''),
    commit,
    built_at: options.builtAt ?? new Date().toISOString(),
  }
  await writeFile(path.join(path.resolve(distRoot), 'build-info.json'), `${JSON.stringify(info, null, 2)}\n`)
  return info
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const upstreamRoot = process.argv[2]
  const distRoot = process.argv[3]
  if (!upstreamRoot || !distRoot) {
    throw new Error('usage: node write-build-info.mjs <upstream-root> <dist-root>')
  }
  await writeBuildInfo(upstreamRoot, distRoot)
}
