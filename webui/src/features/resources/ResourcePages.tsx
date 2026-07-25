import { Button } from '@astryxdesign/core/Button'
import { Dialog, DialogHeader } from '@astryxdesign/core/Dialog'
import { Selector } from '@astryxdesign/core/Selector'
import { TextInput } from '@astryxdesign/core/TextInput'
import { AccountRemoteSelector, ContentCluster, PageStack, SelectionActionBar, Status } from '../../components/presentation'
import { useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import { saveExportHandoff, type AccountRecord, type AccountSyncMode, type AlbumTraversalOrder } from '../../lib/api'
import { getAccountManifestDownloadURL } from '../../lib/api'
import { useAccountPage, useAccountSearch, useAlbumPage, useWorkspaceMutations } from '../../lib/queries'
import { ResourceTable } from './ResourceTable'
import { navigateTo } from '../../app/navigation'
import { handoffCreatedJob } from '../../lib/jobHandoff'
import { AccountEntryDrawer, type AccountDraft, type AccountEntryMode } from './accounts/AccountEntryDrawer'
import { AlbumSelectionDetails, type AlbumRecordWithAccountName } from './albums/AlbumSelectionDetails'
export { JobsPage } from './jobs/JobsPage'
export { SavedQueriesPage } from './saved-queries/SavedQueriesPage'

const pageSize = 25
const maximumSelectedAlbumIDs = 50

export function AccountsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [search, setSearch] = useState('')
  const [draft, setDraft] = useState<AccountDraft>({ fakeid: '', name: '', alias: '' })
  const [entryMode, setEntryMode] = useState<AccountEntryMode>('create')
  const [isEntryOpen, setEntryOpen] = useState(false)
  const [manifest, setManifest] = useState<File | null>(null)
  const [syncMode, setSyncMode] = useState<AccountSyncMode>('incremental')
  const [notice, setNotice] = useState<string>()
  const [isDeleteConfirmationOpen, setDeleteConfirmationOpen] = useState(false)
  const [deleteConfirmation, setDeleteConfirmation] = useState('')
  const deleteTriggerRef = useRef<HTMLElement | null>(null)
  const query = useAccountPage({ page: pageIndex + 1, pageSize })
  const discovery = useAccountSearch({ page: 1, pageSize, search })
  const mutations = useWorkspaceMutations()
  const columns = useMemo<ColumnDef<AccountRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.accounts.columns.name, meta: { role: 'primaryText' } },
    { accessorKey: 'alias', header: messages.resources.accounts.columns.alias, meta: { role: 'secondaryText' }, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' },
    { accessorKey: 'articleCount', header: messages.resources.accounts.columns.articles, meta: { role: 'numeric' }, cell: ({ getValue }) => getValue<number | undefined>() ?? 0 },
    { accessorKey: 'lastSyncAt', header: messages.resources.accounts.columns.synced, meta: { role: 'dateTime' }, cell: ({ getValue }) => formatDate(getValue<string | undefined>(), locale) },
    { accessorKey: 'syncCompleted', header: messages.resources.accounts.columns.state, meta: { role: 'status' }, cell: ({ getValue }) => <Status value={getValue<boolean | undefined>() ? 'ready' : 'queued'} locale={locale} /> }
  ], [locale, messages])
  const actions = messages.resources.accounts.actions
  const one = selected.length === 1 ? selected[0] : undefined
  const selectedAccount = one ? query.data?.data.find((account) => account.id === one) : undefined
  const accountInput = { fakeid: draft.fakeid.trim(), name: draft.name.trim(), alias: draft.alias.trim() || undefined }
  const closeEntry = (isOpen: boolean) => {
    setEntryOpen(isOpen)
    if (!isOpen) setEntryMode('create')
  }
  const openNewEntry = () => {
    setDraft({ fakeid: '', name: '', alias: '' })
    setEntryMode('create')
    setEntryOpen(true)
  }
  const openEditEntry = () => {
    if (!one) return
    if (selectedAccount) setDraft({ fakeid: selectedAccount.fakeid ?? '', name: selectedAccount.name, alias: selectedAccount.alias ?? '' })
    setEntryMode('edit')
    setEntryOpen(true)
  }
  const selectDiscoveryCandidate = (account: AccountRecord) => {
    setDraft({ fakeid: account.fakeid?.trim() ?? '', name: account.name, alias: account.alias ?? '' })
    setNotice(actions.candidateSelected(account.name))
  }
  const saveAccount = () => {
    mutations.saveAccount.mutate(accountInput, {
      onSuccess: (account) => {
        setSelected([account.id])
        setNotice(actions.saved(account.name))
        closeEntry(false)
      },
      onError: () => setNotice(actions.actionFailed)
    })
  }
  const updateAccount = () => {
    if (!one) return
    mutations.updateAccount.mutate({ id: one, input: accountInput }, {
      onSuccess: () => { setNotice(undefined); closeEntry(false) },
      onError: () => setNotice(actions.actionFailed)
    })
  }
  const importManifest = async (candidate: File) => {
    try {
      const upload = await mutations.uploadAccountManifest.mutateAsync(candidate)
      const result = await mutations.importAccountManifest.mutateAsync(upload.handle)
      setNotice(actions.manifestImported(result.report.added, result.report.merged, result.report.unchanged))
    } catch {
      setNotice(actions.manifestFailed)
    }
  }
  const openDeleteConfirmation = () => {
    deleteTriggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setDeleteConfirmation('')
    setDeleteConfirmationOpen(true)
  }
  const closeDeleteConfirmation = (isOpen: boolean) => {
    setDeleteConfirmationOpen(isOpen)
    if (isOpen) return
    setDeleteConfirmation('')
    requestAnimationFrame(() => deleteTriggerRef.current?.focus())
  }
  return (
    <PageStack as="div">
      <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.accounts} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
      <ContentCluster><Button label={actions.title} variant="primary" onClick={openNewEntry} /></ContentCluster>
      <SelectionActionBar selectedCount={selected.length} countLabel={(count) => `${count} ${messages.resources.accounts.selected}`} toolbarLabel={actions.title} actions={<>
      {one ? <><Button label={actions.edit} variant="secondary" onClick={openEditEntry} /><Selector label={actions.syncMode} options={[{ value: 'incremental', label: actions.incremental }, { value: 'full', label: actions.full }]} value={syncMode} onChange={(next) => setSyncMode(next as AccountSyncMode)} /><Button label={actions.sync} variant="secondary" isLoading={mutations.syncAccount.isPending} onClick={() => mutations.syncAccount.mutate({ id: one, mode: syncMode }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })} /></> : <p>{actions.selectOne}</p>}
      </>} moreActions={<div role="group" aria-label={actions.deleteTitle}><Button label={actions.remove} variant="destructive" isLoading={mutations.deleteAccounts.isPending} onClick={openDeleteConfirmation} /></div>} />
      {one ? <p className="field-hint">{syncMode === 'incremental' ? actions.incrementalHint : actions.fullHint}</p> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {isEntryOpen ? <AccountEntryDrawer isOpen onOpenChange={closeEntry} mode={entryMode} actions={actions} draft={draft} onDraftChange={setDraft} onSubmit={entryMode === 'edit' ? updateAccount : saveAccount} isSubmitting={entryMode === 'edit' ? mutations.updateAccount.isPending : mutations.saveAccount.isPending} search={search} onSearchChange={setSearch} onDiscover={() => { void discovery.refetch() }} isDiscovering={discovery.isFetching} candidates={discovery.data?.data} onCandidateSelect={selectDiscoveryCandidate} manifest={manifest} onManifestChange={setManifest} onManifestImport={importManifest} isManifestImporting={mutations.uploadAccountManifest.isPending || mutations.importAccountManifest.isPending} manifestDownloadURL={getAccountManifestDownloadURL()} /> : null}
      {isDeleteConfirmationOpen ? <TypedConfirmationDialog isOpen onOpenChange={closeDeleteConfirmation} title={actions.deleteTitle} description={actions.deleteConfirm} expected={actions.deleteConfirmation(selected)} inputLabel={actions.deleteConfirmationLabel} inputHint={actions.deleteConfirmationHint} actionLabel={actions.confirmDelete} cancelLabel={actions.cancelDelete} confirmation={deleteConfirmation} onConfirmationChange={setDeleteConfirmation} isActionLoading={mutations.deleteAccounts.isPending} onAction={() => mutations.deleteAccounts.mutate({ ids: selected, confirmation: deleteConfirmation }, { onSuccess: () => { setSelected([]); setNotice(undefined); setDeleteConfirmationOpen(false); setDeleteConfirmation('') }, onError: () => setNotice(actions.actionFailed) })} /> : null}
    </PageStack>
  )
}

