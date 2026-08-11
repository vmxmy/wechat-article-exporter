import { DefinitionList, SectionHeader } from '../../../components/presentation'
import { formatCount, formatDateTime, formatDuration, formatRelativeTime } from '../../../lib/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobRecord } from '../../../lib/api'
import { useJobClock } from './jobClockContext'
import { resolveJobTiming } from './jobTiming'

/** Created to completed, with the two spans that explain a slow job: how long
    it waited to be picked up, and how long it has actually been running. Every
    row is guarded, so an older backend simply shows fewer of them. */
export function JobTimeline({ job, messages, locale }: { readonly job: JobRecord; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.resources.jobs
  const timing = resolveJobTiming(job, useJobClock())

  function moment(value: string) {
    return `${formatDateTime(value, locale)} · ${formatRelativeTime(value, locale)}`
  }

  const items = [
    { term: copy.columns.created, description: moment(job.createdAt) },
    { term: copy.columns.started, description: timing.startedAt ? moment(timing.startedAt) : copy.timing.notStarted },
    { term: copy.timing.completedAt, description: timing.completedAt ? moment(timing.completedAt) : copy.timing.stillRunning },
    ...(timing.queueWaitMs !== undefined ? [{ term: copy.timing.queueWait, description: formatDuration(timing.queueWaitMs, locale) }] : []),
    ...(timing.durationMs !== undefined ? [{ term: copy.timing.runTime, description: formatDuration(timing.durationMs, locale) }] : []),
    ...(typeof job.attemptCount === 'number' ? [{ term: copy.detail.attempts, description: formatCount(job.attemptCount, locale) }] : [])
  ]

  return (
    <section>
      <SectionHeader level={3} title={copy.detail.timeline} />
      <DefinitionList labelWidth="8rem" rowGap="relaxed" collapseAt="compact" items={items} />
    </section>
  )
}
