import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import { useRef } from 'react'
import { ControlField, useControlId } from './field-context'
import { SelectorClearButton, SelectorOptionContent } from './selector-parts'

export interface SelectorOption {
  value: string
  label: string
  description?: string
}

export interface SelectorProps {
  label: string
  options: SelectorOption[]
  value: string | null
  onChange: (value: string | null) => void
  placeholder?: string
  htmlName?: string
  hasClear?: boolean
  clearLabel?: string
  isLoading?: boolean
  isDisabled?: boolean
  description?: string
  isRequired?: boolean
  isOptional?: boolean
  isLabelHidden?: boolean
  layout?: 'inline' | 'compact'
  size?: 'sm' | 'lg'
  /** Class name for the outer Field wrapper, used for bounded toolbar sizing. */
  fieldClassName?: string
  className?: string
}

export function Selector({
  label,
  options,
  value,
  onChange,
  placeholder,
  htmlName,
  hasClear = false,
  clearLabel,
  isLoading = false,
  isDisabled = false,
  description,
  isRequired = false,
  isOptional = false,
  isLabelHidden = false,
  layout,
  size,
  fieldClassName,
  className,
}: SelectorProps) {
  const id = useControlId('selector')
  const labelId = `${id}-label`
  const triggerRef = useRef<HTMLButtonElement>(null)
  const isDisabledOrLoading = isDisabled || isLoading
  const triggerLabelId = isLabelHidden ? undefined : labelId
  const descriptionId = description ? `${id}-description` : undefined

  if (hasClear && !clearLabel) throw new Error('Selector requires clearLabel when hasClear is enabled.')

  return (
    <ControlField
      label={label}
      description={description}
      isRequired={isRequired}
      isOptional={isOptional}
      isLabelHidden={isLabelHidden}
      htmlFor={id}
      labelId={labelId}
      descriptionId={descriptionId}
      layout={layout}
      size={size}
      className={fieldClassName}
    >
      <div className={cn('flex items-center gap-2', className)}>
        <Select
          value={value}
          name={htmlName}
          disabled={isDisabledOrLoading}
          required={isRequired}
          onValueChange={(nextValue) => onChange(typeof nextValue === 'string' ? nextValue : null)}
        >
          <SelectTrigger ref={triggerRef} id={id} aria-labelledby={triggerLabelId} aria-describedby={descriptionId} aria-label={isLabelHidden ? label : undefined} className="w-full">
            {/* Base UI skips its own placeholder branch whenever children is a function,
                so an empty selection has to resolve the placeholder here. */}
            <SelectValue placeholder={placeholder}>{(selected: string | null) => selected === null ? placeholder : options.find((option) => option.value === selected)?.label ?? selected}</SelectValue>
            {isLoading ? <Icons.spinner className="size-4 animate-spin" aria-hidden="true" /> : null}
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {options.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  <SelectorOptionContent option={option} />
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        {hasClear && value !== null && clearLabel ? (
          <SelectorClearButton
            label={label}
            copy={{ clear: () => clearLabel }}
            isDisabled={isDisabledOrLoading}
            onClear={() => onChange(null)}
            restoreFocusRef={triggerRef}
          />
        ) : null}
      </div>
    </ControlField>
  )
}
