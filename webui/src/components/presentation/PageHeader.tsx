import type { ReactNode } from 'react'
import './presentation.css'

export interface PageHeaderProps {
  readonly title: string
  readonly description?: string
  readonly eyebrow?: string
  readonly actions?: ReactNode
  readonly titleId?: string
}

export function PageHeader({ title, description, eyebrow, actions, titleId }: PageHeaderProps) {
  return (
    <header className="presentation-page-header">
      <div className="presentation-page-header-copy">
        {eyebrow ? <p className="presentation-eyebrow">{eyebrow}</p> : null}
        <h1 id={titleId} className="presentation-heading">{title}</h1>
        {description ? <p className="presentation-description">{description}</p> : null}
      </div>
      {actions ? <div className="presentation-actions">{actions}</div> : null}
    </header>
  )
}
