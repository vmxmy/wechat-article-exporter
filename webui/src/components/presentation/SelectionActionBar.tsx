import { Toolbar } from '@/components/controls/Toolbar'
import type { ReactNode } from 'react'
import { ActionGroup } from './ActionGroup'
import './presentation.css'

export interface SelectionActionBarProps {
  readonly selectedCount: number
  readonly countLabel: (count: number) => string
  readonly toolbarLabel: string
  readonly actions: ReactNode
  readonly moreActions?: ReactNode
}

export function SelectionActionBar({ selectedCount, countLabel, toolbarLabel, actions, moreActions }: SelectionActionBarProps) {
  if (selectedCount <= 0) return null
  return (
    <div className="presentation-selection-bar" role="region" aria-label={toolbarLabel} aria-live="polite">
      <Toolbar
        label={toolbarLabel}
        size="sm"
        variant="muted"
        startContent={<strong className="presentation-selection-count">{countLabel(selectedCount)}</strong>}
        endContent={<ActionGroup align="start">{actions}{moreActions}</ActionGroup>}
      />
    </div>
  )
}
