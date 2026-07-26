import { CheckboxInput } from '@/components/controls/CheckboxInput'
import type { ReactNode } from 'react'
import { ActionGroup } from './ActionGroup'
import './presentation.css'

export interface MobileResourceMetadata {
  readonly id: string
  readonly label: string
  readonly value: ReactNode
  readonly fullValue?: string
}

export interface MobileResourceRowProps {
  readonly title: ReactNode
  readonly fullTitle?: string
  readonly description?: ReactNode
  readonly metadata?: readonly MobileResourceMetadata[]
  readonly status?: ReactNode
  readonly actions?: ReactNode
  readonly isSelected?: boolean
  readonly selectionLabel?: string
  readonly onSelectionChange?: (selected: boolean) => void
}

export function MobileResourceRow({
  title,
  fullTitle,
  description,
  metadata = [],
  status,
  actions,
  isSelected = false,
  selectionLabel,
  onSelectionChange
}: MobileResourceRowProps) {
  const selectable = Boolean(selectionLabel && onSelectionChange)
  return (
    <article className="presentation-mobile-row" data-selected={isSelected || undefined}>
      <div className="presentation-mobile-row-header">
        <div className="presentation-mobile-row-identity">
          <div title={fullTitle} className="presentation-mobile-title">{title}</div>
          {description ? <div className="presentation-mobile-description">{description}</div> : null}
        </div>
        {selectable ? (
          <CheckboxInput
            label={selectionLabel ?? ''}
            isLabelHidden
            size="sm"
            value={isSelected}
            onChange={(selected) => onSelectionChange?.(selected)}
          />
        ) : null}
      </div>
      {status}
      {metadata.length > 0 ? (
        <dl className="presentation-mobile-metadata">
          {metadata.map((item) => (
            <div key={item.id} className="presentation-mobile-metadata-item">
              <dt className="presentation-mobile-metadata-label">{item.label}</dt>
              <dd title={item.fullValue} className="presentation-mobile-metadata-value">{item.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      {actions ? <ActionGroup align="start">{actions}</ActionGroup> : null}
    </article>
  )
}
