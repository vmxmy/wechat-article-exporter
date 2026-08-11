import { useRef } from 'react'
import type { MessageCatalog } from '../../../i18n'
import { jobTaskFilters, type TaskFilter } from './jobFilters'
import type { JobFilterCounts } from './useJobFilterCounts'

export interface JobFilterTabsProps {
  readonly value: TaskFilter
  readonly counts: JobFilterCounts
  readonly messages: MessageCatalog
  readonly onChange: (filter: TaskFilter) => void
}

/** A roving-tabindex tablist: only the selected tab is in the tab order, and
    the arrow keys move between tabs, per the WAI-ARIA tabs pattern. */
export function JobFilterTabs({ value, counts, messages, onChange }: JobFilterTabsProps) {
  const copy = messages.resources.jobs
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([])

  function moveFocus(index: number) {
    const next = tabRefs.current[index]
    if (!next) return
    next.focus()
    onChange(jobTaskFilters[index])
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLButtonElement>, index: number) {
    const last = jobTaskFilters.length - 1
    if (event.key === 'ArrowRight') moveFocus(index === last ? 0 : index + 1)
    else if (event.key === 'ArrowLeft') moveFocus(index === 0 ? last : index - 1)
    else if (event.key === 'Home') moveFocus(0)
    else if (event.key === 'End') moveFocus(last)
    else return
    event.preventDefault()
  }

  return (
    <div className="jobs-filter-tabs" role="tablist" aria-label={copy.title}>
      {jobTaskFilters.map((filter, index) => {
        const count = counts[filter]
        const isSelected = value === filter
        return (
          <button
            key={filter}
            ref={(element) => { tabRefs.current[index] = element }}
            type="button"
            role="tab"
            aria-selected={isSelected}
            tabIndex={isSelected ? 0 : -1}
            className={`jobs-filter-tab${isSelected ? ' jobs-filter-tab-active' : ''}`}
            onClick={() => onChange(filter)}
            onKeyDown={(event) => handleKeyDown(event, index)}
          >
            <span>{copy.filterTabs[filter]}</span>
            {typeof count === 'number' ? (
              <span className="jobs-filter-tab-count">{copy.filterTabs.count(count)}</span>
            ) : null}
          </button>
        )
      })}
    </div>
  )
}
