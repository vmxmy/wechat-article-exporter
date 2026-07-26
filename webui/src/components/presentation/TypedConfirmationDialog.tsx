import { Button } from '@/components/controls/Button'
import { TextInput } from '@/components/controls/TextInput'
import type { FormEvent, RefObject } from 'react'
import { ActionGroup } from './ActionGroup'
import { PresentationDrawer } from './PresentationDrawer'
import './presentation-layout.css'

export interface TypedConfirmationDialogProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description: string
  readonly closeLabel: string
  /** The exact string the user must retype to enable the action. */
  readonly expected: string
  readonly inputLabel: string
  readonly inputHint: string
  readonly actionLabel: string
  readonly cancelLabel: string
  readonly confirmation: string
  readonly onConfirmationChange: (value: string) => void
  readonly isActionLoading: boolean
  readonly onAction: () => void
  /** Element to return focus to when the dialog closes (e.g. the triggering button). */
  readonly triggerRef?: RefObject<HTMLElement | null>
}

export function TypedConfirmationDialog({
  isOpen,
  onOpenChange,
  title,
  description,
  closeLabel,
  expected,
  inputLabel,
  inputHint,
  actionLabel,
  cancelLabel,
  confirmation,
  onConfirmationChange,
  isActionLoading,
  onAction,
  triggerRef
}: TypedConfirmationDialogProps) {
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (confirmation === expected) onAction()
  }

  return (
    <PresentationDrawer
      isOpen={isOpen}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      closeLabel={closeLabel}
      role="alertdialog"
      restoreFocusRef={triggerRef}
      footer={
        <ActionGroup align="end" gap="control">
          <Button label={actionLabel} variant="destructive" type="submit" form="typed-confirmation-form" isLoading={isActionLoading} isDisabled={confirmation !== expected} />
          <Button label={cancelLabel} variant="secondary" isDisabled={isActionLoading} onClick={() => onOpenChange(false)} />
        </ActionGroup>
      }
    >
      <form id="typed-confirmation-form" className="typed-confirmation-dialog" onSubmit={submit}>
        <div className="confirmation-proof">
          <strong>{inputLabel}</strong>
          <code translate="no">{expected}</code>
          <p>{inputHint}</p>
        </div>
        <TextInput label={inputLabel} value={confirmation} onChange={onConfirmationChange} isRequired hasAutoFocus />
      </form>
    </PresentationDrawer>
  )
}
