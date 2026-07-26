import * as React from 'react'
import {
  Field as ShadcnField,
  FieldLabel,
  FieldDescription,
} from '@/components/ui/field'

export interface ControlFieldProps {
  label: string
  description?: string
  isRequired?: boolean
  isOptional?: boolean
  isLabelHidden?: boolean
  htmlFor?: string
  labelId?: string
  descriptionId?: string
  children: React.ReactNode
}

export function ControlField({
  label,
  description,
  isRequired = false,
  isOptional = false,
  isLabelHidden = false,
  htmlFor,
  labelId,
  descriptionId,
  children,
}: ControlFieldProps) {
  return (
    <ShadcnField>
      {isLabelHidden ? null : (
        <FieldLabel id={labelId} htmlFor={htmlFor}>
          {label}
          {isRequired ? <span className="text-destructive">*</span> : null}
          {isOptional ? <span className="text-muted-foreground"> (optional)</span> : null}
        </FieldLabel>
      )}
      {description ? <FieldDescription id={descriptionId}>{description}</FieldDescription> : null}
      {children}
    </ShadcnField>
  )
}

export function useControlId(prefix: string): string {
  const reactId = React.useId()
  return `${prefix}-${reactId.replace(/[:]/g, '')}`
}
