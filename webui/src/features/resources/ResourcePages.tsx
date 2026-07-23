import { StatusDot } from '@astryxdesign/core/StatusDot'
import { Button } from '@astryxdesign/core/Button'
import { Dialog, DialogHeader } from '@astryxdesign/core/Dialog'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import { consumeArticleQueryHandoff, parseArticleQuery, saveExportHandoff, type AccountRecord, type AccountSyncMode, type AlbumRecord, type AlbumTraversalOrder, type JobControlAction, type JobDetail, type JobRecord, type SavedQueryRecord } from '../../lib/api'
import { getAccountManifestDownloadURL } from '../../lib/api'
import { loadJobHandoff } from '../../lib/jobHandoff'
import { useAccountPage, useAccountSearch, useAlbumPage, useJobDetail, useJobPage, useSavedQueryPage, useWorkspaceMutations } from '../../lib/queries'
import { ResourceTable } from './ResourceTable'
import { UnavailableActionPanel } from '../actions/UnavailableActionPanel'
import { navigateTo } from '../../app/navigation'

const pageSize = 25
const maximumSelectedAlbumIDs = 50

export function AccountsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [search, setSearch] = useState('')
  const [fakeid, setFakeid] = useState('')
  const [name, setName] = useState('')
  const [alias, setAlias] = useState('')
  const [syncMode, setSyncMode] = useState<AccountSyncMode>('incremental')
  const [notice, setNotice] = useState<string>()
  const [isDeleteConfirmationOpen, setDeleteConfirmationOpen] = useState(false)
  const [deleteConfirmation, setDeleteConfirmation] = useState('')
  const query = useAccountPage({ page: pageIndex + 1, pageSize })
  const discovery = useAccountSearch({ page: 1, pageSize, search })
  const mutations = useWorkspaceMutations()
  const columns = useMemo<ColumnDef<AccountRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.accounts.columns.name },
    { accessorKey: 'alias', header: messages.resources.accounts.columns.alias, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' },
    { accessorKey: 'articleCount', header: messages.resources.accounts.columns.articles, cell: ({ getValue }) => getValue<number | undefined>() ?? 0 },
    { accessorKey: 'lastSyncAt', header: messages.resources.accounts.columns.synced, cell: ({ getValue }) => formatDate(getValue<string | undefined>(), locale) },
    { accessorKey: 'syncCompleted', header: messages.resources.accounts.columns.state, cell: ({ getValue }) => <State value={getValue<boolean | undefined>() ? 'ready' : 'queued'} locale={locale} /> }
  ], [locale, messages])
  const actions = messages.resources.accounts.actions
  const one = selected.length === 1 ? selected[0] : undefined
  const accountInput = { fakeid: fakeid.trim(), name: name.trim(), alias: alias.trim() || undefined }
  const selectDiscoveryCandidate = (account: AccountRecord) => {
    setFakeid(account.fakeid?.trim() ?? '')
    setName(account.name)
    setAlias(account.alias ?? '')
    setNotice(actions.candidateSelected(account.name))
  }
  const saveAccount = () => {
    mutations.saveAccount.mutate(accountInput, {
      onSuccess: (account) => {
        setSelected([account.id])
        setNotice(actions.saved(account.name))
      },
      onError: () => setNotice(actions.actionFailed)
    })
  }
  const importManifest = (manifest: File | undefined) => {
    if (!manifest) return
    mutations.uploadAccountManifest.mutate(manifest, {
      onSuccess: (upload) => mutations.importAccountManifest.mutate(upload.handle, {
        onSuccess: ({ report }) => setNotice(actions.manifestImported(report.added, report.merged, report.unchanged)),
        onError: () => setNotice(actions.manifestFailed)
      }),
      onError: () => setNotice(actions.manifestFailed)
    })
  }
  return (
    <>
      <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.accounts} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
      <UnavailableActionPanel messages={messages} title={actions.title} description={actions.description}>
        <form className="account-action-form" onSubmit={(event) => { event.preventDefault(); void discovery.refetch() }}><TextInput label={actions.search} value={search} onChange={setSearch} isRequired /><Button label={actions.discover} type="submit" variant="secondary" isLoading={discovery.isFetching} isDisabled={!search.trim()} /></form>
        {discovery.data ? <section className="discovery-results" aria-labelledby="discovery-results-title" aria-live="polite"><h3 id="discovery-results-title">{actions.discoveryResults}</h3>{discovery.data.data.length === 0 ? <p>{actions.discoveryEmpty}</p> : <ul>{discovery.data.data.map((account) => <li key={account.id}><div><strong>{account.name}</strong>{account.alias ? <span>{account.alias}</span> : null}</div><Button label={actions.useCandidate} variant="secondary" size="sm" onClick={() => selectDiscoveryCandidate(account)} /></li>)}</ul>}</section> : null}
        <form className="account-action-form" onSubmit={(event) => { event.preventDefault(); if (accountInput.fakeid && accountInput.name) saveAccount() }}><TextInput label={actions.fakeid} value={fakeid} onChange={setFakeid} isRequired /><TextInput label={actions.name} value={name} onChange={setName} isRequired /><TextInput label={actions.alias} value={alias} onChange={setAlias} isOptional /><Button label={actions.add} type="submit" variant="primary" isLoading={mutations.saveAccount.isPending} isDisabled={!accountInput.fakeid || !accountInput.name} /><Button label={actions.edit} variant="secondary" isLoading={mutations.updateAccount.isPending} isDisabled={!one || !accountInput.fakeid || !accountInput.name} onClick={() => one && mutations.updateAccount.mutate({ id: one, input: accountInput }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })} /><label className="account-sync-mode">{actions.syncMode}<select aria-label={actions.syncMode} value={syncMode} onChange={(event) => setSyncMode(event.target.value as AccountSyncMode)}><option value="incremental">{actions.incremental}</option><option value="full">{actions.full}</option></select><span className="field-hint">{syncMode === 'incremental' ? actions.incrementalHint : actions.fullHint}</span></label><Button label={actions.sync} variant="secondary" isLoading={mutations.syncAccount.isPending} isDisabled={!one} onClick={() => one && mutations.syncAccount.mutate({ id: one, mode: syncMode }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })} /><Button label={actions.remove} variant="secondary" isLoading={mutations.deleteAccounts.isPending} isDisabled={selected.length === 0} onClick={() => { setDeleteConfirmation(''); setDeleteConfirmationOpen(true) }} /></form>
        <div className="account-action-form"><a className="artifact-download" href={getAccountManifestDownloadURL()}>{actions.downloadManifest}</a><label>{actions.importManifest}<input type="file" accept="application/json,.json" disabled={mutations.uploadAccountManifest.isPending || mutations.importAccountManifest.isPending} onChange={(event) => { const manifest = event.currentTarget.files?.[0]; event.currentTarget.value = ''; importManifest(manifest) }} /></label><p className="field-hint">{actions.manifestHint}</p></div>
        {!one && selected.length > 0 ? <p>{actions.selectOne}</p> : null}{notice ? <p role="alert">{notice}</p> : null}
      </UnavailableActionPanel>
      <TypedConfirmationDialog isOpen={isDeleteConfirmationOpen} onOpenChange={setDeleteConfirmationOpen} title={actions.deleteTitle} description={actions.deleteConfirm} expected={actions.deleteConfirmation(selected)} inputLabel={actions.deleteConfirmationLabel} inputHint={actions.deleteConfirmationHint} actionLabel={actions.confirmDelete} cancelLabel={actions.cancelDelete} confirmation={deleteConfirmation} onConfirmationChange={setDeleteConfirmation} isActionLoading={mutations.deleteAccounts.isPending} onAction={() => mutations.deleteAccounts.mutate({ ids: selected, confirmation: deleteConfirmation }, { onSuccess: () => { setSelected([]); setNotice(undefined); setDeleteConfirmationOpen(false); setDeleteConfirmation('') }, onError: () => setNotice(actions.actionFailed) })} />
    </>
  )
}

