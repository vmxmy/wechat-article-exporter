import { Link } from '@/components/controls/Link'
import { DefinitionList, SectionHeader } from '../../../components/presentation'
import { formatDateTime } from '../../../lib/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobControlAction, JobDetail } from '../../../lib/api'
import { failureReason } from './jobFailure'

type JobDetailCopy = MessageCatalog['resources']['jobs']['detail']
type JobAttentionCopy = MessageCatalog['resources']['jobs']['attention']

function nextStep(state: string, errorClass: string | undefined, permittedActions: readonly JobControlAction[], copy: JobDetailCopy): string {
  if (state === 'blocked_auth') return copy.signInAction
  if (errorClass?.trim().toLowerCase() === 'throttling') return copy.rateLimitAction
  return permittedActions.includes('retry') ? copy.retryAction : copy.refreshAction
}

/** Triage panel, rendered first in the drawer.
 *
 *  Prefers the job-level `errorSummary` the backend now supplies, and falls
 *  back to deriving one from the failed items so the panel still works against
 *  an older backend. */
export function JobFailureSummary({ detail, messages, locale }: { readonly detail: JobDetail; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.resources.jobs.detail
  const attention: JobAttentionCopy = messages.resources.jobs.attention
  const { job } = detail
  const summary = job.errorSummary
  const failedItems = detail.items.filter((item) => item.state === 'failed' || Boolean(item.errorClass))
  const isBlocked = job.state === 'blocked_auth'
  if (!summary && failedItems.length === 0 && !isBlocked) return null

  const errorClass = summary?.errorClass ?? failedItems[0]?.errorClass
  const itemCount = summary?.itemCount ?? failedItems.length
  const occurredAt = summary?.occurredAt ?? failedItems[0]?.updatedAt

  const items = [
    { term: copy.reason, description: failureReason(errorClass, copy) },
    ...(summary?.message ? [{ term: attention.errorMessage, description: <span className="jobs-failure-message">{summary.message}</span> }] : []),
    ...(itemCount > 0 ? [{ term: copy.impact, description: attention.failedItems(itemCount) }] : []),
    ...(occurredAt ? [{ term: attention.occurredAt, description: formatDateTime(occurredAt, locale) }] : []),
    { term: copy.nextAction, description: nextStep(job.state, errorClass, job.permittedActions, copy) }
  ]

  return (
    <section className="jobs-failure-summary">
      <SectionHeader level={3} title={copy.attention} />
      <DefinitionList labelWidth="8rem" rowGap="relaxed" collapseAt="compact" items={items} />
      {isBlocked ? <Link href="/login" isStandalone>{attention.blockedSignIn}</Link> : null}
    </section>
  )
}
