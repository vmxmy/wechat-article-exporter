import {
  createContext,
  useContext,
  type ReactNode,
} from 'react'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { ControlField, useControlId } from './field-context'
import { cn } from '@/lib/utils'

export interface RadioListProps {
  label: string
  value: string
  onChange: (value: string) => void
  orientation?: 'vertical'
  description?: string
  isRequired?: boolean
  isLabelHidden?: boolean
  children: ReactNode
  className?: string
}

export interface RadioListItemProps {
  value: string
  label: string
  description?: string
  isDisabled?: boolean
  className?: string
}

interface RadioListContextValue {
  groupName: string
  selectedValue: string
}

const RadioListContext = createContext<RadioListContextValue | null>(null)

export function RadioList({
  label,
  value,
  onChange,
  orientation = 'vertical',
  description,
  isRequired = false,
  isLabelHidden = false,
  children,
  className,
}: RadioListProps) {
  const groupName = useControlId('radio-list')

  return (
    <ControlField
      label={label}
      description={description}
      isRequired={isRequired}
      isLabelHidden={isLabelHidden}
      htmlFor={groupName}
    >
      <RadioListContext.Provider value={{ groupName, selectedValue: value }}>
        <RadioGroup
          id={groupName}
          name={groupName}
          value={value}
          onValueChange={onChange}
          className={cn(orientation === 'vertical' && 'flex flex-col gap-2', className)}
        >
          {children}
        </RadioGroup>
      </RadioListContext.Provider>
    </ControlField>
  )
}

export function RadioListItem({
  value,
  label,
  description,
  isDisabled = false,
  className,
}: RadioListItemProps) {
  const context = useContext(RadioListContext)
  const itemId = useControlId('radio-list-item')

  if (!context) {
    throw new Error('RadioListItem must be rendered within a RadioList')
  }

  const isSelected = context.selectedValue === value

  return (
    <label
      htmlFor={itemId}
      data-selected={isSelected ? '' : undefined}
      className={cn(
        'flex cursor-pointer items-start gap-2 rounded-md p-1.5 data-[selected]:bg-accent',
        isDisabled && 'cursor-not-allowed opacity-50',
        className
      )}
    >
      <RadioGroupItem id={itemId} value={value} disabled={isDisabled} />
      <span className='grid gap-0.5'>
        <span className='text-sm font-medium'>{label}</span>
        {description ? <span className='text-muted-foreground text-sm'>{description}</span> : null}
      </span>
    </label>
  )
}
