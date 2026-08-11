import { Banner } from '@/components/controls/Banner'
import { Button } from '@/components/controls/Button'
import { Link } from '@/components/controls/Link'
import { EmptyState, PageStack, TypedConfirmationDialog } from '../../../components/presentation'
import { useEffect, useRef, useState } from 'react'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobControlAction } from '../../../lib/api'
import { loadJobHandoff } from '../../../lib/jobHandoff'
import { useJobDetail, useJobPage, useWorkspaceMutations } from '../../../lib/queries'
import { ResourceTable } from '../ResourceTable'
import { JobClockProvider } from './JobClock'
import { jobConfirmation, type JobConfirmationAction } from './jobConfirmation'
import { JobDetailDrawer } from './JobDetailDrawer'
import { JobsFilterToolbar } from './JobsFilterToolbar'
import { useJobColumns } from './jobColumns'
import { jobFilterStates, type TaskFilter } from './jobFilters'
import { useJobFilterCounts } from './useJobFilterCounts'
import './jobs.css'

const pageSize = 25

export function JobsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [handoffJobID] = useState(readJobHandoff)
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>(() => handoffJobID ? [handoffJobID] : [])
  const [isDetailOpen, setDetailOpen] = useState(Boolean(handoffJobID))
  const [notice, setNotice] = useState<string>()
  const [confirmationAction, setConfirmationAction] = useState<JobConfirmationAction>()
  const [confirmationProof, setConfirmationProof] = useState('')
  const [taskFilter, setTaskFilter] = useState<TaskFilter>('all')
  const [kind, setKind] = useState<string>()
  const confirmationTrigger = useRef<HTMLElement | null>(null)

  // Filtering happens server-side: the tab's states go out as repeated `state=`
  // parameters, so pagination totals and the range summary describe the filter
  // rather than whichever rows this page happened to receive.
  const query = useJobPage({ page: pageIndex + 1, pageSize, kind, states: jobFilterStates[taskFilter] })
  const filterCounts = useJobFilterCounts(kind)
  const selectedID = selected.length === 1 ? selected[0] : undefined
  const detail = useJobDetail(selectedID)
  const mutations = useWorkspaceMutations()
  const actions = messages.resources.jobs.actions
  const columns = useJobColumns(messages, locale)
  const permittedActions = detail.data?.job.permittedActions ?? []
  const blockedCount = (query.data?.data ?? []).filter((job) => job.state === 'blocked_auth').length
  const isFiltered = taskFilter !== 'all' || Boolean(kind)
  const emptyStateCopy = messages.resources.jobs.emptyState

  useEffect(() => {
    if (!handoffJobID) return
    const location = new URL(window.location.href)
    location.searchParams.delete('job')
    window.history.replaceState(window.history.state, '', `${location.pathname}${location.search}${location.hash}`)
    try { window.sessionStorage.removeItem('wechat-article.job-handoff.v1') } catch { /* Browser storage can be unavailable. */ }
  }, [handoffJobID])

  function updateSelection(ids: readonly string[]) {
    setSelected(ids)
    setDetailOpen(ids.length === 1)
  }

  function changePage(nextPageIndex: number) {
    setSelected([])
    setDetailOpen(false)
    setPageIndex(nextPageIndex)
  }

  /** A narrowed result set makes the current offset meaningless, so filter
      changes always return to the first page. */
  function changeFilter(next: () => void) {
    next()
    setPageIndex(0)
    setSelected([])
    setDetailOpen(false)
  }

  function control(action: JobControlAction) {
    if (!selectedID || !permittedActions.includes(action)) return
    if (action === 'resume') {
      mutations.controlJob.mutate({ id: selectedID, action }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })
      return
    }
    confirmationTrigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setConfirmationProof('')
    setConfirmationAction(action)
  }

  function closeConfirmation() {
    setConfirmationAction(undefined)
    setConfirmationProof('')
    confirmationTrigger.current = null
  }

  const confirmation = confirmationAction && selectedID ? jobConfirmation(actions, confirmationAction, selectedID) : undefined

  return <PageStack as="div">
    <JobClockProvider>
      {blockedCount > 0 ? (
        <Banner
          status="warning"
          title={messages.resources.jobs.attention.blockedTitle(blockedCount)}
          endContent={<Link href="/login">{messages.resources.jobs.attention.blockedSignIn}</Link>}
        />
      ) : null}
      <ResourceTable
        eyebrow={messages.navigation.operations}
        messages={messages.resources.jobs}
        columns={columns}
        query={query}
        pageIndex={pageIndex}
        onPageChange={changePage}
        onSelectionChange={updateSelection}
        selectionScope={`${taskFilter}|${kind ?? ''}`}
        tableToolbar={
          <JobsFilterToolbar
            taskFilter={taskFilter}
            kind={kind}
            counts={filterCounts}
            messages={messages}
            locale={locale}
            onTaskFilterChange={(filter) => changeFilter(() => setTaskFilter(filter))}
            onKindChange={(next) => changeFilter(() => setKind(next))}
          />
        }
        emptyState={
          <EmptyState
            title={isFiltered ? emptyStateCopy.filteredEmptyTitle : emptyStateCopy.firstUseTitle}
            description={isFiltered ? emptyStateCopy.filteredEmptyDescription : emptyStateCopy.firstUseDescription}
            actions={isFiltered ? (
              <Button
                label={emptyStateCopy.filteredEmptyAction}
                variant="secondary"
                onClick={() => changeFilter(() => { setTaskFilter('all'); setKind(undefined) })}
              />
            ) : undefined}
          />
        }
      />
      {isDetailOpen && selectedID ? (
        <JobDetailDrawer
          detail={detail}
          messages={messages}
          locale={locale}
          permittedActions={permittedActions}
          isControlPending={mutations.controlJob.isPending}
          notice={notice}
          onControl={control}
          onOpenChange={setDetailOpen}
        />
      ) : null}
    </JobClockProvider>
    {confirmationAction && confirmation ? (
      <TypedConfirmationDialog
        isOpen
        triggerRef={confirmationTrigger}
        onOpenChange={(isOpen) => { if (!isOpen) closeConfirmation() }}
        title={confirmation.title}
        closeLabel={messages.a11y.closeDialog}
        description={confirmation.description}
        expected={confirmation.confirmation}
        inputLabel={actions.confirmationLabel}
        inputHint={actions.confirmationHint}
        actionLabel={confirmation.actionLabel}
        cancelLabel={actions.cancelConfirmation}
        confirmation={confirmationProof}
        onConfirmationChange={setConfirmationProof}
        isActionLoading={mutations.controlJob.isPending}
        onAction={() => {
          if (!selectedID) return
          mutations.controlJob.mutate(
            { id: selectedID, action: confirmationAction, confirmation: confirmationProof },
            { onSuccess: () => { setNotice(undefined); closeConfirmation() }, onError: () => setNotice(actions.actionFailed) }
          )
        }}
      />
    ) : null}
  </PageStack>
}

function readJobHandoff(): string | undefined {
  const queryID = new URLSearchParams(window.location.search).get('job')?.trim()
  return queryID || loadJobHandoff()?.id.trim()
}
