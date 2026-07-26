import { Button } from '@/components/controls/Button'
import { ActionGroup, DefinitionList, DetailPanel, PageStack, SectionHeader, Status, TechnicalDetails, TypedConfirmationDialog } from '../../../components/presentation'
import { formatCount, formatDateTime, formatDuration, formatJobKind, formatStatus } from '../../../lib/presentation'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../../i18n'
import type { JobControlAction, JobDetail, JobRecord } from '../../../lib/api'
import { loadJobHandoff } from '../../../lib/jobHandoff'
import { useJobDetail, useJobPage, useWorkspaceMutations } from '../../../lib/queries'
import { ResourceTable } from '../ResourceTable'
import './jobs.css'

const pageSize = 25

type JobConfirmationAction = Exclude<JobControlAction, 'resume'>

export function JobsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [handoffJobID] = useState(readJobHandoff)
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>(() => handoffJobID ? [handoffJobID] : [])
  const [isDetailOpen, setDetailOpen] = useState(Boolean(handoffJobID))
  const [notice, setNotice] = useState<string>()
  const [confirmationAction, setConfirmationAction] = useState<JobConfirmationAction>()
  const [confirmationProof, setConfirmationProof] = useState('')
  const confirmationTrigger = useRef<HTMLElement | null>(null)
  const query = useJobPage({ page: pageIndex + 1, pageSize })
  const selectedID = selected.length === 1 ? selected[0] : undefined
  const detail = useJobDetail(selectedID)
  const mutations = useWorkspaceMutations()
  const actions = messages.resources.jobs.actions
  const columns = useMemo<ColumnDef<JobRecord>[]>(() => [
    { id: 'label', header: messages.resources.jobs.title, meta: { role: 'primaryText' }, cell: ({ row }) => <JobLabel job={row.original} locale={locale} /> },
    { accessorKey: 'kind', header: messages.resources.jobs.columns.kind, meta: { role: 'secondaryText' }, cell: ({ getValue }) => formatJobKind(getValue<string>(), locale).label },
    { accessorKey: 'state', header: messages.resources.jobs.columns.state, meta: { role: 'status' }, cell: ({ getValue }) => <Status value={getValue<string>()} locale={locale} /> },
    { id: 'progress', header: messages.resources.jobs.columns.counts, meta: { role: 'numeric' }, cell: ({ row }) => <JobProgress job={row.original} locale={locale} /> },
    { id: 'duration', header: messages.resources.jobs.columns.updated, meta: { role: 'dateTime' }, cell: ({ row }) => <span title={formatDateTime(row.original.updatedAt, locale)}>{formatJobDuration(row.original, locale)}</span> }
  ], [locale, messages])
  const permittedActions = detail.data?.job.permittedActions ?? []

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
    <ResourceTable eyebrow={messages.navigation.operations} messages={messages.resources.jobs} selectorCopy={messages.selectors} columns={columns} query={query} pageIndex={pageIndex} onPageChange={changePage} onSelectionChange={updateSelection} />
    {isDetailOpen && selectedID ? <DetailPanel isOpen onOpenChange={setDetailOpen} title={messages.resources.jobs.detail.title} description={messages.resources.jobs.detail.description} closeLabel={messages.a11y.closeDialog} footer={<JobControls actions={actions} permittedActions={permittedActions} isLoading={mutations.controlJob.isPending} onControl={control} />}>
      <JobDetailContents detail={detail} messages={messages} locale={locale} />
      {notice ? <p className="jobs-notice" role="alert">{notice}</p> : null}
    </DetailPanel> : null}
    {confirmationAction && confirmation ? <TypedConfirmationDialog isOpen triggerRef={confirmationTrigger} onOpenChange={(isOpen) => { if (!isOpen) closeConfirmation() }} title={confirmation.title} closeLabel={messages.a11y.closeDialog} description={confirmation.description} expected={confirmation.confirmation} inputLabel={actions.confirmationLabel} inputHint={actions.confirmationHint} actionLabel={confirmation.actionLabel} cancelLabel={actions.cancelConfirmation} confirmation={confirmationProof} onConfirmationChange={setConfirmationProof} isActionLoading={mutations.controlJob.isPending} onAction={() => { if (selectedID) mutations.controlJob.mutate({ id: selectedID, action: confirmationAction, confirmation: confirmationProof }, { onSuccess: () => { setNotice(undefined); closeConfirmation() }, onError: () => setNotice(actions.actionFailed) }) }} /> : null}
  </PageStack>
}

