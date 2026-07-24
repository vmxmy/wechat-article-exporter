import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { Collapsible } from '@astryxdesign/core/Collapsible'
import { MultiSelector } from '@astryxdesign/core/MultiSelector'
import { Selector } from '@astryxdesign/core/Selector'
import { Timestamp } from '@astryxdesign/core/Timestamp'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type RowSelectionState, type SortingState, type Updater, type VisibilityState } from '@tanstack/react-table'
import { useEffect, useMemo, useState } from 'react'
import { navigateTo } from '../../app/navigation'
import { ActiveFilterSummary, DetailPanel, EmptyState, MobileResourceRow, SelectionActionBar, Status, TechnicalDetails } from '../../components/presentation'
import type { Locale, MessageCatalog } from '../../i18n'
import { getArticlePreview, parseArticleQuery, saveArticleQueryHandoff, saveExportHandoff, type ArticleQuery, type ArticleRecord, type ArticleSort } from '../../lib/api'
import { formatCount, formatDate, formatDateTime, getResourceColumnPresentation } from '../../lib/presentation'
import { useAccountSelectorPage, useAlbumSelectorPage, useArticleCommentReplies, useArticleComments, useArticleDetail, useArticlePage, useArticleResourceSummary, useSavedQueryPage, useWorkspaceMutations } from '../../lib/queries'
import { ArticleFilterEditor } from './ArticleFilterEditor'
import { getArticleQuerySummaryParts, hasArticleQueryFilters } from './articleQueryPresentation'
import './ArticleTable.css'

interface ArticleTableProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
}

const pageSize = 25
const maximumSelectedArticleIDs = 250

