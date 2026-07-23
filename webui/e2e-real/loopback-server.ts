import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const repositoryRoot = resolve(import.meta.dirname, '..', '..')
const cliDirectory = join(repositoryRoot, 'cli')

export interface LoopbackWorkspace {
  readonly bootstrapURL: string
  close(): Promise<void>
}

export async function startLoopbackWorkspace(): Promise<LoopbackWorkspace> {
  const root = await mkdtemp(join(tmpdir(), 'wechat-article-playwright-'))
  const binary = join(root, 'wechat-article')
  await run('go', ['build', '-trimpath', '-o', binary, './cmd/wechat-article'], cliDirectory)

  const serverProcess = spawn(binary, ['web', '--no-open'], {
    cwd: cliDirectory,
    env: {
      ...process.env,
      WECHAT_ARTICLE_PORTABLE_ROOT: join(root, 'profile'),
      WECHAT_ARTICLE_SECRET_BACKEND: 'memory'
    },
    stdio: 'pipe'
  })

  try {
    const bootstrapURL = await readBootstrapURL(serverProcess)
    return {
      bootstrapURL,
      close: async () => {
        await stop(serverProcess)
        await rm(root, { recursive: true, force: true })
      }
    }
  } catch (error) {
    await stop(serverProcess)
    await rm(root, { recursive: true, force: true })
    throw error
  }
}

async function run(command: string, args: readonly string[], cwd: string): Promise<void> {
  await new Promise<void>((resolveRun, reject) => {
    const child = spawn(command, args, { cwd, env: process.env, stdio: 'pipe' })
    let stderr = ''
    child.stderr.setEncoding('utf8')
    child.stderr.on('data', (chunk: string) => { stderr += chunk })
    child.once('error', reject)
    child.once('exit', (code) => {
      if (code === 0) resolveRun()
      else reject(new Error(`build local browser workspace: ${stderr.trim() || `exit ${code ?? 'unknown'}`}`))
    })
  })
}

async function readBootstrapURL(child: ChildProcessWithoutNullStreams): Promise<string> {
  return new Promise<string>((resolveURL, reject) => {
    let stdout = ''
    let stderr = ''
    const timeout = setTimeout(() => finish(new Error('local browser workspace did not print its bootstrap URL within 20 seconds')), 20_000)
    child.stdout.setEncoding('utf8')
    child.stderr.setEncoding('utf8')
    child.stdout.on('data', (chunk: string) => {
      stdout += chunk
      const lines = stdout.trim().split(/\r?\n/).filter(Boolean)
      if (lines.length === 1 && lines[0]) finish(undefined, lines[0])
      if (lines.length > 1) finish(new Error('local browser workspace printed more than one stdout line'))
    })
    child.stderr.on('data', (chunk: string) => { stderr += chunk })
    child.once('error', finish)
    child.once('exit', (code) => finish(new Error(`local browser workspace exited before bootstrap: ${stderr.trim() || `exit ${code ?? 'unknown'}`}`)))

    function finish(error?: Error, url?: string) {
      clearTimeout(timeout)
      child.stdout.removeAllListeners('data')
      child.stderr.removeAllListeners('data')
      child.removeAllListeners('error')
      child.removeAllListeners('exit')
      if (error) reject(error)
      else if (url) resolveURL(url)
    }
  })
}

async function stop(child: ChildProcessWithoutNullStreams): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return
  await new Promise<void>((resolveStop) => {
    const timeout = setTimeout(() => {
      child.kill('SIGKILL')
      resolveStop()
    }, 5_000)
    child.once('exit', () => {
      clearTimeout(timeout)
      resolveStop()
    })
    child.kill('SIGTERM')
  })
}