function JobLabel({ job, locale }: { readonly job: JobRecord; readonly locale: Locale }) {
  const kind = formatJobKind(job.kind, locale)
  const label = job.label?.trim()
  const displayLabel = label && label.toLocaleLowerCase(locale) !== job.kind.replaceAll('_', ' ').toLocaleLowerCase(locale) ? label : kind.label
  return <div className="jobs-label"><strong>{displayLabel}</strong></div>
}

function JobProgress({ job, locale }: { readonly job: JobRecord; readonly locale: Locale }) {
  const counts = job.counts
  if (!counts || Object.keys(counts).length === 0) return '—'
  const completed = counts.completed ?? counts.succeeded ?? 0
  const total = counts.total
  const failures = counts.failed ?? counts.failures ?? 0
  const progress = total === undefined ? formatCount(completed, locale) : `${formatCount(completed, locale)} / ${formatCount(total, locale)}`
  return <span className="jobs-progress">{progress}{failures > 0 ? <span className="jobs-failures"> · {formatCount(failures, locale)} {formatStatus('failed', locale).label}</span> : null}</span>
}

function formatJobDuration(job: JobRecord, locale: Locale) {
  const created = Date.parse(job.createdAt)
  const updated = Date.parse(job.updatedAt)
  return Number.isNaN(created) || Number.isNaN(updated) ? '—' : formatDuration(Math.max(0, updated - created), locale)
}

function JobControls({ actions, permittedActions, isLoading, onControl }: { readonly actions: MessageCatalog['resources']['jobs']['actions']; readonly permittedActions: readonly JobControlAction[]; readonly isLoading: boolean; readonly onControl: (action: JobControlAction) => void }) {
  if (permittedActions.length === 0) return null
  const actionLabels: Readonly<Record<JobControlAction, string>> = { pause: actions.pause, resume: actions.resume, retry: actions.retry, cancel: actions.cancel }
  return <ActionGroup align="start" aria-label={actions.title}>{permittedActions.map((action) => <Button key={action} label={actionLabels[action]} variant={action === 'cancel' ? 'destructive' : 'secondary'} isLoading={isLoading} onClick={() => onControl(action)} />)}</ActionGroup>
}

