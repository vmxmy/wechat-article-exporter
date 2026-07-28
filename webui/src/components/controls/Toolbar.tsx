import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

export interface ToolbarProps {
  label: string
  size?: 'sm'
  variant?: 'muted'
  /** Narrow viewport at which the toolbar's groups wrap to stacked rows. */
  stackAt?: 'never' | 'compact' | 'medium'
  startContent?: ReactNode
  endContent?: ReactNode
  children?: ReactNode
  className?: string
}

export function Toolbar({
  label,
  size,
  variant,
  stackAt = 'medium',
  startContent,
  endContent,
  children,
  className,
}: ToolbarProps) {
  return (
    <div
      role="toolbar"
      aria-label={label}
      data-stack-at={stackAt}
      className={cn(
        'presentation-toolbar flex items-center justify-between gap-2',
        size === 'sm' && 'min-h-8 px-2 py-1',
        variant === 'muted' && 'rounded bg-muted/50',
        className,
      )}
    >
      <div className="presentation-toolbar-start flex min-w-0 flex-wrap items-center gap-2">
        {startContent}
        {children}
      </div>
      {endContent ? <div className="presentation-toolbar-end">{endContent}</div> : null}
    </div>
  )
}
