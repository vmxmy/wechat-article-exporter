import { Textarea } from '@/components/ui/textarea'
import { ControlField, useControlId } from './field-context'
import { cn } from '@/lib/utils'

export interface TextAreaProps {
  label: string
  value: string
  onChange: (value: string) => void
  rows?: number
  hasSpellCheck?: boolean
  htmlName?: string
  placeholder?: string
  description?: string
  isRequired?: boolean
  isOptional?: boolean
  isLabelHidden?: boolean
  isDisabled?: boolean
  className?: string
}

export function TextArea({
  label,
  value,
  onChange,
  rows = 4,
  hasSpellCheck = true,
  htmlName,
  placeholder,
  description,
  isRequired = false,
  isOptional = false,
  isLabelHidden = false,
  isDisabled = false,
  className,
}: TextAreaProps) {
  const id = useControlId('text-area')
  const fieldId = htmlName ?? id

  return (
    <ControlField
      label={label}
      description={description}
      isRequired={isRequired}
      isOptional={isOptional}
      isLabelHidden={isLabelHidden}
      htmlFor={fieldId}
    >
      <Textarea
        id={fieldId}
        name={htmlName}
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
        rows={rows}
        spellCheck={hasSpellCheck}
        placeholder={placeholder}
        required={isRequired || undefined}
        disabled={isDisabled}
        className={cn(className)}
      />
    </ControlField>
  )
}
