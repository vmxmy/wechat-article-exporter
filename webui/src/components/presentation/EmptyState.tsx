import { EmptyState as AstryxEmptyState } from '@astryxdesign/core/EmptyState'
import type { ReactNode } from 'react'

export interface EmptyStateProps {
  readonly title: string
  readonly description: string
  readonly actions?: ReactNode
  readonly icon?: ReactNode
  readonly headingLevel?: 2 | 3 | 4 | 5 | 6
  readonly isCompact?: boolean
}

export function EmptyState({ title, description, actions, icon, headingLevel = 2, isCompact = false }: EmptyStateProps) {
  return (
    <AstryxEmptyState
      title={title}
      description={description}
      actions={actions}
      icon={icon}
      headingLevel={headingLevel}
      isCompact={isCompact}
    />
  )
}
