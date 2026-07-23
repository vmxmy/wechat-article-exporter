import { StatusDot } from '@astryxdesign/core/StatusDot'
import { useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import type { AccountRecord, AlbumRecord, JobRecord, SavedQueryRecord } from '../../lib/api'
import { useAccountPage, useAlbumPage, useJobPage, useSavedQueryPage } from '../../lib/queries'
import { ResourceTable } from './ResourceTable'

const pageSize = 25

export function AccountsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const query = useAccountPage({ page: pageIndex + 1, pageSize })
  const columns = useMemo<ColumnDef<AccountRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.accounts.columns.name },
    { accessorKey: 'alias', header: messages.resources.accounts.columns.alias, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' },
    { accessorKey: 'articleCount', header: messages.resources.accounts.columns.articles, cell: ({ getValue }) => getValue<number | undefined>() ?? 0 },
    { accessorKey: 'lastSyncAt', header: messages.resources.accounts.columns.synced, cell: ({ getValue }) => formatDate(getValue<string | undefined>(), locale) },
    { accessorKey: 'syncCompleted', header: messages.resources.accounts.columns.state, cell: ({ getValue }) => <State value={getValue<boolean | undefined>() ? 'ready' : 'queued'} locale={locale} /> }
  ], [locale, messages])
  return <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.accounts} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} />
}

export function AlbumsPage({ messages }: { readonly messages: MessageCatalog }) {
  const [pageIndex, setPageIndex] = useState(0)
  const query = useAlbumPage({ page: pageIndex + 1, pageSize })
  const columns = useMemo<ColumnDef<AlbumRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.albums.columns.name },
    { accessorKey: 'articleCount', header: messages.resources.albums.columns.articles },
    { accessorKey: 'paid', header: messages.resources.albums.columns.paid, cell: ({ getValue }) => getValue<boolean | undefined>() ? '✓' : '—' },
    { accessorKey: 'description', header: messages.resources.albums.columns.description, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' }
  ], [messages])
  return <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.albums} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} />
}

export function JobsPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const query = useJobPage({ page: pageIndex + 1, pageSize })
  const columns = useMemo<ColumnDef<JobRecord>[]>(() => [
    { accessorKey: 'kind', header: messages.resources.jobs.columns.kind },
    { accessorKey: 'state', header: messages.resources.jobs.columns.state, cell: ({ getValue }) => <State value={getValue<string>()} locale={locale} /> },
    { accessorKey: 'createdAt', header: messages.resources.jobs.columns.created, cell: ({ getValue }) => formatDate(getValue<string>(), locale) },
    { accessorKey: 'updatedAt', header: messages.resources.jobs.columns.updated, cell: ({ getValue }) => formatDate(getValue<string>(), locale) },
    { accessorKey: 'counts', header: messages.resources.jobs.columns.counts, cell: ({ getValue }) => formatCounts(getValue<Readonly<Record<string, number>> | undefined>()) }
  ], [locale, messages])
  return <ResourceTable eyebrow={messages.navigation.operations} messages={messages.resources.jobs} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} />
}

export function SavedQueriesPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const query = useSavedQueryPage({ page: pageIndex + 1, pageSize })
  const columns = useMemo<ColumnDef<SavedQueryRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.savedQueries.columns.name },
    { accessorKey: 'query', header: messages.resources.savedQueries.columns.query, cell: ({ getValue }) => formatQuery(getValue<Readonly<Record<string, unknown>> | undefined>()) },
    { accessorKey: 'updatedAt', header: messages.resources.savedQueries.columns.updated, cell: ({ getValue }) => formatDate(getValue<string>(), locale) }
  ], [locale, messages])
  return <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.savedQueries} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} />
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