function JobDetailContents({ detail, messages, locale }: { readonly detail: ReturnType<typeof useJobDetail>; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.resources.jobs.detail
  if (detail.isLoading) return <p role="status">{copy.loading}</p>
  if (detail.isError) return <div className="error-state" role="alert"><p>{copy.unavailable}</p><Button label={copy.refresh} variant="secondary" onClick={() => void detail.refetch()} /></div>
  if (!detail.data) return null
  return <JobDetailData detail={detail.data} messages={messages} locale={locale} refreshing={detail.isFetching} onRefresh={() => void detail.refetch()} />
}

function JobDetailData({ detail, messages, locale, refreshing, onRefresh }: { readonly detail: JobDetail; readonly messages: MessageCatalog; readonly locale: Locale; readonly refreshing: boolean; readonly onRefresh: () => void }) {
  const copy = messages.resources.jobs.detail
  const jobKind = formatJobKind(detail.job.kind, locale)
  const failures = detail.items.filter((item) => item.state === 'failed' || Boolean(item.errorClass))
  return <div className="jobs-detail" aria-busy={refreshing}>
    <header className="jobs-detail-header"><div><JobLabel job={detail.job} locale={locale} /><Status value={detail.job.state} locale={locale} /></div><Button label={copy.refresh} variant="secondary" isLoading={refreshing} onClick={onRefresh} /></header>
    <DefinitionList
      labelWidth="8rem"
      rowGap="relaxed"
      collapseAt="compact"
      items={[
        { term: messages.resources.jobs.columns.kind, description: jobKind.label },
        { term: messages.resources.jobs.columns.counts, description: <JobProgress job={detail.job} locale={locale} /> },
        { term: messages.resources.jobs.columns.updated, description: formatJobDuration(detail.job, locale) },
        { term: copy.refreshed, description: formatDateTime(detail.refreshedAt, locale) }
      ]}
    />
    {failures.length ? <JobAttentionSummary failures={failures} permittedActions={detail.job.permittedActions} copy={copy} locale={locale} /> : null}
    <section><SectionHeader level={3} title={copy.items} />{detail.itemsLimited ? <p>{copy.itemsLimited(detail.items.length, detail.itemsTotal)}</p> : null}{detail.items.length === 0 ? <p>{copy.noItems}</p> : <ul className="jobs-detail-list">{detail.items.map((item) => <li key={item.id}><Status value={item.state} locale={locale} /><span>{copy.attempts}: {formatCount(item.attemptCount, locale)}</span></li>)}</ul>}</section>
    <section><SectionHeader level={3} title={copy.logs} />{detail.logs.length === 0 ? <p>{copy.noLogs}</p> : <ul className="jobs-detail-list">{detail.logs.map((log) => <li key={log.id}><span>{formatDateTime(log.createdAt, locale)}</span><span>{log.message}</span></li>)}</ul>}</section>
    <section><SectionHeader level={3} title={copy.lease} /><DefinitionList labelWidth="8rem" rowGap="relaxed" collapseAt="compact" items={[{ term: copy.lease, description: detail.lease.active ? copy.leaseActive : copy.leaseInactive }, { term: copy.expires, description: formatDateTime(detail.lease.expiresAt, locale) }]} /></section>
    <TechnicalDetails label={copy.technicalDetails} items={[{ label: copy.jobID, value: detail.job.id, copyLabel: copy.copyID, copiedLabel: messages.a11y.copied, copyFailedLabel: messages.a11y.copyUnavailable }, { label: copy.profile, value: detail.job.profile, copyLabel: copy.copyValue, copiedLabel: messages.a11y.copied, copyFailedLabel: messages.a11y.copyUnavailable }, { label: messages.resources.jobs.columns.created, value: detail.job.createdAt, copyLabel: copy.copyValue, copiedLabel: messages.a11y.copied, copyFailedLabel: messages.a11y.copyUnavailable }, { label: messages.resources.jobs.columns.updated, value: detail.job.updatedAt, copyLabel: copy.copyValue, copiedLabel: messages.a11y.copied, copyFailedLabel: messages.a11y.copyUnavailable }]} />
  </div>
}

function JobAttentionSummary({ failures, permittedActions, copy, locale }: { readonly failures: readonly JobDetail['items'][number][]; readonly permittedActions: readonly JobControlAction[]; readonly copy: MessageCatalog['resources']['jobs']['detail']; readonly locale: Locale }) {
  const nextAction = permittedActions.includes('retry') ? copy.retryAction : copy.refreshAction
  return <section className="jobs-failure-summary"><SectionHeader level={3} title={copy.attention} /><ul className="jobs-detail-list">{failures.map((item) => <li key={item.id}><Status value={item.state} locale={locale} /><DefinitionList labelWidth="6rem" collapseAt="compact" items={[{ term: copy.reason, description: failureReason(item.errorClass, copy) }, { term: copy.impact, description: copy.itemNotCompleted }, { term: copy.nextAction, description: nextAction }]} /></li>)}</ul></section>
}

function failureReason(errorClass: string | undefined, copy: MessageCatalog['resources']['jobs']['detail']) {
  if (errorClass?.trim().toLowerCase() === 'network') return copy.networkReason
  return copy.unknownReason
}

function jobConfirmation(actions: MessageCatalog['resources']['jobs']['actions'], action: JobConfirmationAction, id: string) {
  const confirmations = {
    pause: { title: actions.pauseTitle, description: actions.confirmPause, actionLabel: actions.pause, confirmation: actions.pauseConfirmation(id) },
    retry: { title: actions.retryTitle, description: actions.confirmRetry, actionLabel: actions.retry, confirmation: actions.retryConfirmation(id) },
    cancel: { title: actions.cancelTitle, description: actions.confirmCancel, actionLabel: actions.cancel, confirmation: actions.cancelConfirmationProof(id) }
  } as const
  return confirmations[action]
}

function readJobHandoff(): string | undefined {
  const queryID = new URLSearchParams(window.location.search).get('job')?.trim()
  return queryID || loadJobHandoff()?.id.trim()
}
