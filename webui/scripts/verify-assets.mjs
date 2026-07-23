import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const manifestPath = resolve('dist/.vite/manifest.json')
const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
const entries = Object.values(manifest)

if (!entries.some((entry) => entry.isEntry)) {
  throw new Error('Vite manifest has no entrypoint.')
}

const assets = entries.flatMap((entry) => [entry.file, ...(entry.css ?? []), ...(entry.assets ?? [])])
if (assets.some((asset) => !/-[A-Za-z0-9_-]{8,}\./.test(asset))) {
  throw new Error('Vite manifest contains a non-fingerprinted asset.')
}
