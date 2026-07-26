import type { ReactNode } from 'react'
import { ActionGroup } from './ActionGroup'
import { PresentationDrawer } from './PresentationDrawer'

export interface DetailPanelProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description?: string
  /** The accessible dialog name. Defaults to the visible title. */
  readonly ariaLabel?: string
  readonly children: ReactNode
  readonly footer?: ReactNode
  readonly width?: number | string
  readonly closeLabel: string
}

export function DetailPanel({
  isOpen,
  onOpenChange,
  title,
  description,
  ariaLabel,
  children,
  footer,
  width = 'min(36rem, 100vw)',
  closeLabel
}: DetailPanelProps) {
  return (
    <PresentationDrawer
      isOpen={isOpen}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      ariaLabel={ariaLabel}
      closeLabel={closeLabel}
      width={width}
      bodyClassName="presentation-detail-body"
      footer={footer ? <ActionGroup align="start">{footer}</ActionGroup> : undefined}
    >
      {children}
    </PresentationDrawer>
  )
}
