import { Button } from '@/components/controls/Button'
import { Selector } from '@/components/controls/Selector'
import { TextInput } from '@/components/controls/TextInput'
import { AccountRemoteSelector, EmptyState, FieldHint, PageHeader, PageStack, Panel, SectionHeader, SelectionActionBar, Status, TypedConfirmationDialog } from '../../components/presentation'
import { useDebounce } from '../../hooks/use-debounce'
import { useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import { ApiError, resolveAccountFromArticle, resolveAccountName, saveExportHandoff, type AccountRecord, type AccountSyncMode, type AlbumTraversalOrder } from '../../lib/api'
import { getAccountManifestDownloadURL } from '../../lib/api'
import { useAccountPage, useAccountSearch, useAlbumPage, useSessionStatus, useWorkspaceMutations } from '../../lib/queries'
import { usePagedBrowserView } from '../../lib/usePagedBrowserView'
import { ResourceTable } from './ResourceTable'
import { navigateTo } from '../../app/navigation'
import { handoffCreatedJob } from '../../lib/jobHandoff'
import { AccountAddDrawer } from './accounts/AccountAddDrawer'
import { AccountTableToolbar } from './accounts/AccountTableToolbar'
import type { AccountDraft, AccountEntryMode } from './accounts/accountEntryTypes'
import { AlbumSelectionDetails, type AlbumRecordWithAccountName } from './albums/AlbumSelectionDetails'
export { JobsPage } from './jobs/JobsPage'
export { SavedQueriesPage } from './saved-queries/SavedQueriesPage'

const pageSize = 25
const maximumSelectedAlbumIDs = 50

export function AccountsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = usePagedBrowserView()
  const [selected, setSelected] = useState<readonly string[]>([])
  const [search, setSearch] = useState('')
  const [draft, setDraft] = useState<AccountDraft>({ fakeid: '', name: '', alias: '' })
  const [entryMode, setEntryMode] = useState<AccountEntryMode>('create')
  const [isEntryOpen, setEntryOpen] = useState(false)
  const [syncMode, setSyncMode] = useState<AccountSyncMode>('incremental')
  const [notice, setNotice] = useState<string>()
  const [discoveryError, setDiscoveryError] = useState<string | undefined>()
  const [articleURL, setArticleURL] = useState('')
  const [isResolving, setResolving] = useState(false)
  const [resolveError, setResolveError] = useState<string | undefined>()
  const [resolvedName, setResolvedName] = useState<string | undefined>()
  const [isDeleteConfirmationOpen, setDeleteConfirmationOpen] = useState(false)
  const [deleteConfirmation, setDeleteConfirmation] = useState('')
  const deleteTriggerRef = useRef<HTMLElement | null>(null)
  const query = useAccountPage({ page: pageIndex + 1, pageSize })
  const discovery = useAccountSearch({ page: 1, pageSize, search })
  const session = useSessionStatus()
  const mutations = useWorkspaceMutations()
  // Distinguish "confirmed unauthenticated" from "still loading" so the drawer
  // never flashes a sign-in prompt while the session query is in flight.
  const isAuthenticated = session.data?.state === 'authenticated'
  const isSessionResolved = !session.isLoading && !session.isError
  const columns = useMemo<ColumnDef<AccountRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.accounts.columns.name, meta: { role: 'primaryText' } },
    { accessorKey: 'alias', header: messages.resources.accounts.columns.alias, meta: { role: 'secondaryText' }, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' },
    { accessorKey: 'lastSyncAt', header: messages.resources.accounts.columns.synced, meta: { role: 'dateTime' }, cell: ({ getValue }) => formatDate(getValue<string | undefined>(), locale) },
    { accessorKey: 'messageCount', header: messages.resources.accounts.columns.messages, meta: { role: 'numeric' }, cell: ({ getValue }) => getValue<number | undefined>() ?? 0 },
    { accessorKey: 'articleCount', header: messages.resources.accounts.columns.articles, meta: { role: 'numeric' }, cell: ({ getValue }) => getValue<number | undefined>() ?? 0 },
    { id: 'syncProgress', header: messages.resources.accounts.columns.progress, meta: { role: 'numeric' }, cell: ({ row }) => formatSyncProgress(row.original) },
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
    setDiscoveryError(undefined)
    setArticleURL('')
    setResolveError(undefined)
    setResolvedName(undefined)
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
  const discoverAccounts = async () => {
    setDiscoveryError(undefined)
    const result = await discovery.refetch()
    if (result.status === 'error') {
      setDiscoveryError(result.error instanceof ApiError && result.error.status === 401 ? actions.discoveryFailedAuth : actions.discoveryFailedGeneric)
    }
  }
  const resolveFromArticle = async () => {
    const trimmed = articleURL.trim()
    if (!trimmed) return
    setResolving(true)
    setResolveError(undefined)
    setResolvedName(undefined)
    try {
      if (isAuthenticated) {
        const account = await resolveAccountFromArticle(trimmed)
        setDraft({ fakeid: account.fakeid?.trim() ?? '', name: account.name, alias: account.alias ?? '' })
        setNotice(actions.candidateSelected(account.name))
      } else {
        const name = await resolveAccountName(trimmed)
        setResolvedName(name)
      }
    } catch (error) {
      setResolveError(error instanceof ApiError && error.status === 401 ? actions.discoveryFailedAuth : actions.articleLinkFailed)
    } finally {
      setResolving(false)
    }
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
  }
  const isEmpty = !query.isLoading && !query.isError && (query.data?.data.length ?? 0) === 0
  const syncSelected = () => {
    if (selected.length === 0) return
    if (one) {
      mutations.syncAccount.mutate({ id: one, mode: syncMode }, {
        onSuccess: () => setNotice(undefined),
        onError: () => setNotice(actions.actionFailed)
      })
      return
    }
    mutations.syncAccounts.mutate({ ids: selected, mode: syncMode }, {
      onSuccess: () => setNotice(undefined),
      onError: () => setNotice(actions.actionFailed)
    })
  }
  const exportSelectedManifest = () => {
    if (selected.length === 0) return
    window.location.assign(getAccountManifestDownloadURL(selected))
  }
  return (
    <PageStack as="div">
      <ResourceTable
        eyebrow={messages.navigation.library}
        messages={messages.resources.accounts}
        columns={columns}
        query={query}
        pageIndex={pageIndex}
        onPageChange={setPageIndex}
        onSelectionChange={setSelected}
        selectionLabel={(count) => `${count} ${messages.resources.accounts.selected}`}
        tableToolbar={<AccountTableToolbar
          actions={actions}
          toolbarLabel={actions.toolbarLabel}
          selectionActionsLabel={actions.selectionActions}
          selectedCount={selected.length}
          syncMode={syncMode}
          isSyncing={mutations.syncAccount.isPending || mutations.syncAccounts.isPending}
          isDeleting={mutations.deleteAccounts.isPending}
          isManifestImporting={mutations.uploadAccountManifest.isPending || mutations.importAccountManifest.isPending}
          onAdd={openNewEntry}
          onImport={importManifest}
          onExport={exportSelectedManifest}
          exportLabel={actions.downloadManifest}
          onEdit={openEditEntry}
          onDelete={openDeleteConfirmation}
          onSync={syncSelected}
          onSyncModeChange={setSyncMode}
        />}
        emptyState={isEmpty ? <EmptyState title={messages.resources.accounts.emptyState.title} description={messages.resources.accounts.emptyState.description} headingLevel={3} actions={<Button label={messages.resources.accounts.emptyState.addAccount} variant="primary" onClick={openNewEntry} />} /> : undefined}
      />
      {selected.length > 0 ? <FieldHint>{syncMode === 'incremental' ? actions.incrementalHint : actions.fullHint}</FieldHint> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {isEntryOpen ? <AccountAddDrawer isOpen onOpenChange={closeEntry} mode={entryMode} actions={actions} closeLabel={messages.a11y.closeDialog} draft={draft} onDraftChange={setDraft} onSubmit={entryMode === 'edit' ? updateAccount : saveAccount} isSubmitting={entryMode === 'edit' ? mutations.updateAccount.isPending : mutations.saveAccount.isPending} isAuthenticated={isAuthenticated} isSessionResolved={isSessionResolved} search={search} onSearchChange={setSearch} onDiscover={() => { void discoverAccounts() }} isDiscovering={discovery.isFetching} discoveryError={discoveryError} candidates={discovery.data?.data} onCandidateSelect={selectDiscoveryCandidate} articleURL={articleURL} onArticleURLChange={setArticleURL} onResolve={() => { void resolveFromArticle() }} isResolving={isResolving} resolveError={resolveError} resolvedName={resolvedName} /> : null}
      {isDeleteConfirmationOpen ? <TypedConfirmationDialog isOpen onOpenChange={closeDeleteConfirmation} triggerRef={deleteTriggerRef} title={actions.deleteTitle} closeLabel={messages.a11y.closeDialog} description={actions.deleteConfirm} expected={actions.deleteConfirmation(selected)} inputLabel={actions.deleteConfirmationLabel} inputHint={actions.deleteConfirmationHint} actionLabel={actions.confirmDelete} cancelLabel={actions.cancelDelete} confirmation={deleteConfirmation} onConfirmationChange={setDeleteConfirmation} isActionLoading={mutations.deleteAccounts.isPending} onAction={() => mutations.deleteAccounts.mutate({ ids: selected, confirmation: deleteConfirmation }, { onSuccess: () => { setSelected([]); setNotice(undefined); setDeleteConfirmationOpen(false); setDeleteConfirmation('') }, onError: () => setNotice(actions.actionFailed) })} /> : null}
    </PageStack>
  )
}

export function AlbumsPage({ messages }: { readonly messages: MessageCatalog }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [accountId, setAccountId] = useState<string>()
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword, 300)
  const [order, setOrder] = useState<AlbumTraversalOrder>('forward')
  const [notice, setNotice] = useState<string>()
  const query = useAlbumPage({ page: pageIndex + 1, pageSize, accountId, keyword: debouncedKeyword })
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
  // Scope tracks the *committed* filter, not the raw input: selections survive a
  // typing burst (debouncedKeyword is unchanged until it settles) but are dropped
  // once the result set actually changes, so no row stays selected while invisible.
  const selectionScope = `${accountId ?? ''}\u0000${debouncedKeyword}`
  const changeKeyword = (value: string) => {
    setKeyword(value)
    setPageIndex(0)
  }
  return <PageStack as="div">
    <PageHeader eyebrow={messages.navigation.library} title={messages.resources.albums.title} titleId="albums-title" description={messages.resources.albums.description} />
    <Panel aria-labelledby="album-filters-title">
      <SectionHeader title={messages.resources.albums.filters.title} titleId="album-filters-title" description={messages.resources.albums.filters.description} />
      <div className="account-action-form"><AccountRemoteSelector label={messages.resources.accounts.columns.name} value={accountId} onChange={(next) => { setAccountId(next); setPageIndex(0) }} placeholder={messages.articles.filters.any} copy={messages.selectors} /><TextInput label={messages.resources.albums.filters.keyword} value={keyword} onChange={changeKeyword} /></div>
    </Panel>
    <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.albums} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={(ids) => { setSelected(ids); setNotice(undefined) }} preserveSelectionAcrossPages maximumSelectedIDs={maximumSelectedAlbumIDs} onSelectionLimited={(limit, current) => setNotice(messages.resources.albums.actions.selectionLimit(limit, current))} selectionScope={selectionScope} hideHeader />
    <SelectionActionBar selectedCount={selected.length} countLabel={(count) => messages.resources.albums.actions.selectedCountWithLimit(count, maximumSelectedAlbumIDs)} toolbarLabel={messages.resources.albums.actions.title} actions={<><Selector label={messages.resources.albums.actions.order} options={[{ value: 'forward', label: messages.resources.albums.actions.forward }, { value: 'reverse', label: messages.resources.albums.actions.reverse }]} value={order} onChange={(next) => setOrder(next as AlbumTraversalOrder)} /><Button label={messages.resources.albums.actions.traverse} variant="secondary" isLoading={mutations.traverseAlbum.isPending || mutations.traverseAlbums.isPending} onClick={() => traverse(false)} /><Button label={messages.resources.albums.actions.download} variant="primary" isLoading={mutations.traverseAlbum.isPending || mutations.traverseAlbums.isPending} onClick={() => traverse(true)} /><Button label={messages.resources.albums.actions.export} variant="secondary" onClick={handoffExport} /></>} />
    <AlbumSelectionDetails album={album} messages={messages} />
    {notice ? <p role="status">{notice}</p> : null}
  </PageStack>
}

function formatDate(value: string | undefined, locale: Locale) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function formatSyncProgress(account: AccountRecord) {
  const total = account.upstreamTotal ?? 0
  const synchronized = account.messageCount ?? account.syncCursor ?? 0
  if (total <= 0) return '—'
  return `${Math.min(100, Math.round((synchronized / total) * 100))}%`
}

function albumAccountName(album: AlbumRecordWithAccountName, unavailable: string) {
  return album.accountName?.trim() || (!album.accountNameAvailable ? unavailable : '—')
}
