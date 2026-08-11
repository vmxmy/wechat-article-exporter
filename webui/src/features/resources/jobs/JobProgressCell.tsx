import { Progress } from '@/components/ui/progress'
import { formatStatus } from '../../../lib/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobRecord } from '../../../lib/api'
import { summarizeJobCounts, type JobProgressSummary } from './jobProgress'

type JobsCopy = MessageCatalog['resources']['jobs']

/** Base UI's Progress emits role/aria-valuenow/aria-valuetext itself and reads
    a null value as indeterminate, so the shadcn wrapper needs no changes. */
export function JobProgressBar({ summary, label, copy, locale }: { readonly summary: JobProgressSummary; readonly label: string; readonly copy: JobsCopy; readonly locale: Locale }) {
  if (summary.mode === 'none') return null
  const total = summary.total ?? 0
  const valueText = summary.mode === 'determinate'
    ? copy.progress.ratioText(summary.settled, total)
    : copy.progress.unknownTotal(summary.completed)
  return (
    <Progress
      className="jobs-progress-bar"
      value={summary.mode === 'indeterminate' ? null : (summary.ratio ?? 0)}
      max={100}
      aria-label={copy.progress.label(label)}
      getAriaValueText={() => valueText}
      data-tone={summary.failed > 0 ? 'error' : summary.partial > 0 ? 'warning' : 'success'}
      data-locale={locale}
    />
  )
}

/** Each non-completed state renders as a single text node ("2 failed"). A bare
    status label would collide with the exact-text selectors the specs use to
    assert a row's state. */
function JobProgressBreakdown({ summary, copy, locale }: { readonly summary: JobProgressSummary; readonly copy: JobsCopy; readonly locale: Locale }) {
  const parts = ([
    ['failed', summary.failed],
    ['partial', summary.partial],
    ['cancelled', summary.cancelled],
    ['running', summary.running],
    ['queued', summary.queued],
    ['blocked_auth', summary.blockedAuth]
  ] as const).filter(([, count]) => count > 0)
  if (parts.length === 0) return null
  return (
    <span className="jobs-progress-breakdown">
      {parts.map(([state, count]) => (
        <span key={state} className="jobs-progress-part" data-state={state}>
          {copy.progress.state(count, formatStatus(state, locale).label)}
        </span>
      ))}
    </span>
  )
}

export function JobProgressCell({ job, messages, locale, label }: { readonly job: JobRecord; readonly messages: MessageCatalog; readonly locale: Locale; readonly label: string }) {
  const copy = messages.resources.jobs
  const summary = summarizeJobCounts(job)
  const value = summary.mode === 'determinate'
    ? copy.progress.ratio(summary.settled, summary.total ?? 0)
    : summary.mode === 'indeterminate'
      ? copy.progress.unknownTotal(summary.completed)
      : copy.progress.noItems
  return (
    <div className="jobs-progress">
      <JobProgressBar summary={summary} label={label} copy={copy} locale={locale} />
      <span className="jobs-progress-value">{value}</span>
      <JobProgressBreakdown summary={summary} copy={copy} locale={locale} />
    </div>
  )
}
