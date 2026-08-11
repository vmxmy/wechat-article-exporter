import type { MessageCatalog } from '../../../i18n'
import type { JobControlAction } from '../../../lib/api'

type JobActionsCopy = MessageCatalog['resources']['jobs']['actions']

export type JobConfirmationAction = Exclude<JobControlAction, 'resume'>

/** The typed proof the server requires for each destructive control. Resume is
    absent because the API accepts it without confirmation. */
export function jobConfirmation(actions: JobActionsCopy, action: JobConfirmationAction, id: string) {
  const confirmations = {
    pause: { title: actions.pauseTitle, description: actions.confirmPause, actionLabel: actions.pause, confirmation: actions.pauseConfirmation(id) },
    retry: { title: actions.retryTitle, description: actions.confirmRetry, actionLabel: actions.retry, confirmation: actions.retryConfirmation(id) },
    cancel: { title: actions.cancelTitle, description: actions.confirmCancel, actionLabel: actions.cancel, confirmation: actions.cancelConfirmationProof(id) }
  } as const
  return confirmations[action]
}