export function AlbumsPage({ messages }: { readonly messages: MessageCatalog }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [accountId, setAccountId] = useState('')
  const [keyword, setKeyword] = useState('')
  const [order, setOrder] = useState<AlbumTraversalOrder>('forward')
  const [notice, setNotice] = useState<string>()
  const query = useAlbumPage({ page: pageIndex + 1, pageSize, accountId, keyword })
  const mutations = useWorkspaceMutations()
  const columns = useMemo<ColumnDef<AlbumRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.albums.columns.name },
    { accessorKey: 'articleCount', header: messages.resources.albums.columns.articles },
    { accessorKey: 'paid', header: messages.resources.albums.columns.paid, cell: ({ getValue }) => getValue<boolean | undefined>() ? '✓' : '—' },
    { accessorKey: 'description', header: messages.resources.albums.columns.description, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' }
  ], [messages])
  const album = selected.length === 1 ? query.data?.data.find((item) => item.id === selected[0]) : undefined
  const traverse = (download: boolean) => {
    if (!album?.accountId) return setNotice(messages.resources.albums.actions.selectOne)
    mutations.traverseAlbum.mutate({ albumId: album.id, accountId: album.accountId, order, download }, { onSuccess: (job) => setNotice(messages.resources.albums.actions.queued(job.id)), onError: () => setNotice(messages.resources.albums.actions.failed) })
  }
  const handoffExport = () => {
    if (!album?.accountId) return
    saveExportHandoff({ selection: { kind: 'album', albumId: album.id }, label: messages.exports.selection.albumLabel(album.id) })
    navigateTo('/exports')
  }
  const selectionScope = `${accountId}\u0000${keyword}`
  const updateFilter = (set: (value: string) => void) => (value: string) => {
    set(value)
    setPageIndex(0)
    setSelected([])
  }
  return <>
    <section className="unavailable-actions" aria-labelledby="album-filters-title">
      <div><h2 id="album-filters-title">{messages.resources.albums.filters.title}</h2><p>{messages.resources.albums.filters.description}</p></div>
      <div className="account-action-form"><TextInput label={messages.resources.albums.filters.accountId} value={accountId} onChange={updateFilter(setAccountId)} /><TextInput label={messages.resources.albums.filters.keyword} value={keyword} onChange={updateFilter(setKeyword)} /></div>
    </section>
    <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.albums} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} preserveSelectionAcrossPages maximumSelectedIDs={maximumSelectedAlbumIDs} selectionScope={selectionScope} />
    <section className="unavailable-actions" aria-labelledby="album-actions-title">
      <div><h2 id="album-actions-title">{messages.resources.albums.actions.title}</h2><p>{messages.resources.albums.actions.description}</p></div>
      <label>{messages.resources.albums.actions.order}<select aria-label={messages.resources.albums.actions.order} value={order} onChange={(event) => setOrder(event.target.value as AlbumTraversalOrder)}><option value="forward">{messages.resources.albums.actions.forward}</option><option value="reverse">{messages.resources.albums.actions.reverse}</option></select></label>
      <div className="action-button-group"><Button label={messages.resources.albums.actions.traverse} variant="secondary" isLoading={mutations.traverseAlbum.isPending} isDisabled={!album?.accountId} onClick={() => traverse(false)} /><Button label={messages.resources.albums.actions.download} variant="primary" isLoading={mutations.traverseAlbum.isPending} isDisabled={!album?.accountId} onClick={() => traverse(true)} /><Button label={messages.resources.albums.actions.export} variant="secondary" isDisabled={!album?.accountId} onClick={handoffExport} /></div>
      {notice ? <p role="status">{notice}</p> : null}
    </section>
  </>
}

