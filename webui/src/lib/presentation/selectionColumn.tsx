import { CheckboxInput, type CheckboxValue } from '@/components/controls/CheckboxInput'
import type { ColumnDef, Row } from '@tanstack/react-table'

export interface CreateSelectionColumnOptions<T> {
  readonly selectAllLabel: string
  readonly selectRowLabel: (row: Row<T>) => string
}

function tableSelectionValue(isSomeSelected: boolean, isAllSelected: boolean): CheckboxValue {
  if (isAllSelected) return true
  if (isSomeSelected) return 'indeterminate'
  return false
}

export function createSelectionColumn<T>({ selectAllLabel, selectRowLabel }: CreateSelectionColumnOptions<T>): ColumnDef<T> {
  return {
    id: 'select',
    enableHiding: false,
    enableSorting: false,
    meta: { role: 'selection' },
    header: ({ table }) => (
      <CheckboxInput
        label={selectAllLabel}
        isLabelHidden
        size="sm"
        value={tableSelectionValue(table.getIsSomePageRowsSelected(), table.getIsAllPageRowsSelected())}
        onChange={() => table.toggleAllPageRowsSelected()}
      />
    ),
    cell: ({ row }) => (
      <CheckboxInput
        label={selectRowLabel(row)}
        isLabelHidden
        size="sm"
        value={row.getIsSelected()}
        onChange={() => row.toggleSelected()}
      />
    )
  }
}
