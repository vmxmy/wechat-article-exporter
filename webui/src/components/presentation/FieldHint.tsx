import type { HTMLAttributes, ReactNode } from 'react'
import './presentation-layout.css'

export interface FieldHintProps extends HTMLAttributes<HTMLParagraphElement> {
  /** `error` colours the copy to signal a validation problem. */
  readonly tone?: 'neutral' | 'error'
  readonly children: ReactNode
}

export function FieldHint({ tone = 'neutral', className, children, ...rest }: FieldHintProps) {
  return (
    <p className={joinClasses('presentation-field-hint', className)} data-tone={tone} {...rest}>
      {children}
    </p>
  )
}

function joinClasses(...values: readonly (string | undefined)[]): string {
  return values.filter(Boolean).join(' ')
}
