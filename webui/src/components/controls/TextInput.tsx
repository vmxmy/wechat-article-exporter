import { Input } from '@/components/ui/input'
import { Icons } from '@/components/icons'
import { ControlField, useControlId } from './field-context'
import { cn } from '@/lib/utils'

export interface TextInputProps {
  label: string
  value: string
  onChange: (value: string) => void
  description?: string
  placeholder?: string
  htmlName?: string
  type?: 'text' | 'password' | 'email' | 'url' | 'search'
  autoComplete?: string
  isRequired?: boolean
  isOptional?: boolean
  isLabelHidden?: boolean
  hasAutoFocus?: boolean
  hasClear?: boolean
  isDisabled?: boolean
  className?: string
}

export function TextInput({
  label,
  value,
  onChange,
  description,
  placeholder,
  htmlName,
  type = 'text',
  autoComplete,
  isRequired = false,
  isOptional = false,
  isLabelHidden = false,
  hasAutoFocus = false,
  hasClear = false,
  isDisabled = false,
  className,
}: TextInputProps) {
  const id = useControlId('text-input')
  const inputId = htmlName ?? id

  return (
    <ControlField
      label={label}
      description={description}
      isRequired={isRequired}
      isOptional={isOptional}
      isLabelHidden={isLabelHidden}
      htmlFor={inputId}
    >
      <div className="relative">
        <Input
          id={inputId}
          name={htmlName}
          type={type}
          value={value}
          onChange={(event) => onChange(event.currentTarget.value)}
          placeholder={placeholder}
          autoComplete={autoComplete}
          required={isRequired || undefined}
          autoFocus={hasAutoFocus}
          disabled={isDisabled}
          className={cn(hasClear && value ? 'pr-9' : null, className)}
        />
        {hasClear && value ? (
          <button
            type="button"
            aria-label="Clear"
            onClick={() => onChange('')}
            className="absolute top-1/2 right-2.5 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <Icons.close className="size-4" />
          </button>
        ) : null}
      </div>
    </ControlField>
  )
}
