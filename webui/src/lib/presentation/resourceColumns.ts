import type { ColumnDef } from '@tanstack/react-table'

export type ResourceColumnRole =
  | 'selection'
  | 'primaryText'
  | 'secondaryText'
  | 'numeric'
  | 'dateTime'
  | 'status'
  | 'actions'
  | 'identifier'
  | 'description'

export type ResourceColumnAlignment = 'start' | 'end' | 'center'
export type ResourceMobilePlacement = 'primary' | 'secondary' | 'metadata' | 'status' | 'actions' | 'hidden'

export interface ResourceColumnPresentation {
  readonly role: ResourceColumnRole
  readonly alignment: ResourceColumnAlignment
  readonly mobilePlacement: ResourceMobilePlacement
  readonly maxLines: 1 | 2 | 3 | undefined
  readonly numeric: boolean
  readonly truncate: boolean
  readonly exposeFullValue: boolean
}

export interface ResourceColumnDefinition<Key extends string = string> {
  readonly key: Key
  readonly label: string
  readonly role: ResourceColumnRole
  readonly mobileLabel?: string
  readonly hideOnMobile?: boolean
}

export interface ResourceMobileField {
  readonly key: string
  readonly label: string
  readonly value: string
  readonly role: ResourceColumnRole
  readonly placement: ResourceMobilePlacement
  readonly fullValue?: string
}

export interface ResourceMobileProjection {
  readonly primary?: ResourceMobileField
  readonly secondary: readonly ResourceMobileField[]
  readonly metadata: readonly ResourceMobileField[]
  readonly status?: ResourceMobileField
  readonly actions?: ResourceMobileField
}

export interface TableColumnMeta {
  readonly role?: ResourceColumnRole
  readonly className?: string
}

const rolePresentation: Readonly<Record<ResourceColumnRole, Omit<ResourceColumnPresentation, 'role'>>> = {
  selection: { alignment: 'center', mobilePlacement: 'hidden', maxLines: 1, numeric: false, truncate: false, exposeFullValue: false },
  primaryText: { alignment: 'start', mobilePlacement: 'primary', maxLines: 2, numeric: false, truncate: true, exposeFullValue: true },
  secondaryText: { alignment: 'start', mobilePlacement: 'secondary', maxLines: 1, numeric: false, truncate: true, exposeFullValue: true },
  numeric: { alignment: 'end', mobilePlacement: 'metadata', maxLines: 1, numeric: true, truncate: false, exposeFullValue: false },
  dateTime: { alignment: 'start', mobilePlacement: 'metadata', maxLines: 1, numeric: true, truncate: false, exposeFullValue: true },
  status: { alignment: 'start', mobilePlacement: 'status', maxLines: 1, numeric: false, truncate: false, exposeFullValue: true },
  actions: { alignment: 'end', mobilePlacement: 'actions', maxLines: 1, numeric: false, truncate: false, exposeFullValue: false },
  identifier: { alignment: 'start', mobilePlacement: 'metadata', maxLines: 1, numeric: false, truncate: true, exposeFullValue: true },
  description: { alignment: 'start', mobilePlacement: 'secondary', maxLines: 3, numeric: false, truncate: true, exposeFullValue: true }
}

export function getResourceColumnPresentation(role: ResourceColumnRole): ResourceColumnPresentation {
  return { role, ...rolePresentation[role] }
}

export function getTableColumnLabel<T>(column: Pick<ColumnDef<T>, 'id' | 'header'>): string {
  return typeof column.header === 'string' ? column.header : column.id ?? '—'
}

export function getTableColumnClassName<T>(column: Pick<ColumnDef<T>, 'meta'>): string {
  const meta = column.meta as TableColumnMeta | undefined
  const presentation = getResourceColumnPresentation(meta?.role ?? 'secondaryText')
  return [
    meta?.className,
    'resource-column',
    `resource-column-${presentation.role}`,
    `resource-column-${presentation.alignment}`,
    presentation.truncate ? 'resource-column-truncate' : undefined
  ].filter(Boolean).join(' ')
}

export function projectResourceToMobile<Key extends string>(
  columns: readonly ResourceColumnDefinition<Key>[],
  values: Readonly<Partial<Record<Key, string | null | undefined>>>
): ResourceMobileProjection {
  const fields = columns.flatMap((column): readonly ResourceMobileField[] => {
    if (column.hideOnMobile) return []
    const value = values[column.key]
    if (value === null || value === undefined || value === '') return []
    const presentation = getResourceColumnPresentation(column.role)
    return [{
      key: column.key,
      label: column.mobileLabel ?? column.label,
      value,
      role: column.role,
      placement: presentation.mobilePlacement,
      fullValue: presentation.exposeFullValue ? value : undefined
    }]
  })

  return {
    primary: fields.find((field) => field.placement === 'primary'),
    secondary: fields.filter((field) => field.placement === 'secondary'),
    metadata: fields.filter((field) => field.placement === 'metadata'),
    status: fields.find((field) => field.placement === 'status'),
    actions: fields.find((field) => field.placement === 'actions')
  }
}
