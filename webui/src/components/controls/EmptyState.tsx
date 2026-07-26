import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

export interface EmptyStateProps {
  title: string
  description: string
  actions?: ReactNode
  icon?: ReactNode
  headingLevel?: 2 | 3 | 4 | 5 | 6
  isCompact?: boolean
  className?: string
}

export function EmptyState({
  title,
  description,
  actions,
  icon,
  headingLevel = 2,
  isCompact = false,
  className,
}: EmptyStateProps) {
  const Heading = `h${headingLevel}` as const

  return (
    <div
      className={cn(
        'flex flex-col items-center text-center',
        isCompact ? 'gap-2 px-4 py-6' : 'gap-3 px-6 py-12',
        className,
      )}
    >
      {icon ? (
        <div className={cn('text-muted-foreground', isCompact ? 'mb-1' : 'mb-2')}>
          {icon}
        </div>
      ) : null}
      <Heading className={cn('font-semibold tracking-tight', isCompact ? 'text-base' : 'text-lg')}>
        {title}
      </Heading>
      <p className={cn('text-muted-foreground', isCompact ? 'text-sm' : 'text-sm')}>
        {description}
      </p>
      {actions ? (
        <div className={cn('flex flex-wrap justify-center gap-2', isCompact ? 'mt-1' : 'mt-2')}>
          {actions}
        </div>
      ) : null}
    </div>
  )
}
