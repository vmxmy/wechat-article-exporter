import { Status } from '../../../components/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobRecord } from '../../../lib/api'
import { failureReason } from './jobFailure'

/** Status plus the one line of triage context that matters for this state.
 *
 *  The error summary lives here rather than in a column of its own: the mobile
 *  projection has a single status slot, so folding it in carries triage context
 *  to small screens for free. */
export function JobStateCell({ job, messages, locale }: { readonly job: JobRecord; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.resources.jobs
  const summary = job.errorSummary
  const isBlocked = job.state === 'blocked_auth'
  const detail = isBlocked
    ? copy.attention.blockedHint
    : summary
      ? failureReason(summary.errorClass, copy.detail)
      : undefined
  return (
    <div className="jobs-state-cell">
      <Status value={job.state} locale={locale} isPulsing={job.state === 'running'} />
      {detail ? <span className="jobs-state-detail">{detail}</span> : null}
    </div>
  )
}