export function ArticleTable({ locale, messages }: ArticleTableProps) {
  const [pageIndex, setPageIndex] = useState(0)
  const [filters, setFilters] = useState<ArticleQuery>({})
  const [query, setQuery] = useState<ArticleQuery>({})
  const [sorting, setSorting] = useState<SortingState>([{ id: 'publishedAt', desc: true }])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const [articlePresentationByID, setArticlePresentationByID] = useState<ReadonlyMap<string, ArticleRecord>>(new Map())
  const [detailArticleID, setDetailArticleID] = useState<string>()
  const [notice, setNotice] = useState<string>()
  const [selectedSavedQueryName, setSelectedSavedQueryName] = useState<string>()
  const [selectedSelectorNames, setSelectedSelectorNames] = useState<{ readonly accounts: ReadonlyMap<string, string>; readonly albums: ReadonlyMap<string, string> }>({ accounts: new Map(), albums: new Map() })
  const sort = sorting[0] ?? { id: 'publishedAt', desc: true }
  const activeSort: ArticleSort = { field: sort.id, direction: sort.desc ? 'desc' : 'asc' }
  const articlePage = useArticlePage({ page: pageIndex + 1, pageSize, ...query, sorts: [activeSort] })
  const accountSelectors = useAccountSelectorPage({ page: 1, pageSize: 100 })
  const albumSelectors = useAlbumSelectorPage({ page: 1, pageSize: 100 })
  const mutations = useWorkspaceMutations()
  const copy = messages.articles.ux
  const articleQueryNames = useMemo(() => ({
    accounts: new Map([...selectedSelectorNames.accounts, ...(accountSelectors.data?.data ?? []).flatMap((account) => account.displayName?.trim() ? [[account.id, account.displayName.trim()] as const] : [])]),
    albums: new Map([...selectedSelectorNames.albums, ...(albumSelectors.data?.data ?? []).flatMap((album) => album.displayName?.trim() ? [[album.id, album.displayName.trim()] as const] : [])])
  }), [accountSelectors.data?.data, albumSelectors.data?.data, selectedSelectorNames])

  useEffect(() => {
    if (!articlePage.data?.data.length) return
    setArticlePresentationByID((current) => {
      const next = new Map(current)
      for (const article of articlePage.data.data) next.set(article.id, article)
      return next
    })
  }, [articlePage.data?.data])

  const applyFilters = () => {
    try {
      const next = parseArticleQuery(filters)
      setPageIndex(0)
      setRowSelection({})
      setQuery(next)
      setNotice(undefined)
    } catch {
      setNotice(messages.articles.filters.invalid)
    }
  }
  const clearFilters = () => {
    setFilters({})
    setQuery({})
    setPageIndex(0)
    setRowSelection({})
    setSelectedSavedQueryName(undefined)
    setNotice(undefined)
  }
  const removeFilter = (field: Exclude<keyof ArticleQuery, 'sorts'>) => {
    const nextFilters = { ...filters }
    const nextQuery = { ...query }
    delete nextFilters[field]
    delete nextQuery[field]
    setFilters(nextFilters)
    setQuery(nextQuery)
    setPageIndex(0)
    setRowSelection({})
    setSelectedSavedQueryName(undefined)
  }
  const updateRowSelection = (updater: Updater<RowSelectionState>) => {
    setRowSelection((current) => {
      const next = selectedArticleIDs(typeof updater === 'function' ? updater(current) : updater)
      return Object.keys(next).length <= maximumSelectedArticleIDs ? next : current
    })
  }

  const columns = useMemo<ColumnDef<ArticleRecord>[]>(() => [
    {
      id: 'select',
      enableSorting: false,
      header: ({ table }) => <CheckboxInput label={messages.articles.selectAll} isLabelHidden value={table.getIsSomePageRowsSelected() ? 'indeterminate' : table.getIsAllPageRowsSelected()} onChange={() => table.toggleAllPageRowsSelected()} />,
      cell: ({ row }) => <CheckboxInput label={messages.articles.selectRow(row.original.title)} isLabelHidden value={row.getIsSelected()} onChange={() => row.toggleSelected()} />
    },
    {
      accessorKey: 'title',
      header: messages.articles.columns.title,
      meta: { role: 'primaryText', className: 'article-title-cell' },
      cell: ({ row, getValue }) => <button className="article-title-button" type="button" title={getValue<string>()} onClick={() => setDetailArticleID(row.original.id)}>{getValue<string>()}</button>
    },
    { accessorKey: 'accountName', header: messages.articles.columns.account, enableSorting: false, meta: { role: 'secondaryText', className: 'article-account-cell' }, cell: ({ row }) => accountNamePresentation(row.original, copy.accountNameUnavailable) },
    { accessorKey: 'publishedAt', header: messages.articles.columns.published, meta: { role: 'dateTime', className: 'article-published-cell' }, cell: ({ getValue }) => formatDate(getValue<string | null>(), locale) },
    { accessorKey: 'status', header: messages.articles.columns.status, meta: { role: 'status', className: 'article-status-cell' }, cell: ({ row }) => <Status value={row.original.state ?? row.original.status} locale={locale} /> }
  ], [copy.accountNameUnavailable, locale, messages])

  // TanStack Table deliberately returns a mutable instance; it is rendered
  // directly in this component rather than handed to a memoized child.
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
      setRowSelection({})
    },
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: updateRowSelection,
    getRowId: (row) => row.id,
    enableRowSelection: true
  })

  const totalPages = articlePage.data ? Math.max(1, Math.ceil(articlePage.data.pagination.total / pageSize)) : 1
  const selectedIDs = Object.entries(rowSelection).flatMap(([id, selected]) => selected ? [id] : [])
  const selectedCount = selectedIDs.length
  const selectedArticle = selectedIDs.length === 1 ? articlePage.data?.data.find((article) => article.id === selectedIDs[0]) : undefined
  const detailArticle = articlePage.data?.data.find((article) => article.id === detailArticleID) ?? selectedArticle
  const activeFilterParts = getArticleQuerySummaryParts(query, locale, messages, articleQueryNames)
  const startDownload = (kind: 'article' | 'metadata' | 'comments' | 'resources', force = false) => {
    if (selectedIDs.length === 0) return
    mutations.downloadArticles.mutate({ articleIds: selectedIDs, kind, force }, {
      onSuccess: () => setNotice(undefined),
      onError: () => setNotice(messages.articles.actions.failed)
    })
  }
  const handoffExport = (selection: 'selected' | 'matching') => {
    const value = selection === 'selected'
      ? selectedIDs.length ? {
          selection: { kind: 'explicit_ids' as const, articleIds: selectedIDs },
          label: messages.exports.workflow.selectedArticlesLabel(selectedIDs.length),
          presentation: {
            articles: selectedIDs.map((id) => articleHandoffPresentation(articlePresentationByID.get(id), messages.exports.workflow.selectedArticles))
          }
        } : undefined
      : {
          selection: { kind: 'all_matching' as const, query: { ...query, sorts: [activeSort] } },
          label: messages.exports.selection.matchingLabel,
          presentation: {
            matching: {
              total: articlePage.data?.pagination.total ?? 0,
              ...(query.accountId ? { accountName: articleQueryNames.accounts.get(query.accountId) } : {}),
              ...(query.albumId ? { albumName: articleQueryNames.albums.get(query.albumId) } : {})
            }
          }
        }
    if (!value) return
    saveExportHandoff(value)
    navigateTo('/exports')
  }
  const saveCurrentQuery = () => {
    try {
      saveArticleQueryHandoff({ ...parseArticleQuery(filters), sorts: [activeSort] })
      navigateTo('/saved-queries')
    } catch {
      setNotice(messages.articles.filters.invalid)
    }
  }
  const preview = (article: ArticleRecord | undefined = selectedArticle) => {
    if (!article) return
    if (article.hasContent === false) return setNotice(messages.articles.actions.previewUnavailable)
    const previewWindow = window.open('', '_blank')
    void getArticlePreview(article.id).then((handoff) => {
      if (!handoff.available || !handoff.documentUrl || !handoff.documentUrl.startsWith('/api/v1/articles/preview/document?')) {
        previewWindow?.close()
        setNotice(messages.articles.actions.previewUnavailable)
        return
      }
      if (!previewWindow) return setNotice(messages.articles.actions.previewBlocked)
      previewWindow.opener = null
      previewWindow.location.replace(handoff.documentUrl)
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
      </header>

      <section className="workspace-panel article-filters" aria-label={messages.articles.filters.title}>
        <ArticleFilterEditor locale={locale} messages={messages} value={filters} onChange={setFilters} selectedAccountLabel={filters.accountId ? articleQueryNames.accounts.get(filters.accountId) : undefined} selectedAlbumLabel={filters.albumId ? articleQueryNames.albums.get(filters.albumId) : undefined} onAccountOptionChange={(option) => {
          const name = option?.displayName?.trim()
          if (!option) return
          if (name) setSelectedSelectorNames((current) => ({ accounts: new Map(current.accounts).set(option.id, name), albums: current.albums }))
        }} onAlbumOptionChange={(option) => {
          const name = option?.displayName?.trim()
          if (!option) return
          if (name) setSelectedSelectorNames((current) => ({ accounts: current.accounts, albums: new Map(current.albums).set(option.id, name) }))
        }} />
        <div className="article-filter-actions">
          <Button label={messages.articles.filters.apply} variant="primary" onClick={applyFilters} />
          <Button label={messages.articles.filters.reset} variant="secondary" onClick={clearFilters} />
          <Button label={copy.saveView} variant="secondary" onClick={saveCurrentQuery} />
        </div>
      </section>

      <ActiveFilterSummary
        label={copy.appliedFilters}
        clearLabel={copy.clearFilters}
        onClear={clearFilters}
        filters={activeFilterParts.map((part) => ({ id: part.id, label: part.label, removeLabel: copy.removeFilter(part.label), onRemove: () => removeFilter(part.id) }))}
      />

      <div className="article-query-toolbar">
        <SavedQuerySelector locale={locale} messages={messages} names={articleQueryNames} value={selectedSavedQueryName} onChange={(name, savedQuery) => {
          setSelectedSavedQueryName(name)
          if (!savedQuery) return
          const next = stripSorting(savedQuery)
          setFilters(next)
          setQuery(next)
          setPageIndex(0)
          setRowSelection({})
        }} />
        <span className="selection-count" aria-live="polite">{selectedCount > 0 ? copy.selectedCount(selectedCount) : ''}</span>
      </div>

      <div className="column-controls article-column-controls" aria-label={messages.articles.visibleColumns}>
        <MultiSelector
          label={messages.articles.visibleColumns}
          isLabelHidden
          options={table.getAllLeafColumns().filter((column) => column.id !== 'select').map((column) => ({ value: column.id, label: typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id }))}
          value={table.getAllLeafColumns().filter((column) => column.id !== 'select' && column.getIsVisible()).map((column) => column.id)}
          onChange={(visible) => table.getAllLeafColumns().filter((column) => column.id !== 'select').forEach((column) => column.toggleVisibility(visible.includes(column.id)))}
          triggerDisplay="count"
          hasSelectAll
        />
      </div>

      {articlePage.isLoading ? <p role="status">{messages.articles.loading}</p> : null}
      {articlePage.isError ? <div className="error-state" role="alert"><p>{messages.articles.unavailable}</p><Button label={messages.articles.retry} variant="secondary" onClick={() => void articlePage.refetch()} /></div> : null}
      {!articlePage.isLoading && !articlePage.isError ? <>
        <ArticleResults
          table={table}
          locale={locale}
          messages={messages}
          onOpenDetail={setDetailArticleID}
          onClearFilters={clearFilters}
          isFirstUse={!hasArticleQueryFilters(query) && (articlePage.data?.pagination.total ?? 0) === 0}
          isFilteredEmpty={hasArticleQueryFilters(query) && (articlePage.data?.pagination.total ?? 0) === 0}
          copy={copy}
        />
        <nav className="pagination" aria-label={messages.articles.pagination}>
          <Button label={messages.articles.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => setPageIndex((current) => current - 1)} />
          <span>{messages.articles.page(pageIndex + 1, totalPages)}</span>
          <Button label={messages.articles.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => setPageIndex((current) => current + 1)} />
        </nav>
      </> : null}

      <SelectionActionBar
        selectedCount={selectedCount}
        countLabel={copy.selectedCount}
        toolbarLabel={copy.selectionActions}
        actions={<>
          <Button label={messages.articles.actions.download} variant="primary" size="sm" onClick={() => startDownload('article')} />
          <Button label={messages.articles.actions.exportSelected} variant="secondary" size="sm" onClick={() => handoffExport('selected')} />
          {selectedArticle ? <Button label={copy.details} variant="secondary" size="sm" onClick={() => setDetailArticleID(selectedArticle.id)} /> : null}
        </>}
        moreActions={<SelectionMoreActions label={copy.moreActions} messages={messages} selectedArticle={selectedArticle} onPreview={() => preview()} onMetadata={() => startDownload('metadata')} onComments={() => startDownload('comments')} onResources={() => startDownload('resources')} onForceResources={() => startDownload('resources', true)} />}
      />

      <section className="article-filter-actions" aria-label={messages.articles.actions.title}>
        <Button label={messages.articles.actions.exportMatching} variant="secondary" onClick={() => handoffExport('matching')} />
        {notice ? <p role="status">{notice}</p> : null}
      </section>
      {detailArticleID ? <ArticleDetailPanel article={detailArticle} locale={locale} messages={messages} isOpen onOpenChange={(isOpen) => { if (!isOpen) setDetailArticleID(undefined) }} onPreview={preview} /> : null}
    </section>
  )
}

function ArticleResults({ table, locale, messages, onOpenDetail, onClearFilters, isFirstUse, isFilteredEmpty, copy }: {
  readonly table: ReturnType<typeof useReactTable<ArticleRecord>>
  readonly locale: Locale
  readonly messages: MessageCatalog
  readonly onOpenDetail: (id: string) => void
  readonly onClearFilters: () => void
  readonly isFirstUse: boolean
  readonly isFilteredEmpty: boolean
  readonly copy: MessageCatalog['articles']['ux']
}) {
  if (isFirstUse) return <EmptyState title={copy.firstUseTitle} description={copy.firstUseDescription} actions={<Button label={copy.firstUseAction} variant="primary" onClick={() => navigateTo('/accounts')} />} />
  if (isFilteredEmpty) return <EmptyState title={copy.filteredEmptyTitle} description={copy.filteredEmptyDescription} actions={<Button label={copy.filteredEmptyAction} variant="secondary" onClick={onClearFilters} />} />
  const rows = table.getRowModel().rows
  return <>
    <div className="data-table-wrap article-data-table" aria-busy={false}>
      <table className="data-table">
        <thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id} scope="col" className={columnClassName(header.column.columnDef)} aria-sort={header.column.getCanSort() ? getSortLabel(header.column.getIsSorted()) : undefined}>{header.isPlaceholder ? null : header.column.getCanSort() ? <button className="sort-button" type="button" onClick={header.column.getToggleSortingHandler()}>{flexRender(header.column.columnDef.header, header.getContext())}</button> : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead>
        <tbody>
          {rows.map((row) => <tr key={row.id} data-selected={row.getIsSelected() || undefined}>{row.getVisibleCells().map((cell) => <td key={cell.id} className={columnClassName(cell.column.columnDef)}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}
          {rows.length === 0 ? <tr><td colSpan={table.getVisibleLeafColumns().length}>{messages.articles.empty}</td></tr> : null}
        </tbody>
      </table>
    </div>
    <div className="article-table-mobile" aria-label={messages.articles.title}>
      {rows.map((row) => <MobileResourceRow
        key={row.id}
        title={<button className="article-title-button" type="button" onClick={() => onOpenDetail(row.original.id)}>{row.original.title}</button>}
        fullTitle={row.original.title}
        description={accountNamePresentation(row.original, copy.accountNameUnavailable)}
        isSelected={row.getIsSelected()}
        selectionLabel={messages.articles.selectRow(row.original.title)}
        onSelectionChange={(selected) => row.toggleSelected(selected)}
        status={<Status value={row.original.state ?? row.original.status} locale={locale} />}
        metadata={[
          { id: 'published', label: messages.articles.columns.published, value: formatDate(row.original.publishedAt, locale), fullValue: row.original.publishedAt ?? undefined },
          { id: 'account', label: messages.articles.columns.account, value: accountNamePresentation(row.original, copy.accountNameUnavailable) }
        ]}
        actions={<Button label={copy.openDetails(row.original.title)} variant="ghost" size="sm" onClick={() => onOpenDetail(row.original.id)} />}
      />)}
    </div>
  </>
}

function SavedQuerySelector({ locale, messages, names, value, onChange }: { readonly locale: Locale; readonly messages: MessageCatalog; readonly names: Parameters<typeof getArticleQuerySummaryParts>[3]; readonly value: string | undefined; readonly onChange: (name: string | undefined, query?: ArticleQuery) => void }) {
  const savedQueries = useSavedQueryPage({ page: 1, pageSize: 25 })
  const copy = messages.articles.ux
  const queryByName = useMemo(() => new Map((savedQueries.data?.data ?? []).map((savedQuery) => [savedQuery.name, savedQuery.query])), [savedQueries.data?.data])
  return <Selector
    label={copy.savedViews}
    options={(savedQueries.data?.data ?? []).map((savedQuery) => ({ value: savedQuery.name, label: savedQuery.name, description: getArticleQuerySummaryParts(savedQuery.query, locale, messages, names).map((part) => part.label).join(' · ') }))}
    value={value ?? null}
    onChange={(next) => onChange(next || undefined, next ? queryByName.get(next) : undefined)}
    placeholder={copy.savedViewsPlaceholder}
    hasClear
    hasSearch
    isLoading={savedQueries.isLoading}
  />
}

function SelectionMoreActions({ label, messages, selectedArticle, onPreview, onMetadata, onComments, onResources, onForceResources }: {
  readonly label: string
  readonly messages: MessageCatalog
  readonly selectedArticle: ArticleRecord | undefined
  readonly onPreview: () => void
  readonly onMetadata: () => void
  readonly onComments: () => void
  readonly onResources: () => void
  readonly onForceResources: () => void
}) {
  if (!selectedArticle) return null
  return <details className="article-selection-more">
    <summary>{label}</summary>
    <div className="article-selection-more-actions">
      <Button label={messages.articles.actions.preview} variant="ghost" size="sm" onClick={onPreview} />
      <Button label={messages.articles.actions.metadata} variant="ghost" size="sm" onClick={onMetadata} />
      <Button label={messages.articles.actions.comments} variant="ghost" size="sm" onClick={onComments} />
      <Button label={messages.articles.actions.resources} variant="ghost" size="sm" onClick={onResources} />
      <Button label={messages.articles.actions.forceResources} variant="ghost" size="sm" onClick={onForceResources} />
    </div>
  </details>
}

function ArticleDetailPanel({ article, locale, messages, isOpen, onOpenChange, onPreview }: { readonly article: ArticleRecord | undefined; readonly locale: Locale; readonly messages: MessageCatalog; readonly isOpen: boolean; readonly onOpenChange: (isOpen: boolean) => void; readonly onPreview: (article: ArticleRecord) => void }) {
  const copy = messages.articles.ux
  const detail = useArticleDetail(article?.id)
  const resourceSummary = useArticleResourceSummary(article?.id)
  return <DetailPanel isOpen={isOpen} onOpenChange={onOpenChange} title={article?.title ?? copy.details} description={article?.accountName?.trim() || undefined}>
    {!article ? null : <div className="article-detail-content">
      <div className="article-detail-actions"><Button label={messages.articles.actions.preview} variant="primary" onClick={() => onPreview(article)} /></div>
      <ResourceSummary summary={resourceSummary} messages={messages} />
      <ArticleDetail detail={detail} articleID={article.id} messages={messages} locale={locale} />
      <TechnicalDetails label={copy.technicalDetails} items={[{ label: 'Article ID', value: article.id, copyLabel: copy.copyArticleID }]} />
    </div>}
  </DetailPanel>
}

function ArticleDetail({ detail, articleID, messages, locale }: { readonly detail: ReturnType<typeof useArticleDetail>; readonly articleID: string; readonly messages: MessageCatalog; readonly locale: Locale }) {
  const copy = messages.articles.ux
  if (detail.isLoading) return <p role="status">{messages.articles.actions.detailLoading}</p>
  if (detail.isError || !detail.data) return <p role="status">{messages.articles.actions.detailUnavailable}</p>
  const { metrics, resources } = detail.data
  return <section className="article-detail-section" aria-label={messages.articles.actions.detailTitle}>
    <h2>{messages.articles.actions.detailTitle}</h2>
    <section className="article-detail-section" aria-label={messages.articles.actions.metricsTitle}>
      <h3>{messages.articles.actions.metricsTitle}</h3>
      {metrics.available ? <>
        <dl className="article-metrics-list">
          <Metric label={copy.metrics.reads} value={metrics.readCount} locale={locale} />
          <Metric label={copy.metrics.oldLikes} value={metrics.oldLikeCount} locale={locale} />
          <Metric label={copy.metrics.likes} value={metrics.likeCount} locale={locale} />
          <Metric label={copy.metrics.shares} value={metrics.shareCount} locale={locale} />
          <Metric label={copy.metrics.comments} value={metrics.commentCount} locale={locale} />
          <Metric label={copy.metrics.captured} value={formatDateTime(metrics.capturedAt, locale)} />
        </dl>
      </> : <p>{messages.articles.actions.metricsUnavailable}</p>}
    </section>
    <section className="article-detail-section" aria-label={messages.articles.actions.resourceDetailsTitle}>
      <h3>{messages.articles.actions.resourceDetailsTitle}</h3>
      {resources.items.length === 0 ? <p>{messages.articles.actions.resourceDetailsEmpty}</p> : <ul>{resources.items.map((resource) => <li key={`${resource.role}-${resource.ordinal}`}>{messages.articles.actions.resourceDetail(resource.role, resource.ordinal, resource.available ? messages.articles.actions.resourceAvailable : messages.articles.actions.resourceMissing)}</li>)}</ul>}
      {resources.total > resources.items.length ? <p>{messages.articles.actions.resourceDetailsLimited(resources.items.length, resources.total)}</p> : null}
    </section>
    <StoredComments articleID={articleID} messages={messages} />
  </section>
}

function Metric({ label, value, locale }: { readonly label: string; readonly value: number | string; readonly locale?: Locale }) {
  return <div><dt>{label}</dt><dd>{typeof value === 'number' ? formatCount(value, locale ?? 'en') : value}</dd></div>
}

function StoredComments({ articleID, messages }: { readonly articleID: string; readonly messages: MessageCatalog }) {
  const [pageState, setPageState] = useState({ articleID, page: 1 })
  const currentPage = pageState.articleID === articleID ? pageState.page : 1
  const comments = useArticleComments(articleID, currentPage, 10)
  const copy = messages.articles.actions
  if (comments.isLoading) return <section aria-label={copy.commentsTitle}><h3>{copy.commentsTitle}</h3><p role="status">{copy.commentsLoading}</p></section>
  if (comments.isError || !comments.data) return <section aria-label={copy.commentsTitle}><h3>{copy.commentsTitle}</h3><p role="status">{copy.commentsUnavailable}</p></section>
  const { comments: commentPage, pendingReplies } = comments.data
  const totalPages = Math.max(1, Math.ceil(commentPage.total / commentPage.limit))
  return <section className="stored-comments" aria-labelledby="stored-comments-title">
    <h3 id="stored-comments-title">{copy.commentsTitle}</h3>
    {pendingReplies > 0 ? <p className="availability-note" role="status">{copy.commentsPartial(pendingReplies)}</p> : null}
    {commentPage.items.length === 0 ? <p>{copy.commentsEmpty}</p> : <div className="comment-list">{commentPage.items.map((comment) => <StoredComment key={comment.id} articleID={articleID} comment={comment} messages={messages} />)}</div>}
    {commentPage.total > commentPage.limit ? <nav className="pagination" aria-label={copy.commentsPagination}><Button label={messages.articles.previous} variant="secondary" size="sm" isDisabled={currentPage === 1} onClick={() => setPageState({ articleID, page: currentPage - 1 })} /><span>{copy.commentsPage(currentPage, totalPages)}</span><Button label={messages.articles.next} variant="secondary" size="sm" isDisabled={currentPage >= totalPages} onClick={() => setPageState({ articleID, page: currentPage + 1 })} /></nav> : null}
  </section>
}

function StoredComment({ articleID, comment, messages }: { readonly articleID: string; readonly comment: import('../../lib/api').ArticleComment; readonly messages: MessageCatalog }) {
  const [open, setOpen] = useState(false)
  const [pageState] = useState({ commentID: comment.id, page: 1 })
  const currentPage = pageState.commentID === comment.id ? pageState.page : 1
  const replies = useArticleCommentReplies(articleID, comment.id, currentPage, 10, open)
  const copy = messages.articles.actions
  return <article className="stored-comment">
    <p className="comment-metadata"><strong>{comment.authorName || copy.unknownAuthor}</strong>{comment.createdAt ? <><span aria-hidden="true"> · </span><Timestamp value={comment.createdAt} format="auto" /></> : null}<span aria-hidden="true"> · </span>{copy.commentStats(comment.likeCount, comment.replyCount)}</p>
    <p>{comment.content}</p>
    {comment.replyCount > 0 ? <Collapsible trigger={copy.expandReplies(comment.replyCount)} isOpen={open} onOpenChange={setOpen}><div className="stored-replies">
      {comment.replyStatus === 'pending' ? <p className="availability-note" role="status">{copy.repliesPending}</p> : null}
      {replies.isLoading ? <p role="status">{copy.repliesLoading}</p> : null}
      {replies.isError ? <p role="status">{copy.repliesUnavailable}</p> : null}
      {replies.data ? <>{replies.data.items.length === 0 ? <p>{copy.repliesEmpty}</p> : replies.data.items.map((reply) => <div key={reply.id} className="stored-reply"><p className="comment-metadata"><strong>{reply.authorName || copy.unknownAuthor}</strong>{reply.createdAt ? <><span aria-hidden="true"> · </span><Timestamp value={reply.createdAt} format="auto" /></> : null}<span aria-hidden="true"> · </span>{copy.replyLikes(reply.likeCount)}</p><p>{reply.content}</p></div>)}</> : null}
    </div></Collapsible> : null}
  </article>
}

function ResourceSummary({ summary, messages }: { readonly summary: ReturnType<typeof useArticleResourceSummary>; readonly messages: MessageCatalog }) {
  if (summary.isLoading) return <p role="status">{messages.articles.actions.resourcesLoading}</p>
  if (summary.isError) return <p role="status">{messages.articles.actions.resourcesUnavailable}</p>
  if (!summary.data) return null
  return <section className="article-detail-section" aria-label={messages.articles.actions.resourcesSummaryTitle}><h2>{messages.articles.actions.resourcesSummaryTitle}</h2><p>{messages.articles.actions.resourcesSummary(summary.data.total, summary.data.available, summary.data.missing)}</p>{summary.data.complete ? <p>{messages.articles.actions.resourcesComplete}</p> : null}</section>
}

function selectedArticleIDs(selection: RowSelectionState): RowSelectionState {
  return Object.fromEntries(Object.entries(selection).filter(([, selected]) => selected))
}

function articleHandoffPresentation(article: ArticleRecord | undefined, fallbackTitle: string) {
  const title = article?.title.trim() || fallbackTitle
  const accountName = article?.accountName?.trim()
  return { title, ...(accountName ? { accountName } : {}) }
}

function accountNamePresentation(article: Pick<ArticleRecord, 'accountName' | 'accountNameAvailable'>, unavailable: string): string {
  return article.accountName?.trim() || (!article.accountNameAvailable ? unavailable : '—')
}

function stripSorting(query: ArticleQuery): ArticleQuery {
  const next = { ...query }
  delete next.sorts
  return next
}

function columnClassName(column: ColumnDef<ArticleRecord>) {
  const meta = column.meta as { readonly className?: string; readonly role?: Parameters<typeof getResourceColumnPresentation>[0] } | undefined
  const presentation = getResourceColumnPresentation(meta?.role ?? 'secondaryText')
  return `${meta?.className ?? ''} resource-column resource-column-${presentation.role} resource-column-${presentation.alignment}${presentation.truncate ? ' resource-column-truncate' : ''}`.trim()
}

function getSortLabel(sort: false | 'asc' | 'desc') {
  if (sort === 'asc') return 'ascending'
  if (sort === 'desc') return 'descending'
  return 'none'
}
