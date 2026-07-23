import { cp, lstat, mkdtemp, readFile, readdir, rename, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { basename, dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const webuiDirectory = resolve(scriptDirectory, '..')
const sourceDirectory = resolve(webuiDirectory, 'dist')
const targetDirectory = resolve(webuiDirectory, '../cli/internal/web/assets')
const checkOnly = process.argv.includes('--check')

await assertDirectory(sourceDirectory, 'Vite dist directory')

if (checkOnly) {
  const mismatch = await firstMismatch(sourceDirectory, targetDirectory)
  if (mismatch) {
    throw new Error(`embedded Go assets are stale: ${mismatch}. Run \`pnpm run sync:go-assets\`.`)
  }
} else {
  const stagingParent = await mkdtemp(join(tmpdir(), 'wechat-article-web-assets-'))
  const stagedAssets = join(stagingParent, basename(targetDirectory))
  try {
    await cp(sourceDirectory, stagedAssets, { recursive: true, force: true })
    const mismatch = await firstMismatch(sourceDirectory, targetDirectory)
    if (mismatch) {
      await replaceDirectory(stagedAssets, targetDirectory)
    }
  } finally {
    await rm(stagingParent, { recursive: true, force: true })
  }
}

async function assertDirectory(directory, label) {
  let info
  try {
    info = await lstat(directory)
  } catch {
    throw new Error(`${label} is missing at ${directory}. Run \`pnpm run build\` first.`)
  }
  if (!info.isDirectory()) throw new Error(`${label} is not a directory: ${directory}`)
}

async function firstMismatch(source, target) {
  try {
    await assertDirectory(target, 'embedded Go asset directory')
  } catch (error) {
    return error.message
  }
  const sourceFiles = await filesUnder(source)
  const targetFiles = await filesUnder(target)
  if (sourceFiles.join('\n') !== targetFiles.join('\n')) {
    return 'file list differs'
  }
  for (const file of sourceFiles) {
    const [sourceBytes, targetBytes] = await Promise.all([readFile(join(source, file)), readFile(join(target, file))])
    if (!sameBytes(sourceBytes, targetBytes)) return `${file} differs`
  }
  return ''
}

async function replaceDirectory(staged, target) {
  const backup = `${target}.previous`
  await rm(backup, { recursive: true, force: true })
  let movedCurrent = false
  try {
    try {
      await rename(target, backup)
      movedCurrent = true
    } catch (error) {
      if (error.code !== 'ENOENT') throw error
    }
    await rename(staged, target)
    if (movedCurrent) await rm(backup, { recursive: true, force: true })
  } catch (error) {
    if (movedCurrent) {
      try {
        await rename(backup, target)
      } catch {
        // Preserve the original replacement error; the caller can inspect the backup.
      }
    }
    throw error
  }
}

async function filesUnder(directory, prefix = '') {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = []
  for (const entry of entries.sort((left, right) => left.name.localeCompare(right.name))) {
    const child = join(directory, entry.name)
    const name = relative(directory, child)
    const childPrefix = prefix ? join(prefix, name) : name
    if (entry.isDirectory()) files.push(...await filesUnder(child, childPrefix))
    else if (entry.isFile()) files.push(childPrefix)
    else throw new Error(`unsupported generated asset entry: ${child}`)
  }
  return files
}

function sameBytes(left, right) {
  if (left.byteLength !== right.byteLength) return false
  const first = new Uint8Array(left)
  const second = new Uint8Array(right)
  return first.every((byte, index) => byte === second[index])
}
