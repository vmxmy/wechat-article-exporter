import { describe, expect, it } from 'vitest'
import { jobBackendStates, jobCountedFilters, jobFilterStates } from '../src/features/resources/jobs/jobFilters'
import { summarizeJobCounts } from '../src/features/resources/jobs/jobProgress'
import { resolveJobTiming } from '../src/features/resources/jobs/jobTiming'
import type { JobRecord } from '../src/lib/api'

function job(overrides: Partial<JobRecord> = {}): JobRecord {
  return {
    id: 'job-1',
    kind: 'export',
    label: 'Export',
    state: 'running',
    createdAt: '2026-07-25T10:00:00.000Z',
    updatedAt: '2026-07-25T10:05:00.000Z',
    permittedActions: [],
    ...overrides
  }
}

describe('jobFilterStates', () => {
  it('partitions every backend state across the three counted buckets', () => {
    const assigned = jobCountedFilters.flatMap((filter) => jobFilterStates[filter])
    expect([...assigned].sort()).toEqual([...jobBackendStates].sort())
    expect(new Set(assigned).size).toBe(assigned.length)
  })

  it('gives cancelled and blocked_auth a home so they are reachable', () => {
    expect(jobFilterStates.attention).toContain('blocked_auth')
    expect(jobFilterStates.done).toContain('cancelled')
  })

  it('leaves the all bucket unfiltered', () => {
    expect(jobFilterStates.all).toEqual([])
  })
})

describe('summarizeJobCounts', () => {
  it('renders nothing when counts are missing or empty', () => {
    expect(summarizeJobCounts({ state: 'running', counts: undefined }).mode).toBe('none')
    expect(summarizeJobCounts({ state: 'running', counts: {} }).mode).toBe('none')
  })

  it('renders nothing for a job with zero items rather than dividing by zero', () => {
    const summary = summarizeJobCounts({ state: 'completed', counts: { total: 0 } })
    expect(summary.mode).toBe('none')
    expect(summary.ratio).toBeUndefined()
  })

  it('is indeterminate only while working with an unknown total', () => {
    expect(summarizeJobCounts({ state: 'running', counts: { completed: 3 } }).mode).toBe('indeterminate')
    expect(summarizeJobCounts({ state: 'completed', counts: { completed: 3 } }).mode).toBe('none')
  })

  it('keeps a queued job with a known total determinate at zero', () => {
    const summary = summarizeJobCounts({ state: 'queued', counts: { queued: 4, total: 4 } })
    expect(summary.mode).toBe('determinate')
    expect(summary.ratio).toBe(0)
  })

  it('counts every terminal item state as settled', () => {
    const summary = summarizeJobCounts({ state: 'partial', counts: { completed: 1, failed: 2, cancelled: 1, running: 1, total: 5 } })
    expect(summary.settled).toBe(4)
    expect(summary.ratio).toBe(80)
    expect(summary.failed).toBe(2)
  })

  it('clamps a ratio that overshoots its total', () => {
    expect(summarizeJobCounts({ state: 'completed', counts: { completed: 9, total: 2 } }).ratio).toBe(100)
  })

  it('ignores the legacy succeeded and failures keys', () => {
    const summary = summarizeJobCounts({ state: 'running', counts: { succeeded: 7, failures: 3, total: 10 } })
    expect(summary.completed).toBe(0)
    expect(summary.failed).toBe(0)
    expect(summary.ratio).toBe(0)
  })
})

describe('resolveJobTiming', () => {
  const now = Date.parse('2026-07-25T10:10:00.000Z')

  it('reports a queued job as waiting and anchors on creation', () => {
    const timing = resolveJobTiming(job({ state: 'queued' }), now)
    expect(timing.phase).toBe('waiting')
    expect(timing.anchorIsStart).toBe(false)
    expect(timing.anchor).toBe('2026-07-25T10:00:00.000Z')
    expect(timing.durationMs).toBe(600_000)
  })

  it('measures a running job against the current clock', () => {
    const timing = resolveJobTiming(job({ startedAt: '2026-07-25T10:02:00.000Z' }), now)
    expect(timing.phase).toBe('running')
    expect(timing.anchorIsStart).toBe(true)
    expect(timing.queueWaitMs).toBe(120_000)
    expect(timing.durationMs).toBe(480_000)
  })

  it('measures a finished job between its own start and end', () => {
    const timing = resolveJobTiming(job({ state: 'completed', startedAt: '2026-07-25T10:02:00.000Z', completedAt: '2026-07-25T10:06:00.000Z' }), now)
    expect(timing.phase).toBe('finished')
    expect(timing.durationMs).toBe(240_000)
  })

  it('clamps clock skew instead of reporting a negative duration', () => {
    const timing = resolveJobTiming(job({ state: 'completed', startedAt: '2026-07-25T10:06:00.000Z', completedAt: '2026-07-25T10:02:00.000Z' }), now)
    expect(timing.durationMs).toBe(0)
  })

  it('falls back to the legacy updated-minus-created span without a start', () => {
    const timing = resolveJobTiming(job({ state: 'running' }), now)
    expect(timing.phase).toBe('unknown')
    expect(timing.durationMs).toBe(300_000)
  })

  it('ignores unparseable timestamps', () => {
    const timing = resolveJobTiming(job({ state: 'completed', startedAt: 'not-a-date', completedAt: 'also-not' }), now)
    expect(timing.phase).toBe('unknown')
    expect(timing.startedAt).toBeUndefined()
  })
})
