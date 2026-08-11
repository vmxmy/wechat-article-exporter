import type { MessageCatalog } from '../../../i18n'

type JobDetailCopy = MessageCatalog['resources']['jobs']['detail']

/** Maps the backend's closed jobs.FailureClass vocabulary onto plain-language
    reasons. Unknown classes fall back rather than leaking the raw enum. */
const failureReasons: Readonly<Record<string, keyof JobDetailCopy>> = {
  network: 'networkReason',
  authentication: 'authReason',
  throttling: 'rateLimitReason',
  storage: 'storageReason',
  interrupted: 'timeoutReason',
  deleted: 'deletedReason',
  unavailable: 'deletedReason',
  parsing: 'parsingReason'
}

export function failureReason(errorClass: string | undefined, copy: JobDetailCopy): string {
  const key = failureReasons[errorClass?.trim().toLowerCase() ?? '']
  const reason = key ? copy[key] : undefined
  return typeof reason === 'string' ? reason : copy.unknownReason
}
