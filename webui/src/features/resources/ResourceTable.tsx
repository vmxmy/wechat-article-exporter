import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type Row, type RowSelectionState, type Updater, type VisibilityState } from '@tanstack/react-table'
import { useEffect, useMemo, useRef, useState } from 'react'
import { DenseRegion, MobileResourceRow, PageHeader, SectionStack } from '../../components/presentation'
import type { MessageCatalog } from '../../i18n'
import { getResourceColumnPresentation, type ResourceColumnRole } from '../../lib/presentation'
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
  readonly onSelectionChange?: (ids: readonly string[]) => void
  readonly preserveSelectionAcrossPages?: boolean
  readonly maximumSelectedIDs?: number
  readonly selectionScope?: string
}

export function ResourceTable<T extends { readonly id?: string; readonly name?: string }>({ eyebrow, messages, columns, query, pageIndex, onPageChange, onSelectionChange, preserveSelectionAcrossPages = false, maximumSelectedIDs, selectionScope }: ResourceTableProps<T>) {
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const selectionChangeRef = useRef(onSelectionChange)
  selectionChangeRef.current = onSelectionChange
  const previousPageIndexRef = useRef(pageIndex)
  const previousPreserveSelectionRef = useRef(preserveSelectionAcrossPages)
  const previousSelectionScopeRef = useRef(selectionScope)
  const selectionColumn = useMemo<ColumnDef<T>>(() => ({
    id: 'select',
    enableHiding: false,
    header: ({ table: currentTable }) => <CheckboxInput label={messages.selectAll} isLabelHidden value={currentTable.getIsSomePageRowsSelected() ? 'indeterminate' : currentTable.getIsAllPageRowsSelected()} onChange={() => currentTable.toggleAllPageRowsSelected()} />,
    cell: ({ row }) => <CheckboxInput label={messages.selectRow(resourcePrimaryText(row.original, row, columns))} isLabelHidden value={row.getIsSelected()} onChange={() => row.toggleSelected()} />
  }), [columns, messages])
  useEffect(() => {
    const pageChanged = previousPageIndexRef.current !== pageIndex
    const persistenceChanged = previousPreserveSelectionRef.current !== preserveSelectionAcrossPages
    previousPageIndexRef.current = pageIndex
    previousPreserveSelectionRef.current = preserveSelectionAcrossPages
    if ((!pageChanged && !persistenceChanged) || preserveSelectionAcrossPages) return
    setRowSelection({})
    selectionChangeRef.current?.([])
  }, [pageIndex, preserveSelectionAcrossPages])
  useEffect(() => {
    if (previousSelectionScopeRef.current === selectionScope) return
    previousSelectionScopeRef.current = selectionScope
    setRowSelection({})
    selectionChangeRef.current?.([])
  }, [selectionScope])
  const updateSelection = (updater: Updater<RowSelectionState>) => {
    setRowSelection((current) => {
      const next = selectedIDs(typeof updater === 'function' ? updater(current) : updater)
      if (maximumSelectedIDs !== undefined && Object.keys(next).length > maximumSelectedIDs) return current
      selectionChangeRef.current?.(Object.keys(next))
      return next
    })
  }
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
    onRowSelectionChange: updateSelection,
    getRowId: (row, index) => row.id ?? row.name ?? String(index),
    enableRowSelection: true
  })
  const totalPages = query.data ? Math.max(1, Math.ceil(query.data.pagination.total / query.data.pagination.pageSize)) : 1
  const visibleColumns = useMemo(() => table.getAllLeafColumns().filter((column) => column.id !== 'select'), [table])
  const titleID = `${eyebrow.toLowerCase().replaceAll(' ', '-')}-title`

  return (
    <SectionStack as="section" gap="section" aria-labelledby={titleID}>
      <PageHeader eyebrow={eyebrow} title={messages.title} titleId={titleID} description={messages.description} />
      <DenseRegion>
        <div className="column-controls resource-column-controls" aria-label={messages.visibleColumns}>
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
              <thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id} scope="col" className={resourceColumnClassName(header.column.columnDef)}>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead>
              <tbody>
                {table.getRowModel().rows.map((row) => <tr key={row.id} data-selected={row.getIsSelected() || undefined}>{row.getVisibleCells().map((cell) => <td key={cell.id} className={resourceColumnClassName(cell.column.columnDef)}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}
                {table.getRowModel().rows.length === 0 ? <tr><td colSpan={table.getVisibleLeafColumns().length}>{messages.empty}</td></tr> : null}
              </tbody>
            </table>
          </div>
        ) : null}
        {!query.isLoading && !query.isError ? <div className="resource-table-mobile" aria-label={messages.title}>
          {table.getRowModel().rows.map((row) => <MobileResourceRow
            key={row.id}
            title={resourcePrimaryCell(row, columns)}
            fullTitle={resourcePrimaryText(row.original, row, columns)}
            description={resourceSecondaryCell(row, columns)}
            isSelected={row.getIsSelected()}
            selectionLabel={messages.selectRow(resourcePrimaryText(row.original, row, columns) || row.id)}
            onSelectionChange={(selected) => row.toggleSelected(selected)}
            status={resourceStatusCell(row, columns)}
            metadata={resourceMetadata(row, columns)}
          />)}
        </div> : null}
        <nav className="pagination" aria-label={messages.pagination}>
          <Button label={messages.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => onPageChange(pageIndex - 1)} />
          <span>{messages.page(pageIndex + 1, totalPages)}</span>
          <Button label={messages.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => onPageChange(pageIndex + 1)} />
        </nav>
      </DenseRegion>
    </SectionStack>
  )
}