export function JobsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [handoffJobID] = useState(readJobHandoff)
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>(() => handoffJobID ? [handoffJobID] : [])
  const [notice, setNotice] = useState<string>()
  const [confirmationAction, setConfirmationAction] = useState<JobConfirmationAction>()
  const [confirmationProof, setConfirmationProof] = useState('')
  const query = useJobPage({ page: pageIndex + 1, pageSize })
  const detail = useJobDetail(selected.length === 1 ? selected[0] : undefined)
  const mutations = useWorkspaceMutations()
  const columns = useMemo<ColumnDef<JobRecord>[]>(() => [
    { accessorKey: 'kind', header: messages.resources.jobs.columns.kind },
    { accessorKey: 'state', header: messages.resources.jobs.columns.state, cell: ({ getValue }) => <State value={getValue<string>()} locale={locale} /> },
    { accessorKey: 'createdAt', header: messages.resources.jobs.columns.created, cell: ({ getValue }) => formatDate(getValue<string>(), locale) },
    { accessorKey: 'updatedAt', header: messages.resources.jobs.columns.updated, cell: ({ getValue }) => formatDate(getValue<string>(), locale) },
    { accessorKey: 'counts', header: messages.resources.jobs.columns.counts, cell: ({ getValue }) => formatCounts(getValue<Readonly<Record<string, number>> | undefined>()) }
  ], [locale, messages])
  const actions = messages.resources.jobs.actions
  const one = selected.length === 1 ? selected[0] : undefined
  const permittedActions = detail.data?.job.permittedActions ?? []
  const isPermitted = (action: JobControlAction) => Boolean(one && detail.isSuccess && permittedActions.includes(action))

  useEffect(() => {
    const location = new URL(window.location.href)
    if (!handoffJobID) return
    location.searchParams.delete('job')
    window.history.replaceState(window.history.state, '', `${location.pathname}${location.search}${location.hash}`)
    try { window.sessionStorage.removeItem('wechat-article.job-handoff.v1') } catch { /* Browser storage can be unavailable. */ }
  }, [handoffJobID])

  const changePage = (nextPageIndex: number) => {
    setSelected([])
    setPageIndex(nextPageIndex)
  }
  const control = (action: 'pause' | 'resume' | 'retry' | 'cancel') => {
    if (!one || !isPermitted(action)) return setNotice(actions.selectOne)
    if (action === 'resume') return mutations.controlJob.mutate({ id: one, action }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })
    setConfirmationProof('')
    setConfirmationAction(action)
  }
  const confirmation = confirmationAction && one ? jobConfirmation(actions, confirmationAction, one) : undefined
  return (
    <>
      <ResourceTable eyebrow={messages.navigation.operations} messages={messages.resources.jobs} columns={columns} query={query} pageIndex={pageIndex} onPageChange={changePage} onSelectionChange={setSelected} />
      <UnavailableActionPanel messages={messages} title={actions.title} description={actions.description}>
        <Button label={actions.pause} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!isPermitted('pause')} onClick={() => control('pause')} /><Button label={actions.resume} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!isPermitted('resume')} onClick={() => control('resume')} /><Button label={actions.retry} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!isPermitted('retry')} onClick={() => control('retry')} /><Button label={actions.cancel} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!isPermitted('cancel')} onClick={() => control('cancel')} />
        {notice ? <p role="alert">{notice}</p> : null}
      </UnavailableActionPanel>
      <TypedConfirmationDialog isOpen={Boolean(confirmationAction)} onOpenChange={(isOpen) => { if (!isOpen) { setConfirmationAction(undefined); setConfirmationProof('') } }} title={confirmation?.title ?? ''} description={confirmation?.description ?? ''} expected={confirmation?.confirmation ?? ''} inputLabel={actions.confirmationLabel} inputHint={actions.confirmationHint} actionLabel={confirmation?.actionLabel ?? ''} cancelLabel={actions.cancelConfirmation} confirmation={confirmationProof} onConfirmationChange={setConfirmationProof} isActionLoading={mutations.controlJob.isPending} onAction={() => { if (one && confirmationAction) mutations.controlJob.mutate({ id: one, action: confirmationAction, confirmation: confirmationProof }, { onSuccess: () => { setNotice(undefined); setConfirmationAction(undefined); setConfirmationProof('') }, onError: () => setNotice(actions.actionFailed) }) }} />
      {one ? <JobDetailPanel detail={detail} messages={messages} locale={locale} shouldFocus={one === handoffJobID} /> : null}
    </>
  )
}

