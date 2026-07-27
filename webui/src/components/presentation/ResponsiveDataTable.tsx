import { MultiSelector } from '@/components/controls/MultiSelector'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type Header, type Table, type VisibilityState } from '@tanstack/react-table'
import { useMemo, useState, type ReactNode } from 'react'
import type { SelectorCopy } from '@/components/controls/selector-parts'
import { getNextVisibleColumnIDs, getTableColumnClassName } from '@/lib/presentation'

interface ResponsiveDataTableProps<T> {
  readonly table: Table<T>
  readonly ariaLabel: string
  readonly visibleColumnsLabel: string
  readonly selectorCopy: Pick<SelectorCopy, 'clear' | 'search' | 'noResults' | 'selectAll' | 'selected'>
  readonly emptyContent: ReactNode
  readonly renderMobileRows: (rows: ReturnType<Table<T>['getRowModel']>['rows']) => ReactNode
  readonly footer?: ReactNode
  readonly isBusy?: boolean
  readonly toolbarContent?: ReactNode
  readonly renderHeader?: (header: Header<T, unknown>) => ReactNode
  readonly getHeaderAriaSort?: (header: Header<T, unknown>) => 'ascending' | 'descending' | 'none' | undefined
  readonly className?: string
}

export function ResponsiveDataTable<T>({ table, ariaLabel, visibleColumnsLabel, selectorCopy, emptyContent, renderMobileRows, footer, isBusy = false, toolbarContent, renderHeader, getHeaderAriaSort, className }: ResponsiveDataTableProps<T>) {
  const hideableColumns = table.getAllLeafColumns().filter((column) => column.getCanHide())
  const hideableColumnIDs = hideableColumns.map((column) => column.id)
  const visibleColumnIDs = hideableColumns.filter((column) => column.getIsVisible()).map((column) => column.id)
  const rows = table.getRowModel().rows

  return <section className={`presentation-data-table-surface${className ? ` ${className}` : ''}`} aria-label={ariaLabel} aria-busy={isBusy || undefined}>
    <div className="presentation-data-table-toolbar">
      {toolbarContent}
      <MultiSelector
        label={visibleColumnsLabel}
        options={hideableColumns.map((column) => ({ value: column.id, label: typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id }))}
        value={visibleColumnIDs}
        onChange={(requestedVisibleColumnIDs) => {
          const nextVisible = new Set(getNextVisibleColumnIDs(hideableColumnIDs, requestedVisibleColumnIDs))
          hideableColumns.forEach((column) => column.toggleVisibility(nextVisible.has(column.id)))
        }}
        copy={selectorCopy}
        triggerDisplay="count"
        hasSelectAll
        isLabelHidden
      />
    </div>
    <div className="presentation-data-table-desktop data-table-wrap">
      <table className="data-table">
        <thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id} scope="col" className={getTableColumnClassName(header.column.columnDef)} aria-sort={getHeaderAriaSort?.(header)}>{header.isPlaceholder ? null : renderHeader ? renderHeader(header) : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead>
        <tbody>
          {rows.map((row) => <tr key={row.id} data-selected={row.getIsSelected() || undefined}>{row.getVisibleCells().map((cell) => <td key={cell.id} className={getTableColumnClassName(cell.column.columnDef)}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}
          {rows.length === 0 ? <tr><td colSpan={table.getVisibleLeafColumns().length}>{emptyContent}</td></tr> : null}
        </tbody>
      </table>
    </div>
    <div className="presentation-data-table-mobile" aria-label={ariaLabel}>{rows.length === 0 ? emptyContent : renderMobileRows(rows)}</div>
    {footer ? <div className="presentation-data-table-footer">{footer}</div> : null}
  </section>
}

interface StaticResponsiveDataTableProps<T> extends Omit<ResponsiveDataTableProps<T>, 'table'> {
  readonly data: readonly T[]
  readonly columns: readonly ColumnDef<T>[]
}

export function StaticResponsiveDataTable<T>({ data, columns, ...props }: StaticResponsiveDataTableProps<T>) {
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const tableData = useMemo(() => [...data], [data])
  // TanStack Table deliberately exposes a mutable instance for immediate rendering.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: tableData,
    columns: [...columns],
    getCoreRowModel: getCoreRowModel(),
    state: { columnVisibility },
    onColumnVisibilityChange: setColumnVisibility
  })
  return <ResponsiveDataTable table={table} {...props} />
}
