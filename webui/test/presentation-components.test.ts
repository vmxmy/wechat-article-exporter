import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ActiveFilterSummary } from '../src/components/presentation/ActiveFilterSummary'
import { ContentCluster, DenseRegion, PageStack, ReadingMeasure, SectionStack } from '../src/components/presentation/LayoutRhythm'
import { MobileResourceRow } from '../src/components/presentation/MobileResourceRow'
import { PageHeader } from '../src/components/presentation/PageHeader'
import { SelectionActionBar } from '../src/components/presentation/SelectionActionBar'
import { Status } from '../src/components/presentation/Status'
import { TechnicalDetails } from '../src/components/presentation/TechnicalDetails'

describe('presentation component accessibility', () => {
  it('renders shared layout rhythm primitives with semantic classes', () => {
    const markup = renderToStaticMarkup(
      createElement(PageStack, { gap: 'subsection' },
        createElement(ReadingMeasure, { size: 'description' }, 'Readable copy'),
        createElement(SectionStack, { as: 'section', gap: 'cluster' },
          createElement(ContentCluster, { justify: 'between' }, createElement('span', null, 'Actions')),
          createElement(DenseRegion, { 'aria-label': 'Results' }, createElement('span', null, 'Rows'))
        )
      )
    )
    expect(markup).toContain('layout-page-stack--subsection')
    expect(markup).toContain('layout-reading-measure--description')
    expect(markup).toContain('layout-section-stack--cluster')
    expect(markup).toContain('layout-content-cluster--justify-between')
    expect(markup).toContain('layout-dense-region')
    expect(markup).toContain('aria-label="Results"')
  })

  it('renders a unique page heading and contextual description', () => {
    const markup = renderToStaticMarkup(createElement(PageHeader, { title: 'Articles', description: 'Browse saved content' }))
    expect(markup.match(/<h1/g)).toHaveLength(1)
    expect(markup).toContain('Browse saved content')
  })

  it('pairs semantic status color with visible text and an accessible label', () => {
    const markup = renderToStaticMarkup(createElement(Status, { value: 'running', locale: 'en' }))
    expect(markup).toContain('Running')
    expect(markup).toContain('aria-label="Running"')
    expect(markup).toContain('data-status-value="running"')
  })

  it('does not expose an unknown backend enum through visible status markup', () => {
    const markup = renderToStaticMarkup(createElement(Status, { value: 'future_backend_state', locale: 'en' }))
    expect(markup).toContain('—')
    expect(markup).not.toContain('future_backend_state')
  })

  it('keeps exact technical values in code and gives copy controls explicit names', () => {
    const exact = '11111111-1111-1111-1111-111111111111'
    const markup = renderToStaticMarkup(createElement(TechnicalDetails, {
      label: 'Technical details',
      items: [{ label: 'Job ID', value: exact, copyLabel: 'Copy job ID' }],
      defaultIsOpen: true
    }))
    expect(markup).toContain(exact)
    expect(markup).toContain('Copy job ID')
    expect(markup).toContain('<code')
    expect(markup).toContain('translate="no"')
  })

  it('labels active-filter removal and clear-all controls', () => {
    const markup = renderToStaticMarkup(createElement(ActiveFilterSummary, {
      label: 'Active filters',
      clearLabel: 'Clear all filters',
      onClear: () => undefined,
      filters: [{ id: 'account', label: 'Account: Fixture', removeLabel: 'Remove account filter', onRemove: () => undefined }]
    }))
    expect(markup).toContain('aria-label="Active filters"')
    expect(markup).toContain('Clear all filters')
    expect(markup).toContain('Remove account filter')
  })

  it('announces contextual selection actions only when resources are selected', () => {
    const props = {
      countLabel: (count: number) => `${count} selected`,
      toolbarLabel: 'Selection actions',
      actions: createElement('button', null, 'Export')
    }
    const hidden = renderToStaticMarkup(createElement(SelectionActionBar, { ...props, selectedCount: 0 }))
    const visible = renderToStaticMarkup(createElement(SelectionActionBar, { ...props, selectedCount: 2 }))
    expect(hidden).toBe('')
    expect(visible).toContain('aria-label="Selection actions"')
    expect(visible).toContain('aria-live="polite"')
    expect(visible).toContain('2 selected')
  })

  it('gives mobile selection a resource-specific accessible name and exposes full text', () => {
    const markup = renderToStaticMarkup(createElement(MobileResourceRow, {
      title: 'Short title',
      fullTitle: 'The complete article title',
      selectionLabel: 'Select The complete article title',
      onSelectionChange: () => undefined,
      metadata: [{ id: 'published', label: 'Published', value: 'Jul 24, 2026', fullValue: '2026-07-24T10:00:00Z' }]
    }))
    expect(markup).toContain('Select The complete article title')
    expect(markup).toContain('title="The complete article title"')
    expect(markup).toContain('title="2026-07-24T10:00:00Z"')
  })
})
