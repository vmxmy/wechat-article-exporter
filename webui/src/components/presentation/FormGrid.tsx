import type { CSSProperties, ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface FormGridProps {
  /** Target column count at wide widths. Grid auto-fills and drops columns as space shrinks. */
  readonly columns?: number
  /** Minimum child width in px before the grid drops a column. */
  readonly minChildWidth?: number
  /** Gap between cells. `control` matches form rows; `cluster` is roomier. */
  readonly gap?: 'control' | 'cluster'
  /** `horizontal` lays children out in a single flex row; default is the auto-fill grid. */
  readonly direction?: 'vertical' | 'horizontal'
  /** Extra class names for descendant scoping. */
  readonly className?: string
  readonly children: ReactNode
}

/**
 * Responsive auto-fill grid. Each column is at least `minChildWidth` wide and
 * grows to fill; the row caps at `columns` via max-width on the track.
 * `direction="horizontal"` switches to a single flex row (label+control pairs).
 */
export function FormGrid({ columns = 2, minChildWidth = 280, gap = 'control', direction = 'vertical', className, children }: FormGridProps) {
  const style = {
    '--form-grid-min': `${minChildWidth}px`,
    '--form-grid-max': columns,
    '--form-grid-gap': gap === 'control' ? '0.75rem' : '1rem'
  } as CSSProperties
  return (
    <div
      className={cn('presentation-form-grid', direction === 'horizontal' && 'presentation-form-grid-horizontal', className)}
      style={style}
      data-direction={direction}
      {...(direction === 'vertical' ? { 'data-columns': columns } : {})}
    >
      {children}
    </div>
  )
}

export function FormGridFullSpan({ children }: { readonly children: ReactNode }) {
  return <div className="presentation-form-grid-full-span">{children}</div>
}
