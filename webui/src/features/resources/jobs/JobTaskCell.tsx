import { formatJobKind } from '../../../lib/presentation'
import type { Locale } from '../../../i18n'
import type { JobRecord } from '../../../lib/api'

/** Renders the job title as a single text node.
 *
 *  The backend derives `label` by title-casing `kind`, so when the two agree
 *  the localized kind is the better label. Splitting this into multiple nodes
 *  would break the exact-text selectors the workspace specs use to find a task
 *  row by name. */
export function JobTaskCell({ job, locale }: { readonly job: JobRecord; readonly locale: Locale }) {
  const kind = formatJobKind(job.kind, locale)
  const label = job.label?.trim()
  const humanizedKind = job.kind.replaceAll('_', ' ').toLocaleLowerCase(locale)
  const displayLabel = label && label.toLocaleLowerCase(locale) !== humanizedKind ? label : kind.label
  return <div className="jobs-label"><strong>{displayLabel}</strong></div>
}
