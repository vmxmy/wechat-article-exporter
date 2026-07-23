import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { TextInput } from '@astryxdesign/core/TextInput'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type SortingState, type VisibilityState } from '@tanstack/react-table'
import { useEffect, useMemo, useState } from 'react'
import type { Locale, MessageCatalog } from '../../i18n'
import type { ArticleRecord } from '../../lib/api'
import { useArticlePage } from '../../lib/queries'

interface ArticleTableProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
}

const pageSize = 25

export function ArticleTable({ locale, messages }: ArticleTableProps) {
  const [pageIndex, setPageIndex] = useState(0)
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [sorting, setSorting] = useState<SortingState>([{ id: 'publishedAt', desc: true }])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const sort = sorting[0] ?? { id: 'publishedAt', desc: true }
  useEffect(() => {
    const timeout = window.setTimeout(() => setQuery(search), 250)
    return () => window.clearTimeout(timeout)
  }, [search])
  const articlePage = useArticlePage({
    page: pageIndex + 1,
    pageSize,
    search: query,
    sort: sort.id,
    direction: sort.desc ? 'desc' : 'asc'
  })

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

  return (
    <section aria-labelledby="articles-title">
      <header className="page-heading">
        <div>
          <p className="eyebrow">{messages.navigation.library}</p>
          <h1 id="articles-title">{messages.articles.title}</h1>
          <p className="lede">{messages.articles.description}</p>
        </div>
        <p className="read-only-badge">{messages.product.beta} · {messages.product.readOnly}</p>
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
    </section>
  )
}

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
