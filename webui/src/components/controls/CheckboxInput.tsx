import { Checkbox } from '@/components/ui/checkbox'
import { ControlField, useControlId } from './field-context'
import { cn } from '@/lib/utils'

export type CheckboxValue = boolean | 'indeterminate'

export interface CheckboxInputProps {
  label: string
  value: CheckboxValue
  onChange: (checked: boolean) => void
  htmlName?: string
  description?: string
  isLabelHidden?: boolean
  isDisabled?: boolean
  isRequired?: boolean
  size?: 'sm' | 'default'
  className?: string
}

export function CheckboxInput({
  label,
  value,
  onChange,
  htmlName,
  description,
  isLabelHidden = false,
  isDisabled = false,
  isRequired = false,
  size = 'default',
  className,
}: CheckboxInputProps) {
  const id = useControlId('checkbox')
  const inputId = htmlName ?? id
  const checked = value === true
  const indeterminate = value === 'indeterminate'

  const labelId = `${inputId}-label`
  const control = (
    <Checkbox
      id={inputId}
      name={htmlName}
      checked={checked}
      indeterminate={indeterminate}
      onCheckedChange={(next) => onChange(next)}
      disabled={isDisabled}
      required={isRequired || undefined}
      aria-labelledby={isLabelHidden ? labelId : undefined}
      className={cn(size === 'sm' ? 'size-3.5' : 'size-4', className)}
    />
  )

  if (isLabelHidden) {
    return (
      <ControlField label={label} isLabelHidden>
        <span className="sr-only" id={labelId}>
          {label}
        </span>
        {control}
      </ControlField>
    )
  }

  return (
    <ControlField label={label} description={description} htmlFor={inputId}>
      <div className="flex items-center gap-2">
        {control}
        <label htmlFor={inputId} className="text-sm leading-none select-none">
          {label}
        </label>
      </div>
    </ControlField>
  )
}
