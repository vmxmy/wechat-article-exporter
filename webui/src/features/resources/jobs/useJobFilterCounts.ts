import { useQueries } from '@tanstack/react-query'
import { getJobPage } from '../../../lib/api'
import { queryKeys } from '../../../lib/queries'
import { jobCountedFilters, jobFilterStates, type TaskFilter } from './jobFilters'

export type JobFilterCounts = Readonly<Partial<Record<TaskFilter, number>>>

const countPolling = 15_000

/** Reads each tab's true total from the server with a `limit=1` probe.
 *
 *  The counts used to be derived from whichever 25 rows happened to be on
 *  screen, so they were wrong on every page but the first. Only three queries
 *  run: the buckets partition every backend state, so `all` is their sum.
 *
 *  A bucket whose query fails stays `undefined` and renders no badge — showing
 *  `0` would read as "nothing to do here", which is the opposite of unknown. */
export function useJobFilterCounts(kind: string | undefined): JobFilterCounts {
  return useQueries({
    queries: jobCountedFilters.map((filter) => ({
      queryKey: queryKeys.jobCounts(kind, filter),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        getJobPage({ page: 1, pageSize: 1, kind, states: jobFilterStates[filter] }, signal),
      refetchInterval: countPolling,
      refetchIntervalInBackground: false
    })),
    // This object is rebuilt on every render and feeds the tab badges only. It
    // must never reach the table's row data, which would churn its identity.
    combine: (results): JobFilterCounts => {
      const counts: Partial<Record<TaskFilter, number>> = {}
      let all = 0
      let allKnown = true
      results.forEach((result, index) => {
        const total = result.data?.pagination.total
        if (typeof total === 'number') {
          counts[jobCountedFilters[index]] = total
          all += total
        } else {
          allKnown = false
        }
      })
      if (allKnown) counts.all = all
      return counts
    }
  })
}
