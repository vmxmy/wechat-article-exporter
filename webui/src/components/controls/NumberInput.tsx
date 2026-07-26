import * as React from 'react'
import { Input } from '@/components/ui/input'
import { Icons } from '@/components/icons'
import { ControlField, useControlId } from './field-context'
import { cn } from '@/lib/utils'

export interface NumberInputProps {
  label: string
  value: number | null | undefined
  onChange: (value: number | null) => void
  description?: string
  placeholder?: string
  htmlName?: string
  autoComplete?: 'off' | 'on'
  min?: number
  max?: number
  step?: number
  isIntegerOnly?: boolean
  hasClear?: boolean
  isRequired?: boolean
  isOptional?: boolean
  isLabelHidden?: boolean
  isDisabled?: boolean
  className?: string
}

export function NumberInput({
  label,
  value,
  onChange,
  description,
  placeholder,
  htmlName,
  autoComplete,
  min,
  max,
  step = 1,
  isIntegerOnly = false,
  hasClear = false,
  isRequired = false,
  isOptional = false,
  isLabelHidden = false,
  isDisabled = false,
  className,
}: NumberInputProps) {
  const id = useControlId('number-input')
  const inputId = htmlName ?? id

  const parse = React.useCallback(
    (raw: string): number | null => {
      if (raw.trim() === '') return null
      const parsed = isIntegerOnly ? Number.parseInt(raw, 10) : Number.parseFloat(raw)
      if (!Number.isFinite(parsed)) return null
      if (isIntegerOnly && !Number.isInteger(parsed)) return parsed
      return parsed
    },
    [isIntegerOnly],
  )

  const displayValue = value === null || value === undefined ? '' : String(value)
  const showClear = hasClear && (value !== null && value !== undefined)

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
          type="number"
          inputMode={isIntegerOnly ? 'numeric' : 'decimal'}
          value={displayValue}
          onChange={(event) => onChange(parse(event.currentTarget.value))}
          placeholder={placeholder}
          autoComplete={autoComplete}
          min={min}
          max={max}
          step={step}
          required={isRequired || undefined}
          disabled={isDisabled}
          className={cn(showClear ? 'pr-9' : null, className)}
        />
        {showClear ? (
          <button
            type="button"
            aria-label="Clear"
            onClick={() => onChange(null)}
            className="absolute top-1/2 right-2.5 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <Icons.close className="size-4" />
          </button>
        ) : null}
      </div>
    </ControlField>
  )
}
