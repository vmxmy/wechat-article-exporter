import { Button } from '@/components/controls/Button'
import { FileInput } from '@/components/controls/FileInput'
import { Toolbar } from '@/components/controls/Toolbar'
import { ActionGroup } from '@/components/presentation'
import type { MessageCatalog } from '@/i18n'

export interface AccountTableToolbarProps {
  readonly actions: MessageCatalog['resources']['accounts']['actions']
  readonly toolbarLabel: string
  readonly isManifestImporting: boolean
  readonly onAdd: () => void
  readonly onImport: (manifest: File) => Promise<void>
}

export function AccountTableToolbar({
  actions,
  toolbarLabel,
  isManifestImporting,
  onAdd,
  onImport
}: AccountTableToolbarProps) {
  return (
    <Toolbar className="account-table-toolbar-actions" label={toolbarLabel} stackAt="medium"
      startContent={
        <ActionGroup className="account-table-toolbar-primary" align="start" gap="control">
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
            fieldClassName="w-auto shrink-0"
          />
        </ActionGroup>
      }
    />
  )
}
