import { Button } from '@astryxdesign/core/Button'
import { Collapsible } from '@astryxdesign/core/Collapsible'
import { Dialog, DialogHeader } from '@astryxdesign/core/Dialog'
import { FileInput } from '@astryxdesign/core/FileInput'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { Layout, LayoutContent, LayoutFooter } from '@astryxdesign/core/Layout'
import { Link } from '@astryxdesign/core/Link'
import { TextInput } from '@astryxdesign/core/TextInput'
import type { AccountRecord } from '../../../lib/api'
import type { MessageCatalog } from '../../../i18n'

export type AccountEntryMode = 'create' | 'edit'

export interface AccountDraft {
  readonly fakeid: string
  readonly name: string
  readonly alias: string
}

interface AccountEntryDrawerProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly mode: AccountEntryMode
  readonly actions: MessageCatalog['resources']['accounts']['actions']
  readonly draft: AccountDraft
  readonly onDraftChange: (draft: AccountDraft) => void
  readonly onSubmit: () => void
  readonly isSubmitting: boolean
  readonly search: string
  readonly onSearchChange: (search: string) => void
  readonly onDiscover: () => void
  readonly isDiscovering: boolean
  readonly candidates?: readonly AccountRecord[]
  readonly onCandidateSelect: (account: AccountRecord) => void
  readonly manifest: File | null
  readonly onManifestChange: (manifest: File | null) => void
  readonly onManifestImport: (manifest: File) => Promise<void>
  readonly isManifestImporting: boolean
  readonly manifestDownloadURL: string
}

export function AccountEntryDrawer({
  isOpen,
  onOpenChange,
  mode,
  actions,
  draft,
  onDraftChange,
  onSubmit,
  isSubmitting,
  search,
  onSearchChange,
  onDiscover,
  isDiscovering,
  candidates,
  onCandidateSelect,
  manifest,
  onManifestChange,
  onManifestImport,
  isManifestImporting,
  manifestDownloadURL
}: AccountEntryDrawerProps) {
  const canSubmit = Boolean(draft.fakeid.trim() && draft.name.trim())
  const submitLabel = mode === 'edit' ? actions.edit : actions.add
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (canSubmit) onSubmit()
  }
  const importManifest = async (selected: File | File[] | null) => {
    if (!(selected instanceof File)) return
    try {
      await onManifestImport(selected)
    } finally {
      onManifestChange(null)
    }
  }

  return (
    <Dialog isOpen={isOpen} onOpenChange={onOpenChange} width="min(36rem, 100vw)" maxHeight="100dvh" position={{ top: 0, right: 0 }} purpose="form">
      <Layout
        height="fill"
        header={<DialogHeader title={actions.title} subtitle={actions.description} onOpenChange={onOpenChange} hasDivider />}
        content={
          <LayoutContent label={actions.title} padding={4}>
            <form id="account-entry-form" onSubmit={submit}>
              <FormLayout>
                <TextInput label={actions.name} value={draft.name} onChange={(name) => onDraftChange({ ...draft, name })} isRequired hasAutoFocus />
                <TextInput label={actions.alias} value={draft.alias} onChange={(alias) => onDraftChange({ ...draft, alias })} isOptional />
              </FormLayout>
            </form>

            <section aria-labelledby="account-discovery-title">
              <h3 id="account-discovery-title">{actions.discover}</h3>
              <form onSubmit={(event) => { event.preventDefault(); onDiscover() }}>
                <FormLayout direction="horizontal">
                  <TextInput label={actions.search} value={search} onChange={onSearchChange} isRequired />
                  <Button label={actions.discover} type="submit" variant="secondary" isLoading={isDiscovering} isDisabled={!search.trim()} />
                </FormLayout>
              </form>
              {candidates ? <section aria-labelledby="discovery-results-title" aria-live="polite"><h4 id="discovery-results-title">{actions.discoveryResults}</h4>{candidates.length === 0 ? <p>{actions.discoveryEmpty}</p> : <ul>{candidates.map((account) => <li key={account.id}><div><strong>{account.name}</strong>{account.alias ? <span> · {account.alias}</span> : null}</div><Button label={actions.useCandidate} variant="secondary" size="sm" onClick={() => onCandidateSelect(account)} /></li>)}</ul>}</section> : null}
            </section>

            <Collapsible trigger={actions.technicalDetails} defaultIsOpen={false}>
              <FormLayout>
                <TextInput label={actions.fakeid} description={actions.fakeidHint} value={draft.fakeid} onChange={(fakeid) => onDraftChange({ ...draft, fakeid })} isRequired />
              </FormLayout>
            </Collapsible>

            <section aria-labelledby="account-manifest-title">
              <h3 id="account-manifest-title">{actions.importManifest}</h3>
              <FormLayout>
                <Link href={manifestDownloadURL} isStandalone>{actions.downloadManifest}</Link>
                <FileInput label={actions.importManifest} value={manifest} onChange={(next) => onManifestChange(next instanceof File ? next : null)} changeAction={importManifest} accept="application/json,.json" description={actions.manifestHint} isDisabled={isManifestImporting} isLoading={isManifestImporting} mode="input" />
              </FormLayout>
            </section>
          </LayoutContent>
        }
        footer={<LayoutFooter hasDivider label={actions.title}><div className="presentation-actions"><Button label={submitLabel} type="submit" form="account-entry-form" variant="primary" isLoading={isSubmitting} isDisabled={!canSubmit} /></div></LayoutFooter>}
      />
    </Dialog>
  )
}
