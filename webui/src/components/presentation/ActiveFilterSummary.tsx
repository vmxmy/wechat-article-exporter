import { Button } from '@/components/controls/Button'
import { Token } from '@/components/controls/Token'
import './presentation.css'

export interface ActiveFilterItem {
  readonly id: string
  readonly label: string
  readonly removeLabel: string
  readonly onRemove: () => void
}

export interface ActiveFilterSummaryProps {
  readonly label: string
  readonly filters: readonly ActiveFilterItem[]
  readonly clearLabel: string
  readonly onClear: () => void
}

export function ActiveFilterSummary({ label, filters, clearLabel, onClear }: ActiveFilterSummaryProps) {
  if (filters.length === 0) return null
  return (
    <section aria-label={label} className="presentation-active-filters">
      <div className="presentation-active-filter-header">
        <strong>{label}</strong>
        <Button label={clearLabel} variant="ghost" size="sm" onClick={onClear} />
      </div>
      <ul className="presentation-active-filter-list">
        {filters.map((filter) => (
          <li key={filter.id}>
            <Token label={filter.label} size="sm" description={filter.removeLabel} onRemove={filter.onRemove} />
          </li>
        ))}
      </ul>
    </section>
  )
}
