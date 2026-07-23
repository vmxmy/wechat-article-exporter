import { StatusDot } from '@astryxdesign/core/StatusDot'
import { Button } from '@astryxdesign/core/Button'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import type { AccountRecord, AlbumRecord, JobRecord, SavedQueryRecord } from '../../lib/api'
import { useAccountPage, useAccountSearch, useAlbumPage, useJobPage, useSavedQueryPage, useWorkspaceMutations } from '../../lib/queries'
import { ResourceTable } from './ResourceTable'
import { UnavailableActionPanel } from '../actions/UnavailableActionPanel'

const pageSize = 25

export function AccountsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [search, setSearch] = useState('')
  const [fakeid, setFakeid] = useState('')
  const [name, setName] = useState('')
  const [alias, setAlias] = useState('')
  const [notice, setNotice] = useState<string>()
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
  return (
    <>
      <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.accounts} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
      <UnavailableActionPanel messages={messages} title={actions.title} description={actions.description}>
        <div className="account-action-form"><TextInput label={actions.search} value={search} onChange={setSearch} /><Button label={actions.discover} variant="secondary" isLoading={discovery.isFetching} onClick={() => void discovery.refetch()} /></div>
        {discovery.data?.data.length ? <p>{discovery.data.data.map((account) => `${account.name} (${account.id})`).join(' · ')}</p> : null}
        <div className="account-action-form"><TextInput label={actions.fakeid} value={fakeid} onChange={setFakeid} /><TextInput label={actions.name} value={name} onChange={setName} /><TextInput label={actions.alias} value={alias} onChange={setAlias} /><Button label={actions.add} variant="primary" isLoading={mutations.saveAccount.isPending} isDisabled={!accountInput.fakeid || !accountInput.name} onClick={() => mutations.saveAccount.mutate(accountInput, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })} /><Button label={actions.edit} variant="secondary" isLoading={mutations.updateAccount.isPending} isDisabled={!one || !accountInput.fakeid || !accountInput.name} onClick={() => one && mutations.updateAccount.mutate({ id: one, input: accountInput }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })} /><Button label={actions.sync} variant="secondary" isLoading={mutations.syncAccount.isPending} isDisabled={!one} onClick={() => one && mutations.syncAccount.mutate(one, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })} /><Button label={actions.remove} variant="secondary" isLoading={mutations.deleteAccounts.isPending} isDisabled={selected.length === 0} onClick={() => { if (window.confirm(actions.deleteConfirm)) mutations.deleteAccounts.mutate(selected, { onSuccess: () => { setSelected([]); setNotice(undefined) }, onError: () => setNotice(actions.actionFailed) }) }} /></div>
        {!one && selected.length > 0 ? <p>{actions.selectOne}</p> : null}{notice ? <p role="alert">{notice}</p> : null}
      </UnavailableActionPanel>
    </>
  )
}

export function AlbumsPage({ messages }: { readonly messages: MessageCatalog }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [notice, setNotice] = useState<string>()
  const query = useAlbumPage({ page: pageIndex + 1, pageSize })
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
    mutations.traverseAlbum.mutate({ albumId: album.id, accountId: album.accountId, download }, { onSuccess: (job) => setNotice(messages.resources.albums.actions.queued(job.id)), onError: () => setNotice(messages.resources.albums.actions.failed) })
  }
  return <>
    <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.albums} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
    <section className="unavailable-actions" aria-labelledby="album-actions-title">
      <div><h2 id="album-actions-title">{messages.resources.albums.actions.title}</h2><p>{messages.resources.albums.actions.description}</p></div>
      <div className="action-button-group"><Button label={messages.resources.albums.actions.traverse} variant="secondary" isLoading={mutations.traverseAlbum.isPending} isDisabled={!album?.accountId} onClick={() => traverse(false)} /><Button label={messages.resources.albums.actions.download} variant="primary" isLoading={mutations.traverseAlbum.isPending} isDisabled={!album?.accountId} onClick={() => traverse(true)} /></div>
      {notice ? <p role="status">{notice}</p> : null}
    </section>
  </>
}

export function JobsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [notice, setNotice] = useState<string>()
  const query = useJobPage({ page: pageIndex + 1, pageSize })
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
  const control = (action: 'pause' | 'resume' | 'retry' | 'cancel') => {
    if (!one) return setNotice(actions.selectOne)
    const confirmations = { pause: actions.confirmPause, resume: undefined, retry: actions.confirmRetry, cancel: actions.confirmCancel } as const
    const confirmation = confirmations[action]
    if (!confirmation || window.confirm(confirmation)) mutations.controlJob.mutate({ id: one, action }, { onSuccess: () => setNotice(undefined), onError: () => setNotice(actions.actionFailed) })
  }
  return (
    <>
      <ResourceTable eyebrow={messages.navigation.operations} messages={messages.resources.jobs} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
      <UnavailableActionPanel messages={messages} title={actions.title} description={actions.description}>
        <Button label={actions.pause} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!one} onClick={() => control('pause')} /><Button label={actions.resume} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!one} onClick={() => control('resume')} /><Button label={actions.retry} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!one} onClick={() => control('retry')} /><Button label={actions.cancel} variant="secondary" isLoading={mutations.controlJob.isPending} isDisabled={!one} onClick={() => control('cancel')} />
        {notice ? <p role="alert">{notice}</p> : null}
      </UnavailableActionPanel>
    </>
  )
}

export function SavedQueriesPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const query = useSavedQueryPage({ page: pageIndex + 1, pageSize })
  const columns = useMemo<ColumnDef<SavedQueryRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.savedQueries.columns.name },
    { accessorKey: 'query', header: messages.resources.savedQueries.columns.query, cell: ({ getValue }) => formatQuery(getValue<Readonly<Record<string, unknown>> | undefined>()) },
    { accessorKey: 'updatedAt', header: messages.resources.savedQueries.columns.updated, cell: ({ getValue }) => formatDate(getValue<string>(), locale) }
  ], [locale, messages])
  const actions = messages.resources.savedQueries.actions
  return (
    <>
      <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.savedQueries} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} />
      <UnavailableActionPanel messages={messages} title={actions.title} description={actions.description} availabilityNote={messages.unavailableActions.apiUnavailable}>
        <Button label={actions.create} variant="secondary" isDisabled /><Button label={actions.edit} variant="secondary" isDisabled /><Button label={actions.remove} variant="secondary" isDisabled />
      </UnavailableActionPanel>
    </>
  )
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

function formatQuery(value: Readonly<Record<string, unknown>> | undefined) {
  if (!value) return '—'
  const entries = Object.entries(value).filter(([, field]) => field !== undefined && field !== '' && field !== null)
  return entries.length === 0 ? '—' : entries.slice(0, 3).map(([key, field]) => `${key}: ${String(field)}`).join(' · ')
}
