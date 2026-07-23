import type { JobRecord } from './api'
import { navigateTo } from '../app/navigation'

const jobHandoffStorageKey = 'wechat-article.job-handoff.v1'

export interface JobHandoff {
  readonly id: string
}

interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

interface JobHandoffDependencies {
  readonly storage?: StorageLike
  readonly navigate?: (href: string) => void
}

export function jobHandoffLocation(jobID: string): string {
  return `/jobs?job=${encodeURIComponent(jobID)}`
}

export function saveJobHandoff(job: Pick<JobRecord, 'id'>, storage = getSessionStorage()): void {
  const id = job.id.trim()
  if (!id || !storage) return
  try {
    storage.setItem(jobHandoffStorageKey, JSON.stringify({ id } satisfies JobHandoff))
  } catch { /* Browser storage can be unavailable. */ }
}

export function loadJobHandoff(storage = getSessionStorage()): JobHandoff | undefined {
  if (!storage) return undefined
  try {
    const raw = storage.getItem(jobHandoffStorageKey)
    if (!raw) return undefined
    const value = JSON.parse(raw) as Partial<JobHandoff>
    return typeof value.id === 'string' && value.id.trim() ? { id: value.id } : undefined
  } catch { return undefined }
}

/**
 * Records a locally created job and moves the workspace to its stable jobs URL.
 * The jobs page can adopt the stored ID or `job` query parameter without coupling
 * mutations to its presentation state.
 */
export function handoffCreatedJob(job: Pick<JobRecord, 'id'>, dependencies: JobHandoffDependencies = {}): void {
  const id = job.id.trim()
  if (!id) return
  saveJobHandoff({ id }, dependencies.storage)
  ;(dependencies.navigate ?? navigateTo)(jobHandoffLocation(id))
}

function getSessionStorage(): StorageLike | undefined {
  if (typeof window === 'undefined') return undefined
  try { return window.sessionStorage } catch { return undefined }
}