function JobDetailPanel({ detail, messages, locale, shouldFocus }: { readonly detail: ReturnType<typeof useJobDetail>; readonly messages: MessageCatalog; readonly locale: Locale; readonly shouldFocus: boolean }) {
  const copy = messages.resources.jobs.detail
  const title = useRef<HTMLHeadingElement>(null)
  useEffect(() => {
    if (shouldFocus && (detail.isSuccess || detail.isError)) title.current?.focus()
  }, [detail.isError, detail.isSuccess, shouldFocus])
  if (detail.isLoading) return <section className="job-detail" aria-live="polite"><h2 ref={title} tabIndex={-1}>{copy.title}</h2><p role="status">{copy.loading}</p></section>
  if (detail.isError) return <section className="job-detail" aria-live="polite"><h2 ref={title} tabIndex={-1}>{copy.title}</h2><div className="error-state" role="alert"><p>{copy.unavailable}</p><Button label={copy.refresh} variant="secondary" onClick={() => void detail.refetch()} /></div></section>
  if (!detail.data) return null
  return <JobDetailContents title={title} detail={detail.data} messages={messages} locale={locale} refreshing={detail.isFetching} onRefresh={() => void detail.refetch()} />
}

function JobDetailContents({ title, detail, messages, locale, refreshing, onRefresh }: { readonly title: React.RefObject<HTMLHeadingElement | null>; readonly detail: JobDetail; readonly messages: MessageCatalog; readonly locale: Locale; readonly refreshing: boolean; readonly onRefresh: () => void }) {
  const copy = messages.resources.jobs.detail
  return <section className="job-detail" aria-labelledby="job-detail-title" aria-busy={refreshing}>
    <header className="job-detail-header"><div><h2 ref={title} id="job-detail-title" tabIndex={-1}>{copy.title}</h2><p>{copy.description}</p></div><Button label={copy.refresh} variant="secondary" isLoading={refreshing} onClick={onRefresh} /></header>
    <p className="detail-refreshed" role="status">{refreshing ? copy.refreshing : `${copy.refreshed}: ${formatDate(detail.refreshedAt, locale)}`}</p>
    <div className="job-detail-grid">
      <section><h3>{copy.lease}</h3><dl className="facts-list"><div><dt>{copy.lease}</dt><dd>{detail.lease.active ? copy.leaseActive : copy.leaseInactive}</dd></div><div><dt>{copy.expires}</dt><dd>{formatDate(detail.lease.expiresAt, locale)}</dd></div></dl></section>
      <section><h3>{copy.items}</h3>{detail.itemsLimited ? <p>{copy.itemsLimited(detail.items.length, detail.itemsTotal)}</p> : null}{detail.items.length === 0 ? <p>{copy.noItems}</p> : <ul className="job-detail-list">{detail.items.map((item) => <li key={item.id}><State value={item.state} locale={locale} /><span>{copy.attempts}: {item.attemptCount}</span>{item.errorClass ? <span>{copy.errorClass}: {item.errorClass}</span> : null}</li>)}</ul>}</section>
      <section><h3>{copy.logs}</h3>{detail.logs.length === 0 ? <p>{copy.noLogs}</p> : <ul className="job-detail-list">{detail.logs.map((log) => <li key={log.id}><strong>{log.level}</strong><span>{formatDate(log.createdAt, locale)}</span><span>{log.message}</span></li>)}</ul>}</section>
    </div>
  </section>
}

