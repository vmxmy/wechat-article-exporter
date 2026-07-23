import { readFile, stat } from 'node:fs/promises'
import { gzipSync } from 'node:zlib'
import { dirname, resolve } from 'node:path'

const manifestPath = resolve('dist/.vite/manifest.json')
const distDirectory = resolve(dirname(manifestPath), '..')
const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
const entries = Object.values(manifest)

if (!entries.some((entry) => entry.isEntry)) {
  throw new Error('Vite manifest has no entrypoint.')
}

const assets = entries.flatMap((entry) => [entry.file, ...(entry.css ?? []), ...(entry.assets ?? [])])
for (const asset of assets) {
  if (!isFingerprintedAsset(asset)) {
    throw new Error(`Vite manifest contains a non-fingerprinted asset: ${asset}`)
  }
  if (!isLocalAssetPath(asset)) {
    throw new Error(`Vite manifest contains an unsafe asset path: ${asset}`)
  }
  try {
    const info = await stat(resolve(distDirectory, asset))
    if (!info.isFile()) throw new Error('not a file')
  } catch {
    throw new Error(`Vite manifest references a missing asset: ${asset}`)
  }
}

const entrypoint = entries.find((entry) => entry.isEntry)
const indexHTML = await readFile(resolve(distDirectory, 'index.html'), 'utf8')
if (hasRemoteReference(indexHTML)) {
  throw new Error('Generated index.html references a remote resource.')
}
for (const asset of [entrypoint.file, ...(entrypoint.css ?? [])]) {
  if (!indexHTML.includes(`/${asset}`)) {
    throw new Error(`Generated index.html does not reference entrypoint asset: ${asset}`)
  }
}

const gzipBudgetBytes = 250 * 1024
const entrypointBytes = await readFile(resolve(distDirectory, entrypoint.file))
const gzipBytes = gzipSync(entrypointBytes).byteLength
if (gzipBytes > gzipBudgetBytes) {
  throw new Error(`Initial JavaScript gzip size ${gzipBytes} exceeds ${gzipBudgetBytes} byte budget.`)
}

function isFingerprintedAsset(asset) {
  const file = asset.split('/').at(-1) ?? ''
  // Vite hashes use a URL-safe alphabet. They may contain both underscores
  // and hyphens (for example, index-Dlmg_X-D.js).
  return /^.+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$/.test(file)
}

function isLocalAssetPath(asset) {
  return /^assets\/[^/\\]+$/.test(asset) && !asset.includes('..')
}

function hasRemoteReference(contents) {
  return /(?:src|href)=["']\s*(?:https?:)?\/\//i.test(contents)
}
