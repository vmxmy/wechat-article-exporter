import { Button } from '@/components/controls/Button'
import { DefinitionList, SectionHeader, Status, TechnicalDetails, type TechnicalDetailItem } from '../../../components/presentation'
import { formatCount, formatDateTime, formatJobKind, formatStatus } from '../../../lib/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobDetail } from '../../../lib/api'
import { JobFailureSummary } from './JobFailureSummary'
import { JobProgressBar } from './JobProgressCell'
import { JobTaskCell } from './JobTaskCell'
import { JobTimeline } from './JobTimeline'
import { summarizeJobCounts } from './jobProgress'

/** Ordered for triage: what went wrong, then how far it got, then when, then
    the raw material (items, logs, lease, identifiers). */
export function JobDetailContents({ detail, messages, locale, refreshing, onRefresh }: {
  readonly detail: JobDetail
  readonly messages: MessageCatalog
  readonly locale: Locale
  readonly refreshing: boolean
  readonly onRefresh: () => void
}) {
  const jobs = messages.resources.jobs
  const copy = jobs.detail
  const { job } = detail
  const summary = summarizeJobCounts(job)
  const label = job.label?.trim() || formatJobKind(job.kind, locale).label
  const countEntries = Object.entries(job.counts ?? {}).filter(([key, count]) => key !== 'total' && count > 0)
  const technical = (term: string, value: TechnicalDetailItem['value'], copyLabel: string = copy.copyValue): TechnicalDetailItem =>
    ({ label: term, value, copyLabel, copiedLabel: messages.a11y.copied, copyFailedLabel: messages.a11y.copyUnavailable })

  return (
    <div className="jobs-detail" aria-busy={refreshing}>
      <header className="jobs-detail-header">
        <div>
          <JobTaskCell job={job} locale={locale} />
          <Status value={job.state} locale={locale} isPulsing={job.state === 'running'} />
        </div>
        <Button label={copy.refresh} variant="secondary" isLoading={refreshing} onClick={onRefresh} />
      </header>

      <JobFailureSummary detail={detail} messages={messages} locale={locale} />

      <section>
        <SectionHeader level={3} title={copy.progress} />
        <JobProgressBar summary={summary} label={label} copy={jobs} locale={locale} />
        <DefinitionList
          labelWidth="8rem"
          rowGap="relaxed"
          collapseAt="compact"
          items={[
            { term: jobs.columns.kind, description: formatJobKind(job.kind, locale).label },
            {
              term: jobs.columns.counts,
              description: summary.total === undefined || summary.total === 0
                ? jobs.progress.noItems
                : jobs.progress.ratioText(summary.settled, summary.total)
            },
            ...countEntries.map(([state, count]) => ({
              term: formatStatus(state, locale).label,
              description: formatCount(count, locale)
            })),
            { term: copy.refreshed, description: formatDateTime(detail.refreshedAt, locale) }
          ]}
        />
      </section>

      <JobTimeline job={job} messages={messages} locale={locale} />

      <section>
        <SectionHeader level={3} title={copy.items} />
        {detail.itemsLimited ? <p>{copy.itemsLimited(detail.items.length, detail.itemsTotal)}</p> : null}
        {detail.items.length === 0 ? <p>{copy.noItems}</p> : (
          <ul className="jobs-detail-list">
            {detail.items.map((item) => (
              <li key={item.id}>
                <Status value={item.state} locale={locale} />
                <span>{copy.attempts}: {formatCount(item.attemptCount, locale)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section>
        <SectionHeader level={3} title={copy.logs} />
        {detail.logs.length === 0 ? <p>{copy.noLogs}</p> : (
          <ul className="jobs-detail-list">
            {detail.logs.map((log) => (
              <li key={log.id}>
                <span>{formatDateTime(log.createdAt, locale)}</span>
                <span>{log.message}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section>
        <SectionHeader level={3} title={copy.lease} />
        <DefinitionList
          labelWidth="8rem"
          rowGap="relaxed"
          collapseAt="compact"
          items={[
            { term: copy.lease, description: detail.lease.active ? copy.leaseActive : copy.leaseInactive },
            { term: copy.expires, description: formatDateTime(detail.lease.expiresAt, locale) }
          ]}
        />
      </section>

      <TechnicalDetails
        label={copy.technicalDetails}
        items={[
          technical(copy.jobID, job.id, copy.copyID),
          technical(copy.profile, job.profile),
          technical(jobs.columns.created, job.createdAt),
          ...(job.startedAt ? [technical(jobs.columns.started, job.startedAt)] : []),
          ...(job.completedAt ? [technical(jobs.timing.completedAt, job.completedAt)] : []),
          technical(jobs.columns.updated, job.updatedAt)
        ]}
      />
    </div>
  )
}
