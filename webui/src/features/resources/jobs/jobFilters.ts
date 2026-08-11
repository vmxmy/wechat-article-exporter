export type TaskFilter = 'all' | 'active' | 'attention' | 'done'

export const jobTaskFilters = ['all', 'active', 'attention', 'done'] as const

/** Every job state the backend can return. */
export const jobBackendStates = [
  'queued',
  'running',
  'completed',
  'partial',
  'failed',
  'cancelled',
  'blocked_auth',
  'paused'
] as const

/** The state sets sent to the server as repeated `state=` parameters. The three
    non-`all` buckets partition jobBackendStates exhaustively and without
    overlap, which is what makes `all = active + attention + done` a valid way
    to derive the total tab count from three queries instead of four. A test
    pins that property. */
export const jobFilterStates: Readonly<Record<TaskFilter, readonly string[]>> = Object.freeze({
  all: [],
  active: ['running', 'queued', 'paused'],
  attention: ['failed', 'blocked_auth'],
  done: ['completed', 'partial', 'cancelled']
})

export const jobCountedFilters = ['active', 'attention', 'done'] as const

export type JobCountedFilter = (typeof jobCountedFilters)[number]
