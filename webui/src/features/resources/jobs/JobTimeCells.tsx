import { formatDateTime, formatDuration, formatRelativeTime } from '../../../lib/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobRecord } from '../../../lib/api'
import { useJobClock } from './jobClockContext'
import { resolveJobTiming } from './jobTiming'

/** Both lines are visible text. The absolute timestamp used to live only in a
    `title` tooltip, which is unreachable on touch and silent to screen
    readers. */
export function JobStartedCell({ job, messages, locale }: { readonly job: JobRecord; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.resources.jobs.timing
  const timing = resolveJobTiming(job, useJobClock())
  return (
    <div className="jobs-time">
      <time className="jobs-time-relative" dateTime={timing.anchor}>
        {timing.anchorIsStart ? formatRelativeTime(timing.anchor, locale) : copy.notStarted}
      </time>
      <span className="jobs-time-absolute">{formatDateTime(timing.anchor, locale)}</span>
    </div>
  )
}

export function JobDurationCell({ job, messages, locale }: { readonly job: JobRecord; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.resources.jobs.timing
  const timing = resolveJobTiming(job, useJobClock())
  const duration = formatDuration(timing.durationMs, locale)
  const label = timing.phase === 'waiting'
    ? copy.waiting(duration)
    : timing.phase === 'running'
      ? copy.elapsed(duration)
      : duration
  return <span className="jobs-time-duration">{label}</span>
}
