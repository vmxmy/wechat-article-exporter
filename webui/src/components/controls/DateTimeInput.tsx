import { Input } from '@/components/ui/input'
import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import { ControlField, useControlId } from './field-context'

export type ISODateTimeString = string

export interface DateTimeInputProps {
  label: string
  value: ISODateTimeString | undefined
  onChange: (value: ISODateTimeString | undefined) => void
  hasClear?: boolean
  hourFormat?: '24h'
  timeIncrement?: number
  description?: string
  isRequired?: boolean
  isOptional?: boolean
  isLabelHidden?: boolean
  isDisabled?: boolean
  className?: string
}

function isoToLocalInputValue(value: ISODateTimeString | undefined): string {
  if (!value) return ''

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''

  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function localInputValueToIso(value: string): ISODateTimeString | undefined {
  if (!value) return undefined

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined

  return date.toISOString()
}

export function DateTimeInput({
  label,
  value,
  onChange,
  hasClear = false,
  timeIncrement,
  description,
  isRequired = false,
  isOptional = false,
  isLabelHidden = false,
  isDisabled = false,
  className,
}: DateTimeInputProps) {
  const inputId = useControlId('datetime-input')
  const showClear = hasClear && value !== undefined

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
          type="datetime-local"
          value={isoToLocalInputValue(value)}
          onChange={(event) => onChange(localInputValueToIso(event.currentTarget.value))}
          step={timeIncrement === undefined ? undefined : timeIncrement * 60}
          required={isRequired || undefined}
          disabled={isDisabled}
          className={cn(showClear ? 'pr-9' : null, className)}
        />
        {showClear ? (
          <button
            type="button"
            aria-label="Clear"
            onClick={() => onChange(undefined)}
            disabled={isDisabled}
            className="absolute top-1/2 right-2.5 -translate-y-1/2 text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
          >
            <Icons.close className="size-4" />
          </button>
        ) : null}
      </div>
    </ControlField>
  )
}