export function SavedQueriesPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [name, setName] = useState('')
  const [queryText, setQueryText] = useState(() => JSON.stringify(consumeArticleQueryHandoff() ?? { keyword: '' }, null, 2))
  const [notice, setNotice] = useState<string>()
  const [queryPendingDeletion, setQueryPendingDeletion] = useState<string>()
  const [deleteConfirmation, setDeleteConfirmation] = useState('')
  const query = useSavedQueryPage({ page: pageIndex + 1, pageSize })
  const mutations = useWorkspaceMutations()
  const columns = useMemo<ColumnDef<SavedQueryRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.savedQueries.columns.name },
    { accessorKey: 'query', header: messages.resources.savedQueries.columns.query, cell: ({ getValue }) => formatQuery(getValue<Readonly<Record<string, unknown>> | undefined>()) },
    { accessorKey: 'updatedAt', header: messages.resources.savedQueries.columns.updated, cell: ({ getValue }) => formatDate(getValue<string>(), locale) }
  ], [locale, messages])
  const actions = messages.resources.savedQueries.actions
  const selectedQuery = selected.length === 1 ? query.data?.data.find((item) => item.name === selected[0]) : undefined
  const parseInput = () => {
    const trimmedName = name.trim()
    if (!trimmedName) throw new Error('name')
    const parsed: unknown = JSON.parse(queryText)
    return { name: trimmedName, query: parseArticleQuery(parsed) }
  }
  const save = () => {
    try {
      const input = parseInput()
      mutations.saveSavedQuery.mutate(input, { onSuccess: () => { setNotice(actions.saved(input.name)) }, onError: () => setNotice(actions.actionFailed) })
    } catch {
      setNotice(actions.invalidQuery)
    }
  }
  const edit = () => {
    if (!selectedQuery) return setNotice(actions.selectOne)
    setName(selectedQuery.name)
    setQueryText(JSON.stringify(selectedQuery.query, null, 2))
    setNotice(actions.editing(selectedQuery.name))
  }
  const remove = () => {
    if (!selectedQuery) return setNotice(actions.selectOne)
    setDeleteConfirmation('')
    setQueryPendingDeletion(selectedQuery.name)
  }
  return (
    <>
      <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.savedQueries} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
      <UnavailableActionPanel messages={messages} title={actions.title} description={actions.description} showConfirmationNote>
        <div className="account-action-form"><TextInput label={actions.name} value={name} onChange={setName} /><TextInput label={actions.query} value={queryText} onChange={setQueryText} /></div>
        <Button label={actions.create} variant="primary" isLoading={mutations.saveSavedQuery.isPending} onClick={save} /><Button label={actions.edit} variant="secondary" isDisabled={!selectedQuery} onClick={edit} /><Button label={actions.remove} variant="secondary" isLoading={mutations.deleteSavedQuery.isPending} isDisabled={!selectedQuery} onClick={remove} />
        {notice ? <p role="status">{notice}</p> : null}
      </UnavailableActionPanel>
      <TypedConfirmationDialog isOpen={Boolean(queryPendingDeletion)} onOpenChange={(isOpen) => { if (!isOpen) { setQueryPendingDeletion(undefined); setDeleteConfirmation('') } }} title={actions.deleteTitle} description={queryPendingDeletion ? actions.deleteConfirm(queryPendingDeletion) : ''} expected={queryPendingDeletion ? actions.deleteConfirmation(queryPendingDeletion) : ''} inputLabel={actions.deleteConfirmationLabel} inputHint={actions.deleteConfirmationHint} actionLabel={actions.confirmDelete} cancelLabel={actions.cancelDelete} confirmation={deleteConfirmation} onConfirmationChange={setDeleteConfirmation} isActionLoading={mutations.deleteSavedQuery.isPending} onAction={() => { if (queryPendingDeletion) mutations.deleteSavedQuery.mutate({ name: queryPendingDeletion, confirmation: deleteConfirmation }, { onSuccess: () => { setSelected([]); setNotice(actions.deleted(queryPendingDeletion)); setQueryPendingDeletion(undefined); setDeleteConfirmation('') }, onError: () => setNotice(actions.actionFailed) }) }} />
    </>
  )
}

