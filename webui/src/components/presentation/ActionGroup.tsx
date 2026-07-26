import type { HTMLAttributes } from 'react'
import './presentation-layout.css'

export interface ActionGroupProps extends HTMLAttributes<HTMLDivElement> {
  /** Inline alignment along the main axis. */
  readonly align?: 'start' | 'center' | 'end'
  /** Gap between controls. `control` is tight (form rows), `cluster` is roomier. */
  readonly gap?: 'control' | 'cluster'
  /** Narrow viewport at which the group collapses to a vertical stack. */
  readonly stackAt?: 'never' | 'compact' | 'medium'
  /** When stacked, stretch children to the group width (full-width buttons). */
  readonly stretchOnStack?: boolean
  /** Prevent wrapping on the main axis (e.g. inside a table cell). */
  readonly nowrap?: boolean
}

export function ActionGroup({
  align = 'end',
  gap = 'control',
  stackAt = 'never',
  stretchOnStack = false,
  nowrap = false,
  className,
  children,
  ...rest
}: ActionGroupProps) {
  return (
    <div
      className={joinClasses('presentation-action-group', className)}
      data-align={align}
      data-gap={gap}
      data-stack-at={stackAt}
      data-stretch-on-stack={stretchOnStack ? 'true' : 'false'}
      data-nowrap={nowrap ? 'true' : 'false'}
      {...rest}
    >
      {children}
    </div>
  )
}

function joinClasses(...values: readonly (string | undefined)[]): string {
  return values.filter(Boolean).join(' ')
}
