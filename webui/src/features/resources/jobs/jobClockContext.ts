import { createContext, useContext } from 'react'

/** Read once at module load, never during render, so a cell used outside the
    provider (static rendering, unit tests) still gets a sane clock without
    calling an impure function mid-render. */
const fallbackNow = Date.now()

export const JobClockContext = createContext<number>(fallbackNow)

export function useJobClock(): number {
  return useContext(JobClockContext)
}
