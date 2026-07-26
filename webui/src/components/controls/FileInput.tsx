import { useEffect, useRef } from 'react'
import type { ChangeEvent } from 'react'
import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import { ControlField, useControlId } from './field-context'

export type FileInputValue = File | File[] | null

export interface FileInputProps {
  label: string
  value: File | null
  onChange: (value: FileInputValue) => void
  changeAction?: (value: FileInputValue) => void | Promise<void>
  accept?: string
  description?: string
  isDisabled?: boolean
  isLoading?: boolean
  mode?: 'input'
  multiple?: boolean
  isRequired?: boolean
  isLabelHidden?: boolean
  className?: string
}

export function FileInput({
  label,
  value,
  onChange,
  changeAction,
  accept,
  description,
  isDisabled = false,
  isLoading = false,
  multiple = false,
  isRequired = false,
  isLabelHidden = false,
  className,
}: FileInputProps) {
  const id = useControlId('file-input')
  const inputRef = useRef<HTMLInputElement>(null)
  const previousValueRef = useRef<File | null>(value)
  const isUnavailable = isDisabled || isLoading

  useEffect(() => {
    const node = inputRef.current
    if (value === null && previousValueRef.current !== null && node) {
      node.value = ''
    }
    previousValueRef.current = value
  }, [value])

  const handleChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const nextValue: FileInputValue = multiple
      ? Array.from(event.currentTarget.files ?? [])
      : event.currentTarget.files?.[0] ?? null

    if (inputRef.current) inputRef.current.value = ''
    onChange(nextValue)
    await changeAction?.(nextValue)
  }

  return (
    <ControlField
      label={label}
      description={description}
      isRequired={isRequired}
      isLabelHidden={isLabelHidden}
      htmlFor={id}
    >
      <button
        type="button"
        className={cn(
          'inline-flex h-9 w-full items-center justify-between gap-2 rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none hover:bg-accent/40 focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50',
          className
        )}
        disabled={isUnavailable}
        aria-busy={isLoading || undefined}
        onClick={() => inputRef.current?.click()}
      >
        <span className="min-w-0 truncate">{value ? value.name : label}</span>
        <span className="relative flex shrink-0 items-center gap-2">
          {isLoading ? <Icons.spinner aria-hidden="true" className="size-4 animate-spin text-muted-foreground" /> : <Icons.upload aria-hidden="true" className="size-4 opacity-60" />}
          <input
            ref={inputRef}
            id={id}
            type="file"
            accept={accept}
            multiple={multiple}
            required={isRequired || undefined}
            aria-label={label}
            onChange={handleChange}
            className="absolute inset-0 size-full cursor-pointer opacity-0"
          />
        </span>
      </button>
    </ControlField>
  )
}
