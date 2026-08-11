import { Button } from '@/components/controls/Button'
import { ActionGroup } from '../../../components/presentation'
import type { MessageCatalog } from '../../../i18n'
import type { JobControlAction } from '../../../lib/api'

type JobActionsCopy = MessageCatalog['resources']['jobs']['actions']

export function JobControls({ actions, permittedActions, isLoading, onControl }: {
  readonly actions: JobActionsCopy
  readonly permittedActions: readonly JobControlAction[]
  readonly isLoading: boolean
  readonly onControl: (action: JobControlAction) => void
}) {
  if (permittedActions.length === 0) return null
  const actionLabels: Readonly<Record<JobControlAction, string>> = { pause: actions.pause, resume: actions.resume, retry: actions.retry, cancel: actions.cancel }
  return (
    <ActionGroup align="start" aria-label={actions.title}>
      {permittedActions.map((action) => (
        <Button
          key={action}
          label={actionLabels[action]}
          variant={action === 'cancel' ? 'destructive' : 'secondary'}
          isLoading={isLoading}
          onClick={() => onControl(action)}
        />
      ))}
    </ActionGroup>
  )
}
