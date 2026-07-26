import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import type { RefObject } from 'react'

export interface SelectorDisplayOption {
  readonly value: string
  readonly label: string
  readonly description?: string
}

export interface SelectorCopy {
  readonly clear: (label: string) => string
  readonly search: (label: string) => string
  readonly noResults: string
  readonly loading: string
  readonly unavailable: string
  readonly retry: string
  readonly selectAll: string
  readonly selected: (count: number) => string
  readonly duplicate: (position: number, total: number) => string
}

export function SelectorOptionContent({ option }: { readonly option: Pick<SelectorDisplayOption, 'label' | 'description'> }) {
  return (
    <span className="flex min-w-0 flex-col">
      <span className="truncate">{option.label}</span>
      {option.description ? <span className="truncate text-xs text-muted-foreground">{option.description}</span> : null}
    </span>
  )
}

export function SelectorClearButton({
  label,
  copy,
  isDisabled = false,
  onClear,
  restoreFocusRef,
  className
}: {
  readonly label: string
  readonly copy: Pick<SelectorCopy, 'clear'>
  readonly isDisabled?: boolean
  readonly onClear: () => void
  readonly restoreFocusRef?: RefObject<HTMLElement | null>
  readonly className?: string
}) {
  return (
    <button
      type="button"
      aria-label={copy.clear(label)}
      disabled={isDisabled}
      className={cn('text-muted-foreground hover:text-foreground inline-flex size-8 shrink-0 items-center justify-center rounded-md disabled:pointer-events-none disabled:opacity-50', className)}
      onClick={() => {
        onClear()
        window.requestAnimationFrame(() => restoreFocusRef?.current?.focus())
      }}
    >
      <Icons.close className="size-4" aria-hidden="true" />
    </button>
  )
}
