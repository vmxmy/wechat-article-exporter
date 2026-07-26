import { cn } from '@/lib/utils'

export interface TimestampProps {
  value: string
  format?: 'auto'
  className?: string
}

function formatTimestamp(value: string) {
  try {
    return new Intl.DateTimeFormat('en-US', {
      dateStyle: 'medium',
      timeStyle: 'short'
    }).format(new Date(value))
  } catch {
    return value
  }
}

export function Timestamp({
  value,
  format = 'auto',
  className
}: TimestampProps) {
  const formattedValue = format === 'auto' ? formatTimestamp(value) : value

  return (
    <time className={cn(className)} dateTime={value}>
      {formattedValue}
    </time>
  )
}
