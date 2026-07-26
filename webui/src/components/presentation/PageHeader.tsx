import type { ReactNode } from 'react'
import { ActionGroup } from './ActionGroup'
import { ReadingMeasure } from './LayoutRhythm'
import './presentation.css'

export interface PageHeaderProps {
  readonly title: string
  readonly description?: string
  readonly supportingCopy?: string
  readonly eyebrow?: string
  readonly actions?: ReactNode
  readonly titleId?: string
}

export function PageHeader({ title, description, supportingCopy, eyebrow, actions, titleId }: PageHeaderProps) {
  return (
    <header className="presentation-page-header">
      <ReadingMeasure size="description" className="presentation-page-header-copy">
        {eyebrow ? <p className="presentation-eyebrow">{eyebrow}</p> : null}
        <h1 id={titleId} className="presentation-heading">{title}</h1>
        {description ? <p className="presentation-description">{description}</p> : null}
        {supportingCopy ? <p className="presentation-supporting-copy">{supportingCopy}</p> : null}
      </ReadingMeasure>
      {actions ? <ActionGroup align="start">{actions}</ActionGroup> : null}
    </header>
  )
}
