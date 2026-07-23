import { describe, expect, it, vi } from 'vitest'
import { handoffCreatedJob, jobHandoffLocation, loadJobHandoff, saveJobHandoff } from '../src/lib/jobHandoff'

function createStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value)
  }
}

describe('created job handoff', () => {
  it('uses an encoded stable jobs URL', () => {
    expect(jobHandoffLocation('job / fixture?one')).toBe('/jobs?job=job%20%2F%20fixture%3Fone')
  })

  it('persists only a non-empty job ID', () => {
    const storage = createStorage()

    saveJobHandoff({ id: ' job-1 ' }, storage)
    expect(loadJobHandoff(storage)).toEqual({ id: 'job-1' })

    saveJobHandoff({ id: '   ' }, storage)
    expect(loadJobHandoff(storage)).toEqual({ id: 'job-1' })
  })

  it('persists the handoff before navigating to jobs', () => {
    const storage = createStorage()
    const navigate = vi.fn()

    handoffCreatedJob({ id: 'job-1' }, { storage, navigate })

    expect(loadJobHandoff(storage)).toEqual({ id: 'job-1' })
    expect(navigate).toHaveBeenCalledWith('/jobs?job=job-1')
  })
})