function selectedIDs(selection: RowSelectionState): RowSelectionState {
  return Object.fromEntries(Object.entries(selection).filter(([, selected]) => selected))
}

function columnLabel<T>(column: { readonly id: string; readonly columnDef: ColumnDef<T> }) {
  return typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id
}

type ResourceColumnMeta = { readonly role?: ResourceColumnRole }

function resourceColumnRole<T>(column: ColumnDef<T>): ResourceColumnRole {
  return (column.meta as ResourceColumnMeta | undefined)?.role ?? 'secondaryText'
}

function resourceColumnClassName<T>(column: ColumnDef<T>) {
  const presentation = getResourceColumnPresentation(resourceColumnRole(column))
  return `resource-column resource-column-${presentation.role} resource-column-${presentation.alignment}${presentation.truncate ? ' resource-column-truncate' : ''}`
}

function resourcePrimaryText<T>(resource: T, row: Row<T>, columns: readonly ColumnDef<T>[]): string {
  const source = columns.find((column) => resourceColumnRole(column) === 'primaryText') ?? columns[0]
  const key = resourcePrimaryKey(source)
  const value = key ? readableResourceValue((resource as Record<string, unknown>)[key]) : undefined
  if (value) return value
  const cell = row.getAllCells().find((candidate) => candidate.column.id === source?.id || candidate.column.id === key)
  return readableResourceValue(cell?.getValue<unknown>()) ?? '—'
}

function resourcePrimaryCell<T>(row: Row<T>, columns: readonly ColumnDef<T>[]) {
  const source = columns.find((column) => resourceColumnRole(column) === 'primaryText') ?? columns[0]
  const key = resourcePrimaryKey(source)
  const cell = row.getAllCells().find((candidate) => candidate.column.id === source?.id || candidate.column.id === key)
  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : resourcePrimaryText(row.original, row, columns)
}

function resourcePrimaryKey<T>(column: ColumnDef<T> | undefined): string | undefined {
  const definition = column as { readonly accessorKey?: string; readonly id?: string } | undefined
  return definition?.accessorKey ?? definition?.id
}

function readableResourceValue(value: unknown): string | undefined {
  if (typeof value === 'string') return value.trim() || undefined
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return undefined
}

function resourceSecondaryCell<T>(row: Row<T>, columns: readonly ColumnDef<T>[]) {
  const source = columns.find((column) => {
    const role = resourceColumnRole(column)
    return role === 'secondaryText' || role === 'description'
  })
  const cell = row.getAllCells().find((candidate) => candidate.column.id === source?.id || candidate.column.id === (source as { accessorKey?: string } | undefined)?.accessorKey)
  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : undefined
}

function resourceStatusCell<T>(row: Row<T>, columns: readonly ColumnDef<T>[]) {
  const source = columns.find((column) => resourceColumnRole(column) === 'status')
  const cell = row.getAllCells().find((candidate) => candidate.column.id === source?.id || candidate.column.id === (source as { accessorKey?: string } | undefined)?.accessorKey)
  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : undefined
}

function resourceMetadata<T>(row: Row<T>, columns: readonly ColumnDef<T>[]) {
  return columns.flatMap((column) => {
    const role = resourceColumnRole(column)
    if (role === 'primaryText' || role === 'secondaryText' || role === 'description' || role === 'status' || role === 'actions') return []
    const cell = row.getAllCells().find((candidate) => candidate.column.id === column.id || candidate.column.id === (column as { accessorKey?: string }).accessorKey)
    if (!cell) return []
    return [{ id: cell.id, label: columnLabel({ id: cell.column.id, columnDef: cell.column.columnDef }), value: flexRender(cell.column.columnDef.cell, cell.getContext()) }]
  })
}
