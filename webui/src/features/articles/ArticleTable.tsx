import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { TextInput } from '@astryxdesign/core/TextInput'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type SortingState, type VisibilityState } from '@tanstack/react-table'
import { useEffect, useMemo, useState } from 'react'
import type { Locale, MessageCatalog } from '../../i18n'
import { getArticlePreview, parseArticleQuery, saveArticleQueryHandoff, saveExportHandoff, type ArticleQuery, type ArticleRecord, type ArticleSort } from '../../lib/api'
import { useArticleDetail, useArticlePage, useArticleResourceSummary } from '../../lib/queries'
import { useWorkspaceMutations } from '../../lib/queries'
import { navigateTo } from '../../app/navigation'

interface ArticleTableProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
}

const pageSize = 25

export function ArticleTable({ locale, messages }: ArticleTableProps) {
  const [pageIndex, setPageIndex] = useState(0)
  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<ArticleQuery>({})
  const [query, setQuery] = useState<ArticleQuery>({})
  const [sorting, setSorting] = useState<SortingState>([{ id: 'publishedAt', desc: true }])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const [notice, setNotice] = useState<string>()
  const sort = sorting[0] ?? { id: 'publishedAt', desc: true }
  useEffect(() => {
    const timeout = window.setTimeout(() => setQuery((current) => ({ ...current, keyword: search.trim() || undefined })), 250)
    return () => window.clearTimeout(timeout)
  }, [search])
  useEffect(() => setRowSelection({}), [pageIndex, query, sort.id, sort.desc])
  const activeSort: ArticleSort = { field: sort.id, direction: sort.desc ? 'desc' : 'asc' }
  const articlePage = useArticlePage({
    page: pageIndex + 1,
    pageSize,
    ...query,
    sorts: [activeSort]
  })
  const mutations = useWorkspaceMutations()
  const updateFilter = (field: keyof ArticleQuery, value: string) => {
    setFilters((current) => ({ ...current, [field]: value.trim() || undefined }))
  }
  const applyFilters = () => {
    try {
      const next = parseArticleQuery({ ...filters, keyword: search.trim() || undefined })
      setPageIndex(0)
      setQuery(next)
      setNotice(undefined)
    } catch { setNotice(messages.articles.filters.invalid) }
  }
  const resetFilters = () => { setFilters({}); setSearch(''); setPageIndex(0); setQuery({}) }

  const columns = useMemo<ColumnDef<ArticleRecord>[]>(() => [
    {
      id: 'select',
      enableSorting: false,
      header: ({ table }) => (
        <CheckboxInput
          label={messages.articles.selectAll}
          isLabelHidden
          value={table.getIsSomePageRowsSelected() ? 'indeterminate' : table.getIsAllPageRowsSelected()}
          onChange={() => table.toggleAllPageRowsSelected()}
        />
      ),
      cell: ({ row }) => (
        <CheckboxInput
          label={messages.articles.selectRow(row.original.title)}
          isLabelHidden
          value={row.getIsSelected()}
          onChange={() => row.toggleSelected()}
        />
      )
    },
    { accessorKey: 'title', header: messages.articles.columns.title },
    { accessorKey: 'accountId', header: messages.articles.columns.account, cell: ({ getValue }) => getValue<string | undefined>() ?? '—' },
    {
      accessorKey: 'publishedAt',
      header: messages.articles.columns.published,
      cell: ({ getValue }) => formatDate(getValue<string | null>(), locale)
    },
    {
      accessorKey: 'status',
      header: messages.articles.columns.status,
      cell: ({ row }) => <ArticleStatus status={row.original.state ?? row.original.status ?? ''} locale={locale} />
    }
  ], [locale, messages])

  // TanStack Table deliberately returns a mutable instance; it is not passed
  // to a memoized child or wrapped in React memoization in this component.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: articlePage.data ? [...articlePage.data.data] : [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    enableSortingRemoval: false,
    pageCount: articlePage.data ? Math.max(1, Math.ceil(articlePage.data.pagination.total / pageSize)) : -1,
    state: { sorting, columnVisibility, rowSelection },
    onSortingChange: (updater) => {
      setSorting((current) => typeof updater === 'function' ? updater(current) : updater)
      setPageIndex(0)
    },
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    getRowId: (row) => row.id,
    enableRowSelection: true
  })

  const totalPages = articlePage.data ? Math.max(1, Math.ceil(articlePage.data.pagination.total / pageSize)) : 1
  const selectedCount = Object.values(rowSelection).filter(Boolean).length
  const selectedIDs = Object.entries(rowSelection).filter(([, selected]) => selected).map(([id]) => id)
  const selectedArticle = selectedIDs.length === 1 ? articlePage.data?.data.find((article) => article.id === selectedIDs[0]) : undefined
  const resourceSummary = useArticleResourceSummary(selectedArticle?.id)
  const articleDetail = useArticleDetail(selectedArticle?.id)
  const startDownload = (kind: 'article' | 'metadata' | 'comments' | 'resources', force = false) => {
    if (selectedIDs.length === 0) return
    mutations.downloadArticles.mutate({ articleIds: selectedIDs, kind, force }, {
      onSuccess: (job) => setNotice(`${kind}: ${job.id}`),
      onError: () => setNotice(messages.articles.actions.failed)
    })
  }
  const handoffExport = (selection: 'selected' | 'matching') => {
    const value = selection === 'selected'
      ? selectedIDs.length ? { selection: { kind: 'explicit_ids' as const, articleIds: selectedIDs }, label: messages.exports.selection.explicit(selectedIDs.length) } : undefined
      : { selection: { kind: 'all_matching' as const, query: { ...query, sorts: [activeSort] } }, label: messages.exports.selection.matchingLabel }
    if (!value) return
    saveExportHandoff(value)
    navigateTo('/exports')
  }
  const saveCurrentQuery = () => {
    saveArticleQueryHandoff({ ...query, sorts: [activeSort] })
    navigateTo('/saved-queries')
  }
  const preview = () => {
    if (!selectedArticle) return
    if (selectedArticle.hasContent === false) return setNotice(messages.articles.actions.previewUnavailable)
    const previewWindow = window.open('', '_blank')
    void getArticlePreview(selectedArticle.id).then((handoff) => {
      if (!handoff.available || !handoff.documentUrl || !handoff.documentUrl.startsWith('/api/v1/articles/preview/document?')) {
        previewWindow?.close()
        setNotice(messages.articles.actions.previewUnavailable)
        return
      }
      if (!previewWindow) return setNotice(messages.articles.actions.previewBlocked)
      previewWindow.opener = null
      previewWindow.location.replace(handoff.documentUrl)
      setNotice(`${messages.articles.actions.preview}: ${handoff.title}`)
    }).catch(() => { previewWindow?.close(); setNotice(messages.articles.actions.failed) })
  }

  return (
    <section aria-labelledby="articles-title">
      <header className="page-heading">
        <div>
          <p className="eyebrow">{messages.navigation.library}</p>
          <h1 id="articles-title">{messages.articles.title}</h1>
          <p className="lede">{messages.articles.description}</p>
        </div>
        <p className="read-only-badge">{messages.product.local}</p>
      </header>
      <div className="table-toolbar">
        <TextInput
          label={messages.articles.search}
          isLabelHidden
          value={search}
          placeholder={messages.articles.searchPlaceholder}
          hasClear
          onChange={(value) => {
            setSearch(value)
            setPageIndex(0)
          }}
        />
        <span className="selection-count" aria-live="polite">{selectedCount} {messages.articles.selected}</span>
      </div>
      <section className="workspace-panel article-filters" aria-label={messages.articles.filters.advanced}>
        <h2>{messages.articles.filters.title}</h2>
        <p className="field-hint">{messages.articles.filters.advancedHint}</p>
        <div className="article-filter-grid">
          <TextInput label={messages.articles.filters.accountId} value={filters.accountId ?? ''} onChange={(value) => updateFilter('accountId', value)} />
          <TextInput label={messages.articles.filters.albumId} value={filters.albumId ?? ''} onChange={(value) => updateFilter('albumId', value)} />
          <TextInput label={messages.articles.filters.author} value={filters.author ?? ''} onChange={(value) => updateFilter('author', value)} />
          <TextInput label={messages.articles.filters.state} value={filters.state ?? ''} onChange={(value) => updateFilter('state', value)} />
          <TextInput label={messages.articles.filters.messageTypes} value={(filters.messageTypes ?? []).join(', ')} onChange={(value) => setFilters((current) => ({ ...current, messageTypes: parseMessageTypes(value) }))} />
          <TextInput label={messages.articles.filters.publishedFrom} value={filters.publishedFrom ?? ''} onChange={(value) => updateFilter('publishedFrom', value)} />
          <TextInput label={messages.articles.filters.publishedTo} value={filters.publishedTo ?? ''} onChange={(value) => updateFilter('publishedTo', value)} />
          <label>{messages.articles.filters.hasContent}<select value={optionalBoolean(filters.hasContent)} onChange={(event) => setFilters((current) => ({ ...current, hasContent: parseOptionalBoolean(event.target.value) }))}><option value="">{messages.articles.filters.any}</option><option value="true">{messages.articles.filters.yes}</option><option value="false">{messages.articles.filters.no}</option></select></label>
          <label>{messages.articles.filters.hasComments}<select value={optionalBoolean(filters.hasComments)} onChange={(event) => setFilters((current) => ({ ...current, hasComments: parseOptionalBoolean(event.target.value) }))}><option value="">{messages.articles.filters.any}</option><option value="true">{messages.articles.filters.yes}</option><option value="false">{messages.articles.filters.no}</option></select></label>
          {booleanFilters.map(({ field, label }) => <label key={field}>{messages.articles.filters[label]}<select value={optionalBoolean(filters[field])} onChange={(event) => setFilters((current) => ({ ...current, [field]: parseOptionalBoolean(event.target.value) }))}><option value="">{messages.articles.filters.any}</option><option value="true">{messages.articles.filters.yes}</option><option value="false">{messages.articles.filters.no}</option></select></label>)}
          {numberFilters.map(({ field, label }) => <TextInput key={field} label={messages.articles.filters[label]} value={numberText(filters[field])} onChange={(value) => setFilters((current) => ({ ...current, [field]: parseOptionalNumber(value) }))} />)}
        </div>
        <div className="export-actions"><Button label={messages.articles.filters.apply} variant="secondary" onClick={applyFilters} /><Button label={messages.articles.filters.reset} variant="secondary" onClick={resetFilters} /></div>
      </section>
      <div className="column-controls" aria-label={messages.articles.visibleColumns}>
        {table.getAllLeafColumns().filter((column) => column.id !== 'select').map((column) => (
          <CheckboxInput
            key={column.id}
            label={typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id}
            value={column.getIsVisible()}
            onChange={() => column.toggleVisibility()}
          />
        ))}
      </div>
      {articlePage.isLoading ? <p role="status">{messages.articles.loading}</p> : null}
      {articlePage.isError ? (
        <div className="error-state" role="alert">
          <p>{messages.articles.unavailable}</p>
          <Button label={messages.articles.retry} variant="secondary" onClick={() => void articlePage.refetch()} />
        </div>
      ) : null}
      {!articlePage.isLoading && !articlePage.isError ? (
        <div className="data-table-wrap">
          <table className="data-table">
            <thead>
              {table.getHeaderGroups().map((headerGroup) => (
                <tr key={headerGroup.id}>
                  {headerGroup.headers.map((header) => (
                    <th key={header.id} scope="col" aria-sort={header.column.getCanSort() ? getSortLabel(header.column.getIsSorted()) : undefined}>
                      {header.isPlaceholder ? null : header.column.getCanSort() ? (
                        <button
                          className="sort-button"
                          type="button"
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                        </button>
                      ) : flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows.map((row) => (
                <tr key={row.id} data-selected={row.getIsSelected() || undefined}>
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
                  ))}
                </tr>
              ))}
              {table.getRowModel().rows.length === 0 ? (
                <tr><td colSpan={table.getVisibleLeafColumns().length}>{messages.articles.empty}</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      ) : null}
      <nav className="pagination" aria-label={messages.articles.pagination}>
        <Button label={messages.articles.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => setPageIndex((current) => current - 1)} />
        <span>{messages.articles.page(pageIndex + 1, totalPages)}</span>
        <Button label={messages.articles.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => setPageIndex((current) => current + 1)} />
      </nav>
      <section className="unavailable-actions" aria-labelledby="article-actions-title">
        <div><h2 id="article-actions-title">{messages.articles.actions.title}</h2><p>{messages.articles.actions.description}</p></div>
        {selectedArticle ? <ResourceSummary summary={resourceSummary} messages={messages} /> : null}
        {selectedArticle ? <ArticleDetail detail={articleDetail} messages={messages} locale={locale} /> : null}
        <div className="action-button-group">
          <Button label={messages.articles.actions.preview} variant="secondary" isDisabled={!selectedArticle} onClick={preview} />
          <Button label={messages.articles.actions.download} variant="primary" isDisabled={selectedIDs.length === 0} onClick={() => startDownload('article')} />
          <Button label={messages.articles.actions.metadata} variant="secondary" isDisabled={selectedIDs.length === 0} onClick={() => startDownload('metadata')} />
          <Button label={messages.articles.actions.comments} variant="secondary" isDisabled={selectedIDs.length === 0} onClick={() => startDownload('comments')} />
          <Button label={messages.articles.actions.resources} variant="secondary" isDisabled={selectedIDs.length === 0} onClick={() => startDownload('resources')} />
          <Button label={messages.articles.actions.forceResources} variant="secondary" isDisabled={!selectedArticle} onClick={() => startDownload('resources', true)} />
          <Button label={messages.articles.actions.exportSelected} variant="primary" isDisabled={selectedIDs.length === 0} onClick={() => handoffExport('selected')} />
          <Button label={messages.articles.actions.exportMatching} variant="secondary" onClick={() => handoffExport('matching')} />
          <Button label={messages.articles.actions.saveQuery} variant="secondary" onClick={saveCurrentQuery} />
        </div>
        {notice ? <p role="status">{notice}</p> : null}
      </section>
    </section>
  )
}

function ArticleDetail({ detail, messages, locale }: { readonly detail: ReturnType<typeof useArticleDetail>; readonly messages: MessageCatalog; readonly locale: Locale }) {
  if (detail.isLoading) return <p role="status">{messages.articles.actions.detailLoading}</p>
  if (detail.isError || !detail.data) return <p role="status">{messages.articles.actions.detailUnavailable}</p>
  const { metrics, resources } = detail.data
  return (
    <section aria-label={messages.articles.actions.detailTitle}>
      <h3>{messages.articles.actions.detailTitle}</h3>
      <section aria-label={messages.articles.actions.metricsTitle}>
        <h4>{messages.articles.actions.metricsTitle}</h4>
        {metrics.available ? (
          <p>{messages.articles.actions.metricsSummary(metrics.readCount, metrics.oldLikeCount, metrics.likeCount, metrics.shareCount, metrics.commentCount, metrics.capturedAt ? formatDate(metrics.capturedAt, locale) : '—')}</p>
        ) : <p>{messages.articles.actions.metricsUnavailable}</p>}
      </section>
      <section aria-label={messages.articles.actions.resourceDetailsTitle}>
        <h4>{messages.articles.actions.resourceDetailsTitle}</h4>
        {resources.items.length === 0 ? <p>{messages.articles.actions.resourceDetailsEmpty}</p> : (
          <ul>
            {resources.items.map((resource) => <li key={`${resource.role}-${resource.ordinal}`}>{messages.articles.actions.resourceDetail(resource.role, resource.ordinal, resource.available ? messages.articles.actions.resourceAvailable : messages.articles.actions.resourceMissing)}</li>)}
          </ul>
        )}
        {resources.total > resources.items.length ? <p>{messages.articles.actions.resourceDetailsLimited(resources.items.length, resources.total)}</p> : null}
      </section>
    </section>
  )
}

function ResourceSummary({ summary, messages }: { readonly summary: ReturnType<typeof useArticleResourceSummary>; readonly messages: MessageCatalog }) {
  if (summary.isLoading) return <p role="status">{messages.articles.actions.resourcesLoading}</p>
  if (summary.isError) return <p role="status">{messages.articles.actions.resourcesUnavailable}</p>
  if (!summary.data) return null
  return (
    <section aria-label={messages.articles.actions.resourcesSummaryTitle}>
      <h3>{messages.articles.actions.resourcesSummaryTitle}</h3>
      <p>{messages.articles.actions.resourcesSummary(summary.data.total, summary.data.available, summary.data.missing)}</p>
      {summary.data.complete ? <p>{messages.articles.actions.resourcesComplete}</p> : null}
    </section>
  )
}

function optionalBoolean(value: boolean | undefined) { return value === undefined ? '' : String(value) }
function parseOptionalBoolean(value: string) { return value === '' ? undefined : value === 'true' }
function numberText(value: number | undefined) { return value === undefined ? '' : String(value) }
function parseOptionalNumber(value: string) { const parsed = Number(value); return value.trim() && Number.isInteger(parsed) && parsed >= 0 ? parsed : undefined }
function parseMessageTypes(value: string) { return value.trim() ? value.split(',').map((item) => Number(item.trim())).filter((item) => Number.isInteger(item) && item >= 0) : undefined }

const booleanFilters: ReadonlyArray<{ readonly field: 'deleted' | 'original' | 'paid'; readonly label: 'deleted' | 'original' | 'paid' }> = [
  { field: 'deleted', label: 'deleted' }, { field: 'original', label: 'original' }, { field: 'paid', label: 'paid' }
]

const numberFilters: ReadonlyArray<{ readonly field: 'readMin' | 'readMax' | 'oldLikeMin' | 'oldLikeMax' | 'shareMin' | 'shareMax' | 'likeMin' | 'likeMax' | 'commentMin' | 'commentMax' | 'weCoinMin' | 'weCoinMax' | 'mediaSecondsMin' | 'mediaSecondsMax'; readonly label: 'readMin' | 'readMax' | 'oldLikeMin' | 'oldLikeMax' | 'shareMin' | 'shareMax' | 'likeMin' | 'likeMax' | 'commentMin' | 'commentMax' | 'weCoinMin' | 'weCoinMax' | 'mediaSecondsMin' | 'mediaSecondsMax' }> = [
  { field: 'readMin', label: 'readMin' }, { field: 'readMax', label: 'readMax' }, { field: 'oldLikeMin', label: 'oldLikeMin' }, { field: 'oldLikeMax', label: 'oldLikeMax' },
  { field: 'shareMin', label: 'shareMin' }, { field: 'shareMax', label: 'shareMax' }, { field: 'likeMin', label: 'likeMin' }, { field: 'likeMax', label: 'likeMax' },
  { field: 'commentMin', label: 'commentMin' }, { field: 'commentMax', label: 'commentMax' }, { field: 'weCoinMin', label: 'weCoinMin' }, { field: 'weCoinMax', label: 'weCoinMax' },
  { field: 'mediaSecondsMin', label: 'mediaSecondsMin' }, { field: 'mediaSecondsMax', label: 'mediaSecondsMax' }
]

function ArticleStatus({ status, locale }: { readonly status: string; readonly locale: Locale }) {
  const statusInfo = getStatusInfo(status, locale)
  return <span className="article-status"><StatusDot variant={statusInfo.variant} label={statusInfo.label} />{statusInfo.label}</span>
}

function getStatusInfo(status: string, locale: Locale) {
  const labels = locale === 'zh-CN'
    ? { ready: '就绪', failed: '失败', queued: '已排队' }
    : { ready: 'Ready', failed: 'Failed', queued: 'Queued' }
  if (status === 'ready') return { label: labels.ready, variant: 'success' as const }
  if (status === 'failed') return { label: labels.failed, variant: 'error' as const }
  if (status === 'queued') return { label: labels.queued, variant: 'warning' as const }
  return { label: status, variant: 'neutral' as const }
}

function getSortLabel(sort: false | 'asc' | 'desc') {
  if (sort === 'asc') return 'ascending'
  if (sort === 'desc') return 'descending'
  return 'none'
}

function formatDate(value: string | null, locale: Locale) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale).format(date)
}
