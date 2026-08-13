import type { JobRecord } from '../../../lib/api'

/** `none` renders no bar at all; `indeterminate` renders a bar with unknown
    extent; `determinate` renders a ratio. */
export type JobProgressMode = 'determinate' | 'indeterminate' | 'none'

export interface JobProgressSummary {
  readonly total?: number
  readonly completed: number
  readonly partial: number
  readonly failed: number
  readonly cancelled: number
  readonly running: number
  readonly queued: number
  readonly blockedAuth: number
  /** Items that reached a terminal state, whatever the outcome. */
  readonly settled: number
  readonly ratio?: number
  readonly mode: JobProgressMode
}

const workingStates = new Set(['running', 'blocked_auth', 'paused'])

function readCount(counts: Readonly<Record<string, number>> | undefined, key: string): number {
  const value = counts?.[key]
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0
}

/** Reads only real job-item state keys. The `succeeded`/`failures` keys the
    page used to read never appear in backend data. */
export function summarizeJobCounts(job: Pick<JobRecord, 'state' | 'counts'>): JobProgressSummary {
  const counts = job.counts
  const completed = readCount(counts, 'completed')
  const partial = readCount(counts, 'partial')
  const failed = readCount(counts, 'failed')
  const cancelled = readCount(counts, 'cancelled')
  const running = readCount(counts, 'running')
  const queued = readCount(counts, 'queued')
  const blockedAuth = readCount(counts, 'blocked_auth')
  const settled = completed + partial + failed + cancelled
  const rawTotal = counts?.total
  const total = typeof rawTotal === 'number' && Number.isFinite(rawTotal) && rawTotal >= 0 ? Math.floor(rawTotal) : undefined
  const base = { total, completed, partial, failed, cancelled, running, queued, blockedAuth, settled }

  if (!counts || Object.keys(counts).length === 0) return { ...base, mode: 'none' }
  // A job with zero items has nothing to divide by, and a 0/0 bar reads as
  // "stuck at zero" rather than "nothing to do".
  if (total === 0) return { ...base, mode: 'none' }
  if (total === undefined) {
    return { ...base, mode: workingStates.has(job.state) ? 'indeterminate' : 'none' }
  }
  // A queued job with known extent is determinate at zero: indeterminate means
  // "working, extent unknown", and a queued job is not working.
  const ratio = Math.min(100, Math.max(0, Math.round((settled / total) * 100)))
  return { ...base, ratio, mode: 'determinate' }
}
