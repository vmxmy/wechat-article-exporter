import { Combobox } from '@base-ui/react/combobox'
import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import { useId, useMemo, useRef, useState } from 'react'
import { ControlField } from './field-context'
import { SelectorClearButton, SelectorOptionContent, type SelectorCopy, type SelectorDisplayOption } from './selector-parts'
import { selectorOptionClassName, selectorPopupClassName, selectorTriggerClassName } from './selector-styles'

export interface SearchableSelectorProps {
  readonly label: string
  readonly options: readonly SelectorDisplayOption[]
  readonly value: string | null
  readonly onChange: (value: string | null) => void
  readonly placeholder?: string
  readonly copy: Pick<SelectorCopy, 'clear' | 'search' | 'noResults' | 'loading'>
  readonly hasClear?: boolean
  readonly isLoading?: boolean
  readonly isDisabled?: boolean
  readonly description?: string
  readonly isLabelHidden?: boolean
  readonly layout?: 'inline' | 'compact'
  readonly size?: 'sm' | 'lg'
  readonly className?: string
}

export function SearchableSelector({
  label,
  options,
  value,
  onChange,
  placeholder,
  copy,
  hasClear = false,
  isLoading = false,
  isDisabled = false,
  description,
  isLabelHidden = false,
  layout,
  size,
  className
}: SearchableSelectorProps) {
  const labelId = useId()
  const descriptionId = description ? `${labelId}-description` : undefined
  const inputRef = useRef<HTMLInputElement>(null)
  const [isOpen, setIsOpen] = useState(false)
  const [query, setQuery] = useState('')
  const selected = options.find((option) => option.value === value)
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const filteredOptions = useMemo(() => options.filter((option) => {
    if (!normalizedQuery) return true
    return `${option.label} ${option.description ?? ''}`.toLocaleLowerCase().includes(normalizedQuery)
  }), [normalizedQuery, options])
  const inputValue = isOpen ? query : selected?.label ?? ''

  return (
    <ControlField label={label} description={description} isLabelHidden={isLabelHidden} labelId={labelId} descriptionId={descriptionId} layout={layout} size={size}>
      <div className={cn('flex items-center gap-2', className)}>
        <Combobox.Root
          value={value}
          onValueChange={(next) => {
            onChange(next)
            setIsOpen(false)
          }}
          inputValue={inputValue}
          onInputValueChange={setQuery}
          open={isOpen}
          onOpenChange={(open) => {
            setIsOpen(open)
            if (open) setQuery('')
          }}
          disabled={isDisabled || isLoading}
        >
          <div className="relative w-full">
            <Combobox.Input
              ref={inputRef}
              aria-labelledby={isLabelHidden ? undefined : labelId}
              aria-describedby={descriptionId}
              aria-label={isLabelHidden ? label : undefined}
              placeholder={placeholder}
              className={`${selectorTriggerClassName} w-full pr-9 placeholder:text-muted-foreground`}
            />
            {isLoading ? <Icons.spinner className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-muted-foreground" aria-hidden="true" /> : <Icons.chevronsUpDown className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 opacity-50" aria-hidden="true" />}
          </div>
          <Combobox.Portal>
            <Combobox.Positioner align="start" className="z-50">
              <Combobox.Popup className={`${selectorPopupClassName} w-(--anchor-width) min-w-72 p-1 outline-none data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1`}>
                {isLoading ? <p className="px-2 py-3 text-sm text-muted-foreground" role="status">{copy.loading}</p> : null}
                {!isLoading && filteredOptions.length === 0 ? <Combobox.Empty className="px-2 py-6 text-center text-sm text-muted-foreground">{copy.noResults}</Combobox.Empty> : null}
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
        {hasClear && value !== null ? <SelectorClearButton label={label} copy={copy} isDisabled={isDisabled || isLoading} onClear={() => onChange(null)} restoreFocusRef={inputRef} /> : null}
      </div>
    </ControlField>
  )
}
