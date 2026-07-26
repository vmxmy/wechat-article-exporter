import type * as React from 'react'

import { cn } from '@/lib/utils'

export interface LinkProps extends Omit<React.ComponentProps<'a'>, 'onClick'> {
  href: string
  children: React.ReactNode
  isStandalone?: boolean
  hasUnderline?: boolean
  onClick?: (event: React.MouseEvent<HTMLAnchorElement>) => void
  className?: string
}

export function Link({
  className,
  hasUnderline = false,
  isStandalone = false,
  ...props
}: LinkProps) {
  return (
    <a
      className={cn(
        'text-primary underline-offset-4',
        hasUnderline && 'underline',
        isStandalone && !hasUnderline && 'hover:underline',
        className,
      )}
      {...props}
    />
  )
}
