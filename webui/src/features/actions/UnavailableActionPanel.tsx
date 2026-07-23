import type { ReactNode } from 'react'
import type { MessageCatalog } from '../../i18n'

interface UnavailableActionPanelProps {
  readonly messages: MessageCatalog
  readonly title: string
  readonly description: string
  readonly children: ReactNode
  readonly availabilityNote?: string
  readonly showConfirmationNote?: boolean
}

export function UnavailableActionPanel({ messages, title, description, children, availabilityNote, showConfirmationNote = false }: UnavailableActionPanelProps) {
  const titleId = `${title.replaceAll(' ', '-').toLowerCase()}-actions-title`
  return (
    <section className="unavailable-actions" aria-labelledby={titleId}>
      <div>
        <h2 id={titleId}>{title}</h2>
        <p>{description}</p>
      </div>
      <div className="action-button-group" aria-describedby={availabilityNote ? 'unavailable-api-note' : undefined}>
        {children}
      </div>
      {availabilityNote ? <p id="unavailable-api-note" className="availability-note">{availabilityNote}</p> : null}
      {showConfirmationNote ? <details className="confirmation-note">
        <summary>{messages.unavailableActions.confirmationTitle}</summary>
        <p>{messages.unavailableActions.confirmationDescription}</p>
      </details> : null}
    </section>
  )
}
