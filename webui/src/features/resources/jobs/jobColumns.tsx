import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { formatJobKind } from '../../../lib/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobRecord } from '../../../lib/api'
import { JobDurationCell, JobStartedCell } from './JobTimeCells'
import { JobProgressCell } from './JobProgressCell'
import { JobStateCell } from './JobStateCell'
import { JobTaskCell } from './JobTaskCell'

function taskLabel(job: JobRecord, locale: Locale): string {
  const label = job.label?.trim()
  return label || formatJobKind(job.kind, locale).label
}

/** The primary column keeps `id: 'label'` deliberately: ResourceTable resolves
 *  a row's accessible name through `accessorKey ?? id` against the raw record,
 *  so renaming the id would leave every selection checkbox unnamed.
 *
 *  The deps stay `[locale, messages]`. Anything that changes per tick — the
 *  elapsed clock in particular — must be read inside a cell, never threaded
 *  through here, or useReactTable receives a fresh column array on every tick. */
export function useJobColumns(messages: MessageCatalog, locale: Locale): ColumnDef<JobRecord>[] {
  return useMemo<ColumnDef<JobRecord>[]>(() => {
    const columns = messages.resources.jobs.columns
    return [
      { id: 'label', header: columns.task, meta: { role: 'primaryText' }, cell: ({ row }) => <JobTaskCell job={row.original} locale={locale} /> },
      { accessorKey: 'kind', header: columns.kind, meta: { role: 'secondaryText' }, cell: ({ getValue }) => formatJobKind(getValue<string>(), locale).label },
      { accessorKey: 'state', header: columns.state, meta: { role: 'status' }, cell: ({ row }) => <JobStateCell job={row.original} messages={messages} locale={locale} /> },
      { id: 'progress', header: columns.counts, meta: { role: 'numeric' }, cell: ({ row }) => <JobProgressCell job={row.original} messages={messages} locale={locale} label={taskLabel(row.original, locale)} /> },
      { id: 'started', header: columns.started, meta: { role: 'dateTime' }, cell: ({ row }) => <JobStartedCell job={row.original} messages={messages} locale={locale} /> },
      { id: 'duration', header: columns.duration, meta: { role: 'dateTime' }, cell: ({ row }) => <JobDurationCell job={row.original} messages={messages} locale={locale} /> }
    ]
  }, [locale, messages])
}