export function AlbumsPage({ messages }: { readonly messages: MessageCatalog }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [accountId, setAccountId] = useState<string>()
  const [keyword, setKeyword] = useState('')
  const [order, setOrder] = useState<AlbumTraversalOrder>('forward')
  const [notice, setNotice] = useState<string>()
  const query = useAlbumPage({ page: pageIndex + 1, pageSize, accountId, keyword })
  const mutations = useWorkspaceMutations()
  const columns = useMemo<ColumnDef<AlbumRecordWithAccountName>[]>(() => [
    { accessorKey: 'name', header: messages.resources.albums.columns.name, meta: { role: 'primaryText' }, cell: ({ getValue, row }) => <div><strong>{getValue<string>()}</strong><span> · {messages.resources.accounts.columns.name}: {albumAccountName(row.original, messages.articles.ux.accountNameUnavailable)}</span></div> },
    { accessorKey: 'articleCount', header: messages.resources.albums.columns.articles, meta: { role: 'numeric' } },
    { accessorKey: 'paid', header: messages.resources.albums.columns.paid, meta: { role: 'status' }, cell: ({ getValue }) => getValue<boolean | undefined>() ? '✓' : '—' },
    { accessorKey: 'description', header: messages.resources.albums.columns.description, meta: { role: 'description' }, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' }
  ], [messages])
  const album = selected.length === 1 ? query.data?.data.find((item) => item.id === selected[0]) as AlbumRecordWithAccountName | undefined : undefined
  const traverse = (download: boolean) => {
    if (selected.length === 0) return setNotice(messages.resources.albums.actions.selectAtLeastOne)
    if (selected.length === 1 && album?.accountId) {
      mutations.traverseAlbum.mutate({ albumId: album.id, accountId: album.accountId, order, download }, { onSuccess: (job) => { setNotice(messages.resources.albums.actions.queued); handoffCreatedJob(job) }, onError: () => setNotice(messages.resources.albums.actions.failed) })
      return
    }
    mutations.traverseAlbums.mutate({ albumIds: selected, order, download }, { onSuccess: (job) => { setNotice(messages.resources.albums.actions.queued); handoffCreatedJob(job) }, onError: () => setNotice(messages.resources.albums.actions.failed) })
  }
  const handoffExport = () => {
    if (selected.length === 0) return
    const selection = selected.length === 1
      ? { kind: 'album' as const, albumId: selected[0] }
      : { kind: 'album_ids' as const, albumIds: selected }
    const label = selected.length === 1
      ? album?.name.trim() || messages.exports.workflow.savedAlbumFallback
      : messages.exports.selection.albumsLabel(selected.length)
    saveExportHandoff({ selection, label })
    navigateTo('/exports')
  }
  const selectionScope = `${accountId ?? ''}\u0000${keyword}`
  const updateFilter = (set: (value: string) => void) => (value: string) => {
    set(value)
    setPageIndex(0)
    setSelected([])
  }
  return <PageStack as="div">
    <section className="workspace-panel" aria-labelledby="album-filters-title">
      <div><h2 id="album-filters-title">{messages.resources.albums.filters.title}</h2><p>{messages.resources.albums.filters.description}</p></div>
      <div className="account-action-form"><AccountRemoteSelector label={messages.resources.accounts.columns.name} value={accountId} onChange={(next) => { setAccountId(next); setPageIndex(0); setSelected([]) }} placeholder={messages.articles.filters.any} copy={{ unavailable: messages.articles.ux.accountUnavailable, noResults: messages.articles.ux.selectorNoResults, duplicate: messages.articles.ux.duplicateSelection }} /><TextInput label={messages.resources.albums.filters.keyword} value={keyword} onChange={updateFilter(setKeyword)} /></div>
    </section>
    <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.albums} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} preserveSelectionAcrossPages maximumSelectedIDs={maximumSelectedAlbumIDs} selectionScope={selectionScope} />
    <SelectionActionBar selectedCount={selected.length} countLabel={(count) => `${count} ${messages.resources.albums.selected}`} toolbarLabel={messages.resources.albums.actions.title} actions={<><Selector label={messages.resources.albums.actions.order} options={[{ value: 'forward', label: messages.resources.albums.actions.forward }, { value: 'reverse', label: messages.resources.albums.actions.reverse }]} value={order} onChange={(next) => setOrder(next as AlbumTraversalOrder)} /><Button label={messages.resources.albums.actions.traverse} variant="secondary" isLoading={mutations.traverseAlbum.isPending || mutations.traverseAlbums.isPending} onClick={() => traverse(false)} /><Button label={messages.resources.albums.actions.download} variant="primary" isLoading={mutations.traverseAlbum.isPending || mutations.traverseAlbums.isPending} onClick={() => traverse(true)} /><Button label={messages.resources.albums.actions.export} variant="secondary" onClick={handoffExport} /></>} />
    <AlbumSelectionDetails album={album} messages={messages} />
    {notice ? <p role="status">{notice}</p> : null}
  </PageStack>
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

function formatDate(value: string | undefined, locale: Locale) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function albumAccountName(album: AlbumRecordWithAccountName, unavailable: string) {
  return album.accountName?.trim() || (!album.accountNameAvailable ? unavailable : '—')
}
