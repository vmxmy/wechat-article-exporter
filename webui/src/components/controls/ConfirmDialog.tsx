import { Button } from './Button'
import { useRef } from 'react'
import type { ReactNode } from 'react'
import { ActionGroup, PresentationDrawer } from '../presentation'

export interface ConfirmDialogProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description?: string
  readonly closeLabel: string
  readonly cancelLabel: string
  readonly actionLabel: string
  readonly onAction: () => void
  readonly isActionLoading?: boolean
  readonly isDestructive?: boolean
  readonly children?: ReactNode
  readonly restoreFocus?: boolean
}

export function ConfirmDialog({
  isOpen,
  onOpenChange,
  title,
  description,
  closeLabel,
  cancelLabel,
  actionLabel,
  onAction,
  isActionLoading = false,
  isDestructive = false,
  children,
  restoreFocus = true
}: ConfirmDialogProps) {
  const actionRef = useRef<HTMLButtonElement>(null)
  const close = () => onOpenChange(false)

  return (
    <PresentationDrawer
      isOpen={isOpen}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      closeLabel={closeLabel}
      role="alertdialog"
      initialFocusRef={actionRef}
      restoreFocus={restoreFocus}
      footer={
        <ActionGroup align="end" gap="control">
          <Button
            ref={actionRef}
            label={actionLabel}
            variant={isDestructive ? 'destructive' : 'primary'}
            isLoading={isActionLoading}
            onClick={() => {
              onAction()
              close()
            }}
          />
          <Button label={cancelLabel} variant="secondary" isDisabled={isActionLoading} onClick={close} />
        </ActionGroup>
      }
    >
      {children}
    </PresentationDrawer>
  )
}
