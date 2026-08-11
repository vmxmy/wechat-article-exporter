import type { JobRecord } from '../../../lib/api'

export type JobPhase = 'waiting' | 'running' | 'finished' | 'unknown'

export interface JobTiming {
  readonly startedAt?: string
  readonly completedAt?: string
  readonly phase: JobPhase
  /** The timestamp the "Started" column anchors on: the real start when the
      backend reports one, otherwise the creation time. */
  readonly anchor: string
  readonly anchorIsStart: boolean
  readonly queueWaitMs?: number
  readonly durationMs?: number
}

const terminalStates = new Set(['completed', 'partial', 'failed', 'cancelled'])

function parse(value: string | undefined): number | undefined {
  if (!value) return undefined
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? undefined : parsed
}

/** Clamps clock skew rather than surfacing a negative duration. */
function elapsed(from: number | undefined, to: number | undefined): number | undefined {
  if (from === undefined || to === undefined) return undefined
  return Math.max(0, to - from)
}

export function resolveJobTiming(job: JobRecord, now: number): JobTiming {
  const created = parse(job.createdAt)
  const started = parse(job.startedAt)
  const completed = parse(job.completedAt)
  const updated = parse(job.updatedAt)
  const anchorIsStart = started !== undefined
  const anchor = anchorIsStart ? job.startedAt! : job.createdAt
  const queueWaitMs = elapsed(created, started)

  if (started !== undefined && completed !== undefined) {
    return { startedAt: job.startedAt, completedAt: job.completedAt, phase: 'finished', anchor, anchorIsStart, queueWaitMs, durationMs: elapsed(started, completed) }
  }
  if (started !== undefined) {
    return { startedAt: job.startedAt, phase: 'running', anchor, anchorIsStart, queueWaitMs, durationMs: elapsed(started, now) }
  }
  if (!terminalStates.has(job.state) && job.state !== 'running' && created !== undefined) {
    return { phase: 'waiting', anchor, anchorIsStart, durationMs: elapsed(created, now) }
  }
  // No startedAt at all: either the backend predates the widened DTO or the
  // job never ran. Fall back to the legacy updatedAt - createdAt span so the
  // column keeps working rather than reading as empty.
  return { phase: 'unknown', anchor, anchorIsStart, durationMs: elapsed(created, updated) }
}
