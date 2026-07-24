import { Dialog, DialogHeader } from '@astryxdesign/core/Dialog'
import type { ReactNode } from 'react'
import './presentation.css'

export interface DetailPanelProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description?: string
  /**
   * The accessible dialog name. Defaults to the visible title so the panel is
   * useful for both human reading and direct keyboard/screen-reader lookup.
   */
  readonly ariaLabel?: string
  readonly children: ReactNode
  readonly footer?: ReactNode
  readonly width?: number | string
}

export function DetailPanel({ isOpen, onOpenChange, title, description, ariaLabel = title, children, footer, width = 'min(36rem, 100vw)' }: DetailPanelProps) {
  return (
    <Dialog isOpen={isOpen} onOpenChange={onOpenChange} aria-label={ariaLabel} width={width} maxHeight="100dvh" position={{ top: 0, right: 0 }} purpose="info">
      <DialogHeader title={title} subtitle={description} onOpenChange={onOpenChange} />
      <div className="presentation-detail-body">{children}</div>
      {footer ? <footer className="presentation-actions">{footer}</footer> : null}
    </Dialog>
  )
}
