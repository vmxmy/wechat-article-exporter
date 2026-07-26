import { Combobox } from '@base-ui/react/combobox'
import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import { useId, useMemo, useRef, useState } from 'react'
import { ControlField } from './field-context'
import { SelectorClearButton, SelectorOptionContent, type SelectorCopy, type SelectorDisplayOption } from './selector-parts'
import { selectorOptionClassName, selectorPopupClassName, selectorTriggerClassName } from './selector-styles'

export interface MultiSelectorOption extends SelectorDisplayOption {
  readonly value: string
}

export interface MultiSelectorProps {
  readonly label: string
  readonly options: readonly MultiSelectorOption[]
  readonly value: readonly string[]
  readonly onChange: (values: string[]) => void
  readonly copy: Pick<SelectorCopy, 'clear' | 'search' | 'noResults' | 'selectAll' | 'selected'>
  readonly placeholder?: string
  readonly isLabelHidden?: boolean
  readonly triggerDisplay?: 'labels' | 'count'
  readonly hasClear?: boolean
  readonly hasSelectAll?: boolean
  readonly description?: string
  readonly isDisabled?: boolean
  readonly className?: string
}

export function MultiSelector({
  label,
  options,
  value,
  onChange,
  copy,
  placeholder,
  isLabelHidden = false,
  triggerDisplay = 'labels',
  hasClear = false,
  hasSelectAll = false,
  description,
  isDisabled = false,
  className,
}: MultiSelectorProps) {
  const labelId = useId()
  const descriptionId = description ? `${labelId}-description` : undefined
  const inputRef = useRef<HTMLInputElement>(null)
  const [isOpen, setIsOpen] = useState(false)
  const [query, setQuery] = useState('')
  const selectedValues = new Set(value)
  const selectedOptions = options.filter((option) => selectedValues.has(option.value))
  const selectionLabel = triggerDisplay === 'count' && value.length > 0
    ? copy.selected(value.length)
    : selectedOptions.map((option) => option.label).join(', ') || placeholder
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const filteredOptions = useMemo(() => options.filter((option) => {
    if (!normalizedQuery) return true
    return `${option.label} ${option.description ?? ''}`.toLocaleLowerCase().includes(normalizedQuery)
  }), [normalizedQuery, options])
  const inputValue = isOpen ? query : ''

  return (
    <ControlField label={label} description={description} isLabelHidden={isLabelHidden} labelId={labelId} descriptionId={descriptionId}>
      <div className={cn('flex items-center gap-1', className)}>
        <Combobox.Root
          multiple
          value={[...value]}
          onValueChange={(next) => {
            onChange([...next])
            setIsOpen(false)
          }}
          inputValue={inputValue}
          onInputValueChange={setQuery}
          open={isOpen}
          onOpenChange={(open) => {
            setIsOpen(open)
            if (open) setQuery('')
          }}
          disabled={isDisabled}
        >
          <div className="relative w-full">
            <Combobox.Input
              ref={inputRef}
              aria-labelledby={isLabelHidden ? undefined : labelId}
              aria-describedby={descriptionId}
              aria-label={isLabelHidden ? label : undefined}
              placeholder={selectionLabel}
              className={`${selectorTriggerClassName} w-full pr-9 placeholder:text-muted-foreground`}
            />
            <Icons.chevronsUpDown className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 opacity-50" aria-hidden="true" />
          </div>
          <Combobox.Portal>
            <Combobox.Positioner align="start" className="z-50">
              <Combobox.Popup className={`${selectorPopupClassName} w-(--anchor-width) min-w-72 p-1 outline-none data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1`}>
                {hasSelectAll ? <button type="button" className={`${selectorOptionClassName} text-left`} disabled={isDisabled} onClick={() => onChange(options.map((option) => option.value))}><Icons.checks className="size-4" />{copy.selectAll}</button> : null}
                {filteredOptions.length === 0 ? <Combobox.Empty className="px-2 py-6 text-center text-sm text-muted-foreground">{copy.noResults}</Combobox.Empty> : null}
                <Combobox.List>
                  {filteredOptions.map((option) => (
                    <Combobox.Item key={option.value} value={option.value} className={selectorOptionClassName}>
                      <span className="absolute right-2 flex size-3.5 items-center justify-center"><Combobox.ItemIndicator><Icons.check className="size-4" /></Combobox.ItemIndicator></span>
                      <SelectorOptionContent option={option} />
                    </Combobox.Item>
                  ))}
                </Combobox.List>
              </Combobox.Popup>
            </Combobox.Positioner>
          </Combobox.Portal>
        </Combobox.Root>
        {hasClear && value.length > 0 ? <SelectorClearButton label={label} copy={copy} isDisabled={isDisabled} onClear={() => onChange([])} restoreFocusRef={inputRef} /> : null}
      </div>
    </ControlField>
  )
}
