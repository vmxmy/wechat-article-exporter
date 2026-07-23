import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type VisibilityState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import type { MessageCatalog } from '../../i18n'
import type { PaginatedResponse } from '../../lib/api'

export interface ResourceTableProps<T> {
  readonly eyebrow: string
  readonly messages: MessageCatalog['resources'][keyof MessageCatalog['resources']]
  readonly columns: ColumnDef<T>[]
  readonly query: {
    readonly data?: PaginatedResponse<T>
    readonly isLoading: boolean
    readonly isError: boolean
    readonly isFetching: boolean
    readonly refetch: () => Promise<unknown>
  }
  readonly pageIndex: number
  readonly onPageChange: (pageIndex: number) => void
}

export function ResourceTable<T extends { readonly id?: string; readonly name?: string }>({ eyebrow, messages, columns, query, pageIndex, onPageChange }: ResourceTableProps<T>) {
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const selectionColumn = useMemo<ColumnDef<T>>(() => ({
    id: 'select',
    enableHiding: false,
    header: ({ table: currentTable }) => <CheckboxInput label={messages.selectAll} isLabelHidden value={currentTable.getIsSomePageRowsSelected() ? 'indeterminate' : currentTable.getIsAllPageRowsSelected()} onChange={() => currentTable.toggleAllPageRowsSelected()} />,
    cell: ({ row }) => <CheckboxInput label={messages.selectRow(row.id)} isLabelHidden value={row.getIsSelected()} onChange={() => row.toggleSelected()} />
  }), [messages])
  // TanStack Table deliberately returns a mutable instance; it is rendered
  // directly here rather than being memoized or passed to a memoized child.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: query.data ? [...query.data.data] : [],
    columns: [selectionColumn, ...columns],
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    state: { columnVisibility, rowSelection },
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    getRowId: (row, index) => row.id ?? row.name ?? String(index),
    enableRowSelection: true
  })
  const totalPages = query.data ? Math.max(1, Math.ceil(query.data.pagination.total / query.data.pagination.pageSize)) : 1
  const selectedCount = Object.values(rowSelection).filter(Boolean).length
  const visibleColumns = useMemo(() => table.getAllLeafColumns().filter((column) => column.id !== 'select'), [table])

  return (
    <section aria-labelledby={`${eyebrow.toLowerCase().replaceAll(' ', '-')}-title`}>
      <header className="page-heading">
        <div>
          <p className="eyebrow">{eyebrow}</p>
          <h1 id={`${eyebrow.toLowerCase().replaceAll(' ', '-')}-title`}>{messages.title}</h1>
          <p className="lede">{messages.description}</p>
        </div>
        <p className="read-only-badge"><StatusDot variant="neutral" label={messages.title} />{selectedCount} {messages.selected}</p>
      </header>
      <div className="column-controls" aria-label={messages.visibleColumns}>
        {visibleColumns.map((column) => (
          <CheckboxInput key={column.id} label={columnLabel(column)} value={column.getIsVisible()} onChange={() => column.toggleVisibility()} />
        ))}
      </div>
      {query.isLoading ? <p role="status">{messages.loading}</p> : null}
      {query.isError ? (
        <div className="error-state" role="alert">
          <p>{messages.unavailable}</p>
          <Button label={messages.retry} variant="secondary" onClick={() => void query.refetch()} />
        </div>
      ) : null}
      {!query.isLoading && !query.isError ? (
        <div className="data-table-wrap" aria-busy={query.isFetching}>
          <table className="data-table">
            <thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id} scope="col">{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead>
            <tbody>
              {table.getRowModel().rows.map((row) => <tr key={row.id} data-selected={row.getIsSelected() || undefined}>{row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}
              {table.getRowModel().rows.length === 0 ? <tr><td colSpan={table.getVisibleLeafColumns().length}>{messages.empty}</td></tr> : null}
            </tbody>
          </table>
        </div>
      ) : null}
      <nav className="pagination" aria-label={messages.pagination}>
        <Button label={messages.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => onPageChange(pageIndex - 1)} />
        <span>{messages.page(pageIndex + 1, totalPages)}</span>
        <Button label={messages.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => onPageChange(pageIndex + 1)} />
      </nav>
    </section>
  )
}

function columnLabel<T>(column: { readonly id: string; readonly columnDef: ColumnDef<T> }) {
  return typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id
}
