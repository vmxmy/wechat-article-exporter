import { Button } from '@/components/controls/Button'
import type { ReactNode } from 'react'
import { ActionGroup } from './ActionGroup'
import { PresentationDrawer } from './PresentationDrawer'

export interface FormDrawerProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description?: string
  readonly closeLabel: string
  /** id of the <form> rendered inside children; the footer submit targets it via form={formId}. */
  readonly formId: string
  readonly submitLabel: string
  readonly isSubmitting?: boolean
  readonly canSubmit?: boolean
  readonly submitVariant?: 'primary' | 'destructive'
  /** Secondary footer controls (cancel, delete, etc.). Rendered before the submit button. */
  readonly footerSecondary?: ReactNode
  readonly children: ReactNode
}

export function FormDrawer({
  isOpen,
  onOpenChange,
  title,
  description,
  closeLabel,
  formId,
  submitLabel,
  isSubmitting = false,
  canSubmit = true,
  submitVariant = 'primary',
  footerSecondary,
  children
}: FormDrawerProps) {
  return (
    <PresentationDrawer
      isOpen={isOpen}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      closeLabel={closeLabel}
      bodyClassName="presentation-detail-body"
      footer={
        <ActionGroup align="end" gap="control">
          {footerSecondary}
          <Button
            label={submitLabel}
            type="submit"
            form={formId}
            variant={submitVariant}
            isLoading={isSubmitting}
            isDisabled={!canSubmit}
          />
        </ActionGroup>
      }
    >
      {children}
    </PresentationDrawer>
  )
}
