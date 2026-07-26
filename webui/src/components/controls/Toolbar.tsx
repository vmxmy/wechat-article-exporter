import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

export interface ToolbarProps {
  label: string
  size?: 'sm'
  variant?: 'muted'
  startContent?: ReactNode
  endContent?: ReactNode
  children?: ReactNode
  className?: string
}

export function Toolbar({
  label,
  size,
  variant,
  startContent,
  endContent,
  children,
  className,
}: ToolbarProps) {
  return (
    <div
      role="toolbar"
      aria-label={label}
      className={cn(
        'flex items-center justify-between gap-2',
        size === 'sm' && 'min-h-8 px-2 py-1',
        variant === 'muted' && 'rounded bg-muted/50',
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        {startContent}
        {children}
      </div>
      {endContent}
    </div>
  )
}