type JobConfirmationAction = 'pause' | 'retry' | 'cancel'

function jobConfirmation(actions: MessageCatalog['resources']['jobs']['actions'], action: JobConfirmationAction, id: string) {
  const confirmations = {
    pause: { title: actions.pauseTitle, description: actions.confirmPause, actionLabel: actions.pause, confirmation: actions.pauseConfirmation(id) },
    retry: { title: actions.retryTitle, description: actions.confirmRetry, actionLabel: actions.retry, confirmation: actions.retryConfirmation(id) },
    cancel: { title: actions.cancelTitle, description: actions.confirmCancel, actionLabel: actions.cancel, confirmation: actions.cancelConfirmationProof(id) }
  } as const
  return confirmations[action]
}

type TypedConfirmationDialogProps = {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description: string
  readonly expected: string
  readonly inputLabel: string
  readonly inputHint: string
  readonly actionLabel: string
  readonly cancelLabel: string
  readonly confirmation: string
  readonly onConfirmationChange: (value: string) => void
  readonly isActionLoading: boolean
  readonly onAction: () => void
}

function TypedConfirmationDialog({ isOpen, onOpenChange, title, description, expected, inputLabel, inputHint, actionLabel, cancelLabel, confirmation, onConfirmationChange, isActionLoading, onAction }: TypedConfirmationDialogProps) {
  const close = () => onOpenChange(false)
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (confirmation === expected) onAction()
  }
  return <Dialog isOpen={isOpen} onOpenChange={onOpenChange} purpose="form" role="alertdialog" aria-label={title}>
    <DialogHeader title={title} subtitle={description} onOpenChange={onOpenChange} />
    <form className="typed-confirmation-dialog" onSubmit={submit}>
      <div className="confirmation-proof"><strong>{inputLabel}</strong><code>{expected}</code><p>{inputHint}</p></div>
      <TextInput label={inputLabel} value={confirmation} onChange={onConfirmationChange} isRequired hasAutoFocus />
      <div className="action-button-group"><Button label={actionLabel} variant="destructive" type="submit" isLoading={isActionLoading} isDisabled={confirmation !== expected} /><Button label={cancelLabel} variant="secondary" isDisabled={isActionLoading} onClick={close} /></div>
    </form>
  </Dialog>
}

