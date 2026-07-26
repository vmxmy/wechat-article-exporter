import { Badge } from '@/components/ui/badge'
import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'

export interface TokenProps {
  label: string
  size?: 'sm'
  description: string
  onRemove: () => void
  className?: string
}

export function Token({
  label,
  size,
  description,
  onRemove,
  className,
}: TokenProps) {
  return (
    <Badge
      variant="secondary"
      className={cn(
        'gap-1',
        size === 'sm' && 'px-1.5 py-0 text-xs',
        className,
      )}
    >
      {label}
      <button
        type="button"
        aria-label={description}
        onClick={onRemove}
        className="rounded-sm p-0.5 hover:bg-foreground/10 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      >
        <Icons.close className="size-3" />
      </button>
    </Badge>
  )
}
