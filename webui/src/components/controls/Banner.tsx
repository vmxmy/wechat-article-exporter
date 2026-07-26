import type { ReactNode } from 'react'

import { Icons } from '@/components/icons'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { cn } from '@/lib/utils'

export type BannerStatus = 'success' | 'error' | 'warning'

export interface BannerProps {
  status: BannerStatus
  title: string
  endContent?: ReactNode
  isDismissable?: boolean
  onDismiss?: () => void
  className?: string
}

const statusStyles = {
  success: {
    icon: Icons.circleCheck,
    className: 'border-green-500/50 bg-green-500/10 text-green-700 dark:text-green-400',
    role: 'status' as const,
  },
  error: {
    icon: Icons.circleX,
    className: 'border-destructive/50 bg-destructive/10 text-destructive',
    role: 'alert' as const,
  },
  warning: {
    icon: Icons.warning,
    className: 'border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400',
    role: 'alert' as const,
  },
} satisfies Record<BannerStatus, { icon: typeof Icons.circleCheck; className: string; role: 'status' | 'alert' }>

export function Banner({
  status,
  title,
  endContent,
  isDismissable = false,
  onDismiss,
  className,
}: BannerProps) {
  const { icon: StatusIcon, className: statusClassName, role } = statusStyles[status]

  return (
    <Alert
      role={role}
      variant={status === 'error' ? 'destructive' : 'default'}
      className={cn(statusClassName, className)}
    >
      <StatusIcon />
      <div className="flex min-w-0 items-center justify-between gap-3">
        <AlertTitle>{title}</AlertTitle>
        {(endContent || isDismissable) && (
          <div className="flex shrink-0 items-center gap-2">
            {endContent}
            {isDismissable && (
              <button
                type="button"
                aria-label="Dismiss banner"
                className="rounded-sm p-1 transition-colors hover:bg-current/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-current"
                onClick={onDismiss}
              >
                <Icons.close className="size-4" />
              </button>
            )}
          </div>
        )}
      </div>
    </Alert>
  )
}
