import { readFile } from 'node:fs/promises'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const webUIRoot = resolve(scriptDirectory, '..')
const assetDirectory = resolve(webUIRoot, '../cli/internal/web/assets')
const budget = JSON.parse(await readFile(resolve(webUIRoot, 'release-size-budget.json'), 'utf8'))
const manifest = JSON.parse(await readFile(resolve(assetDirectory, '.vite/manifest.json'), 'utf8'))

if (budget.schemaVersion !== 1 || !Number.isSafeInteger(budget.initialJavaScriptGzipBytes) || budget.initialJavaScriptGzipBytes <= 0) {
  throw new Error('Release-size budget must contain a positive initialJavaScriptGzipBytes value.')
}

const entries = Object.entries(manifest)
  .filter(([, entry]) => entry?.isEntry)
  .sort(([left], [right]) => left.localeCompare(right))

if (entries.length !== 1) {
  throw new Error(`Expected exactly one Vite entrypoint in embedded asset manifest; found ${entries.length}.`)
}

const initialJavaScript = collectInitialJavaScript(entries[0][0], manifest)
let gzipBytes = 0
for (const asset of initialJavaScript) {
  gzipBytes += gzipSync(await readEmbeddedAsset(asset)).byteLength
}

const limit = budget.initialJavaScriptGzipBytes
console.log(`Embedded initial JavaScript gzip: ${gzipBytes} bytes across ${initialJavaScript.length} asset(s); budget: ${limit} bytes.`)
if (gzipBytes > limit) {
  throw new Error(`Embedded initial JavaScript gzip size ${gzipBytes} exceeds ${limit} byte budget.`)
}

function collectInitialJavaScript(entryName, records) {
  const assets = new Set()
  const visited = new Set()

  function visit(name) {
    if (visited.has(name)) return
    visited.add(name)
    const record = records[name]
    if (!record) throw new Error(`Vite manifest import is missing: ${name}`)
    if (typeof record.file !== 'string') throw new Error(`Vite manifest record has no file: ${name}`)
    if (record.file.endsWith('.js')) assets.add(record.file)
    for (const imported of record.imports ?? []) {
      if (typeof imported !== 'string') throw new Error(`Vite manifest import is not a string in ${name}`)
      visit(imported)
    }
  }

  visit(entryName)
  return [...assets].sort()
}

async function readEmbeddedAsset(asset) {
  if (typeof asset !== 'string' || asset.startsWith('/') || asset.includes('\\')) {
    throw new Error(`Embedded manifest contains an unsafe JavaScript asset path: ${String(asset)}`)
  }
  const path = resolve(assetDirectory, asset)
  if (relative(assetDirectory, path).startsWith('..')) {
    throw new Error(`Embedded manifest escapes its asset directory: ${asset}`)
  }
  return readFile(path)
}