function State({ value, locale }: { readonly value: string; readonly locale: Locale }) {
  const success = value === 'completed' || value === 'ready'
  const error = value === 'failed' || value === 'cancelled'
  const variant = success ? 'success' : error ? 'error' : value === 'running' ? 'warning' : 'neutral'
  const labels = locale === 'zh-CN' ? { ready: '已就绪', queued: '已排队', completed: '已完成', running: '运行中', failed: '失败', cancelled: '已取消' } : { ready: 'Ready', queued: 'Queued', completed: 'Completed', running: 'Running', failed: 'Failed', cancelled: 'Cancelled' }
  return <span className="article-status"><StatusDot variant={variant} label={labels[value as keyof typeof labels] ?? value} />{labels[value as keyof typeof labels] ?? value}</span>
}

function formatDate(value: string | undefined, locale: Locale) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function formatCounts(value: Readonly<Record<string, number>> | undefined) {
  if (!value) return '—'
  const entries = Object.entries(value)
  return entries.length === 0 ? '—' : entries.map(([key, count]) => `${key}: ${count}`).join(' · ')
}

function readJobHandoff(): string | undefined {
  const queryID = new URLSearchParams(window.location.search).get('job')?.trim()
  return queryID || loadJobHandoff()?.id.trim()
}

function formatQuery(value: Readonly<Record<string, unknown>> | undefined) {
  if (!value) return '—'
  const entries = Object.entries(value).filter(([, field]) => field !== undefined && field !== '' && field !== null)
  return entries.length === 0 ? '—' : entries.slice(0, 3).map(([key, field]) => `${key}: ${String(field)}`).join(' · ')
}
