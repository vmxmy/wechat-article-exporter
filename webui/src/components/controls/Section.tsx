import type { ReactNode } from 'react'

export interface SectionProps {
  id?: string
  className?: string
  variant?: 'transparent'
  dividers?: Array<'top' | 'bottom'>
  padding?: number
  'aria-labelledby'?: string
  children: ReactNode
}

export function Section({
  id,
  className,
  variant: _variant,
  dividers,
  padding: _padding,
  'aria-labelledby': ariaLabelledby,
  children,
}: SectionProps) {
  const sectionClassName = [
    className,
    dividers?.includes('top') ? 'border-t' : undefined,
    dividers?.includes('bottom') ? 'border-b' : undefined,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <section
      id={id}
      aria-labelledby={ariaLabelledby}
      className={sectionClassName || undefined}
    >
      {children}
    </section>
  )
}
