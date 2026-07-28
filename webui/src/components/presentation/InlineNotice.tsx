import type { ReactNode } from 'react'
import './presentation-layout.css'

export interface InlineNoticeProps {
  /** `alert` for errors; `status` for success/information. Sets the ARIA role. */
  readonly tone?: 'status' | 'alert'
  readonly children: ReactNode
}

/**
 * Single in-flow page notice for operation results (save/sync/export success or
 * failure). Lives between the filter region and the data surface so all resource
 * pages surface notices in the same place. Renders nothing when there is no child.
 */
export function InlineNotice({ tone = 'status', children }: InlineNoticeProps) {
  if (!children) return null
  return (
    <p className="presentation-inline-notice" role={tone} data-tone={tone === 'alert' ? 'error' : 'neutral'}>
      {children}
    </p>
  )
}
