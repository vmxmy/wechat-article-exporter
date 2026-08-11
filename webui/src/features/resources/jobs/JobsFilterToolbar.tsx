import { Toolbar } from '@/components/controls/Toolbar'
import { Selector } from '@/components/controls/Selector'
import { ActiveFilterSummary } from '../../../components/presentation'
import { listJobKinds } from '../../../lib/presentation'
import { useMemo } from 'react'
import type { Locale, MessageCatalog } from '../../../i18n'
import { JobFilterTabs } from './JobFilterTabs'
import type { TaskFilter } from './jobFilters'
import type { JobFilterCounts } from './useJobFilterCounts'

export interface JobsFilterToolbarProps {
  readonly taskFilter: TaskFilter
  readonly kind: string | undefined
  readonly counts: JobFilterCounts
  readonly messages: MessageCatalog
  readonly locale: Locale
  readonly onTaskFilterChange: (filter: TaskFilter) => void
  readonly onKindChange: (kind: string | undefined) => void
}

export function JobsFilterToolbar({ taskFilter, kind, counts, messages, locale, onTaskFilterChange, onKindChange }: JobsFilterToolbarProps) {
  const copy = messages.resources.jobs
  const kindOptions = useMemo(() => listJobKinds(locale).map((option) => ({ value: option.value, label: option.label })), [locale])
  const kindLabel = kindOptions.find((option) => option.value === kind)?.label

  return (
    <>
      <Toolbar className="jobs-filter-toolbar" label={copy.filterToolbarLabel} stackAt="medium">
        <JobFilterTabs value={taskFilter} counts={counts} messages={messages} onChange={onTaskFilterChange} />
        <div className="jobs-filter-kind">
          <Selector
            label={copy.filters.kind}
            isLabelHidden
            size="sm"
            options={kindOptions}
            value={kind ?? null}
            placeholder={copy.filters.allKinds}
            hasClear
            clearLabel={copy.filters.clearKind}
            onChange={(value) => onKindChange(value ?? undefined)}
          />
        </div>
      </Toolbar>
      {kind && kindLabel ? (
        <ActiveFilterSummary
          label={copy.filters.appliedFilters}
          clearLabel={copy.filters.clearFilters}
          onClear={() => onKindChange(undefined)}
          filters={[{ id: 'kind', label: kindLabel, removeLabel: copy.filters.removeFilter(kindLabel), onRemove: () => onKindChange(undefined) }]}
        />
      ) : null}
    </>
  )
}
