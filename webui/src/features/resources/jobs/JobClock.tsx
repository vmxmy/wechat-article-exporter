import { useEffect, useState, type ReactNode } from 'react'
import { JobClockContext } from './jobClockContext'

const tickInterval = 15_000

/** One shared tick for every elapsed-time cell on the page.
 *
 *  Elapsed durations must keep moving between the 5s data polls: React Query
 *  shares structure, so an unchanged payload does not re-render. The tick lives
 *  in context rather than in the column definitions on purpose — feeding `now`
 *  into the `columns` memo would hand useReactTable a new array every interval,
 *  which is the shape of the known auto-reset render loop. */
export function JobClockProvider({ children }: { readonly children: ReactNode }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), tickInterval)
    return () => window.clearInterval(timer)
  }, [])
  return <JobClockContext.Provider value={now}>{children}</JobClockContext.Provider>
}
