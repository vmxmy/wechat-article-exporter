import { describe, expect, it } from 'vitest'
import { getResourceColumnPresentation, projectResourceToMobile, type ResourceColumnDefinition } from '../src/lib/presentation/resourceColumns'

describe('resource column presentation', () => {
  it('derives alignment, truncation, numeric treatment, and full-value access by role', () => {
    expect(getResourceColumnPresentation('primaryText')).toMatchObject({ alignment: 'start', maxLines: 2, truncate: true, exposeFullValue: true })
    expect(getResourceColumnPresentation('numeric')).toMatchObject({ alignment: 'end', numeric: true, truncate: false })
    expect(getResourceColumnPresentation('actions')).toMatchObject({ alignment: 'end', mobilePlacement: 'actions', exposeFullValue: false })
  })

  it('projects one shared row model into mobile identity, metadata, status, and actions', () => {
    const columns = [
      { key: 'title', label: 'Title', role: 'primaryText' },
      { key: 'account', label: 'Account', role: 'secondaryText' },
      { key: 'reads', label: 'Reads', role: 'numeric' },
      { key: 'published', label: 'Published', role: 'dateTime' },
      { key: 'status', label: 'Status', role: 'status' },
      { key: 'actions', label: 'Actions', role: 'actions' },
      { key: 'debug', label: 'Debug', role: 'identifier', hideOnMobile: true }
    ] as const satisfies readonly ResourceColumnDefinition[]

    const projection = projectResourceToMobile(columns, {
      title: 'A long article title',
      account: 'Fixture account',
      reads: '1,234',
      published: 'Jul 24, 2026',
      status: 'Ready',
      actions: 'Open',
      debug: 'article-1'
    })

    expect(projection.primary?.value).toBe('A long article title')
    expect(projection.secondary.map((field) => field.value)).toEqual(['Fixture account'])
    expect(projection.metadata.map((field) => field.key)).toEqual(['reads', 'published'])
    expect(projection.status?.value).toBe('Ready')
    expect(projection.actions?.value).toBe('Open')
    expect(JSON.stringify(projection)).not.toContain('article-1')
  })
})
