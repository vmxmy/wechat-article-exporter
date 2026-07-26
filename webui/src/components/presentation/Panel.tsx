import type { HTMLAttributes } from 'react'
import './presentation-layout.css'

export interface PanelProps extends HTMLAttributes<HTMLElement> {
  /** Element type. `section` for landmark regions, `div` otherwise. */
  readonly as?: 'section' | 'div'
  /** Surface treatment. `surface` is the default elevated card. */
  readonly tone?: 'surface' | 'muted' | 'accent'
  /** Inner padding. `none` when the caller manages its own spacing. */
  readonly padding?: 'default' | 'none'
}

export function Panel({ as = 'section', tone = 'surface', padding = 'default', className, children, ...rest }: PanelProps) {
  const Tag = as
  return (
    <Tag
      className={joinClasses('presentation-panel', className)}
      data-tone={tone}
      data-padding={padding}
      {...rest}
    >
      {children}
    </Tag>
  )
}

function joinClasses(...values: readonly (string | undefined)[]): string {
  return values.filter(Boolean).join(' ')
}
