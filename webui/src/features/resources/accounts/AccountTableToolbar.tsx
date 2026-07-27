import { Button } from '@/components/controls/Button'
import { FileInput } from '@/components/controls/FileInput'
import { Selector } from '@/components/controls/Selector'
import { ActionGroup } from '@/components/presentation'
import type { MessageCatalog } from '@/i18n'
import type { AccountSyncMode } from '@/lib/api'

export interface AccountTableToolbarProps {
  readonly actions: MessageCatalog['resources']['accounts']['actions']
  readonly toolbarLabel: string
  readonly selectedCount: number
  readonly syncMode: AccountSyncMode
  readonly isSyncing: boolean
  readonly isDeleting: boolean
  readonly isManifestImporting: boolean
  readonly onAdd: () => void
  readonly onImport: (manifest: File) => Promise<void>
  readonly onExport: () => void
  readonly exportLabel: string
  readonly onEdit: () => void
  readonly onDelete: () => void
  readonly onSync: () => void
  readonly onSyncModeChange: (mode: AccountSyncMode) => void
}

export function AccountTableToolbar({
  actions,
  toolbarLabel,
  selectedCount,
  syncMode,
  isSyncing,
  isDeleting,
  isManifestImporting,
  onAdd,
  onImport,
  onExport,
  exportLabel,
  onEdit,
  onDelete,
  onSync,
  onSyncModeChange
}: AccountTableToolbarProps) {
  const canActOnSelection = selectedCount > 0
  const canEdit = selectedCount === 1

  return (
    <ActionGroup className="account-table-toolbar-actions" align="start" gap="control" nowrap role="group" aria-label={toolbarLabel}>
      <Button label={actions.addAccount} variant="primary" onClick={onAdd} />
      <FileInput
        label={actions.importManifest}
        value={null}
        onChange={() => undefined}
        changeAction={async (manifest) => {
          if (manifest instanceof File) await onImport(manifest)
        }}
        accept="application/json,.json"
        isDisabled={isManifestImporting}
        isLoading={isManifestImporting}
        isLabelHidden
        className="account-table-toolbar-import"
      />
      <Button label={exportLabel} variant="secondary" isDisabled={!canActOnSelection} onClick={onExport} />
      <Button label={actions.edit} variant="secondary" isDisabled={!canEdit} onClick={onEdit} />
      <Button label={actions.remove} variant="destructive" isDisabled={!canActOnSelection} isLoading={isDeleting} onClick={onDelete} />
      <Selector
        label={actions.syncMode}
        options={[
          { value: 'incremental', label: actions.incremental },
          { value: 'full', label: actions.full }
        ]}
        value={syncMode}
        onChange={(mode) => {
          if (mode) onSyncModeChange(mode as AccountSyncMode)
        }}
        isDisabled={!canActOnSelection || isSyncing || isDeleting}
        isLabelHidden
        className="account-table-toolbar-sync-mode"
      />
      <Button label={actions.sync} variant="secondary" isDisabled={!canActOnSelection || isDeleting} isLoading={isSyncing} onClick={onSync} />
    </ActionGroup>
  )
}
