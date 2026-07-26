import { createElement, type ReactNode } from 'react'
import './presentation.css'

export interface SectionHeaderProps {
  readonly title: string
  readonly description?: string
  readonly titleId?: string
  readonly level?: 2 | 3 | 4
  readonly children?: ReactNode
  readonly className?: string
}

export function SectionHeader({
  title,
  description,
  titleId,
  level = 2,
  children,
  className,
}: SectionHeaderProps) {
  const Heading = `h${level}` as 'h2' | 'h3' | 'h4'
  return (
    <header className={['presentation-section-header', className].filter(Boolean).join(' ')}>
      {createElement(Heading, { id: titleId, className: 'presentation-section-heading' }, title)}
      {description ? <p className="presentation-section-description">{description}</p> : null}
      {children}
    </header>
  )
}
