import { Button } from '@/components/controls/Button'
import { DetailPanel } from '../../../components/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobControlAction } from '../../../lib/api'
import type { useJobDetail } from '../../../lib/queries'
import { JobControls } from './JobControls'
import { JobDetailContents } from './JobDetailContents'

export interface JobDetailDrawerProps {
  readonly detail: ReturnType<typeof useJobDetail>
  readonly messages: MessageCatalog
  readonly locale: Locale
  readonly permittedActions: readonly JobControlAction[]
  readonly isControlPending: boolean
  readonly notice: string | undefined
  readonly onControl: (action: JobControlAction) => void
  readonly onOpenChange: (isOpen: boolean) => void
}

export function JobDetailDrawer({ detail, messages, locale, permittedActions, isControlPending, notice, onControl, onOpenChange }: JobDetailDrawerProps) {
  const copy = messages.resources.jobs.detail
  const actions = messages.resources.jobs.actions
  return (
    <DetailPanel
      isOpen
      onOpenChange={onOpenChange}
      title={copy.title}
      description={copy.description}
      closeLabel={messages.a11y.closeDialog}
      footer={<JobControls actions={actions} permittedActions={permittedActions} isLoading={isControlPending} onControl={onControl} />}
    >
      {detail.isLoading ? <p role="status">{copy.loading}</p> : null}
      {detail.isError ? (
        <div className="error-state" role="alert">
          <p>{copy.unavailable}</p>
          <Button label={copy.refresh} variant="secondary" onClick={() => void detail.refetch()} />
        </div>
      ) : null}
      {!detail.isLoading && !detail.isError && detail.data ? (
        <JobDetailContents
          detail={detail.data}
          messages={messages}
          locale={locale}
          refreshing={detail.isFetching}
          onRefresh={() => void detail.refetch()}
        />
      ) : null}
      {notice ? <p className="jobs-notice" role="alert">{notice}</p> : null}
    </DetailPanel>
  )
}
