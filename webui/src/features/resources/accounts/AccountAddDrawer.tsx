import { Button } from '@/components/controls/Button'
import { Collapsible } from '@/components/controls/Collapsible'
import { TextInput } from '@/components/controls/TextInput'
import { ActionGroup, FieldHint, FormDrawer, FormGrid, Panel } from '../../../components/presentation'
import { navigateTo } from '../../../app/navigation'
import type { MessageCatalog } from '../../../i18n'
import type { AccountRecord } from '../../../lib/api'
import type { AccountDraft, AccountEntryMode } from './accountEntryTypes'

export interface AccountAddDrawerProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly mode: AccountEntryMode
  readonly actions: MessageCatalog['resources']['accounts']['actions']
  readonly closeLabel: string
  readonly draft: AccountDraft
  readonly onDraftChange: (draft: AccountDraft) => void
  readonly onSubmit: () => void
  readonly isSubmitting: boolean
  readonly isAuthenticated: boolean
  readonly isSessionResolved: boolean
  readonly search: string
  readonly onSearchChange: (search: string) => void
  readonly onDiscover: () => void
  readonly isDiscovering: boolean
  readonly discoveryError?: string
  readonly candidates?: readonly AccountRecord[]
  readonly onCandidateSelect: (account: AccountRecord) => void
  readonly articleURL: string
  readonly onArticleURLChange: (articleURL: string) => void
  readonly onResolve: () => void
  readonly isResolving: boolean
  readonly resolveError?: string
  readonly resolvedName?: string
}

export function AccountAddDrawer({
  isOpen,
  onOpenChange,
  mode,
  actions,
  closeLabel,
  draft,
  onDraftChange,
  onSubmit,
  isSubmitting,
  isAuthenticated,
  isSessionResolved,
  search,
  onSearchChange,
  onDiscover,
  isDiscovering,
  discoveryError,
  candidates,
  onCandidateSelect,
  articleURL,
  onArticleURLChange,
  onResolve,
  isResolving,
  resolveError,
  resolvedName
}: AccountAddDrawerProps) {
  const candidateSelected = Boolean(draft.fakeid.trim())
  // A discovered candidate OR a manually entered fakeid both satisfy the save contract.
  const canSubmit = Boolean(draft.name.trim()) && Boolean(draft.fakeid.trim())
  const submitLabel = mode === 'edit' ? actions.edit : actions.add
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (canSubmit) onSubmit()
  }
  const goToLogin = () => {
    onOpenChange(false)
    navigateTo('/login')
  }
  const clearCandidate = () => onDraftChange({ ...draft, fakeid: '', name: '', alias: '' })
  const footerSecondary = <>
    <Button label={actions.cancel} variant="secondary" onClick={() => onOpenChange(false)} />
    {mode === 'create' && candidateSelected ? <Button label={actions.clearCandidate} variant="secondary" onClick={clearCandidate} /> : null}
  </>

  return (
    <FormDrawer
      isOpen={isOpen}
      onOpenChange={onOpenChange}
      title={mode === 'edit' ? actions.editTitle : actions.createTitle}
      description={mode === 'edit' ? actions.editDescription : actions.createDescription}
      closeLabel={closeLabel}
      formId="account-add-form"
      submitLabel={submitLabel}
      isSubmitting={isSubmitting}
      canSubmit={canSubmit}
      footerSecondary={footerSecondary}
    >
      <form id="account-add-form" onSubmit={submit}>
        <FormGrid>
          <TextInput label={actions.name} value={draft.name} onChange={(name) => onDraftChange({ ...draft, name })} isRequired hasAutoFocus />
          <TextInput label={actions.alias} value={draft.alias} onChange={(alias) => onDraftChange({ ...draft, alias })} isOptional />
        </FormGrid>
      </form>

      {mode === 'create' ? (
      <section aria-labelledby="account-discovery-title">
        <h3 id="account-discovery-title">{actions.discoveryTitle}</h3>
        <FieldHint>{actions.discoveryDescription}</FieldHint>
        <Collapsible trigger={actions.articleLinkTitle} defaultIsOpen={false}>
          <FieldHint>{actions.articleLinkDescription}</FieldHint>
          <form onSubmit={(event) => { event.preventDefault(); onResolve() }}>
            <ActionGroup align="end" gap="cluster">
              <TextInput label={actions.articleLinkLabel} value={articleURL} onChange={onArticleURLChange} isRequired placeholder={actions.articleLinkPlaceholder} />
              <Button label={actions.articleLinkResolve} type="submit" variant="secondary" isLoading={isResolving} isDisabled={!articleURL.trim()} />
            </ActionGroup>
          </form>
          {resolvedName ? (
            <Panel as="div" tone="accent" padding="none" style={{ marginTop: 'var(--spacing-3)' }}>
              <p>{actions.articleLinkResolved(resolvedName)}</p>
              {!isAuthenticated ? <Button label={actions.discoverySignIn} variant="primary" size="sm" onClick={goToLogin} /> : null}
            </Panel>
          ) : null}
          {resolveError ? (
            <Panel as="div" tone="muted" padding="none" style={{ marginTop: 'var(--spacing-3)' }}>
              <strong>{actions.discoveryFailedTitle}</strong>
              <FieldHint tone="error">{resolveError}</FieldHint>
              {!isAuthenticated ? (
                <ActionGroup align="start" gap="control">
                  <Button label={actions.discoveryReAuth} variant="secondary" size="sm" onClick={goToLogin} />
                </ActionGroup>
              ) : null}
            </Panel>
          ) : null}
        </Collapsible>
        {isAuthenticated ? (
          <>
            <form onSubmit={(event) => { event.preventDefault(); onDiscover() }}>
              <ActionGroup align="end" gap="cluster">
                <TextInput label={actions.search} value={search} onChange={onSearchChange} isRequired placeholder={actions.searchPlaceholder} />
                <Button label={actions.discover} type="submit" variant="secondary" isLoading={isDiscovering} isDisabled={!search.trim()} />
              </ActionGroup>
            </form>
            {discoveryError ? (
              <Panel as="div" tone="muted" padding="none" style={{ marginTop: 'var(--spacing-3)' }}>
                <strong>{actions.discoveryFailedTitle}</strong>
                <FieldHint tone="error">{discoveryError}</FieldHint>
                <ActionGroup align="start" gap="control">
                  <Button label={actions.discoveryRetry} variant="secondary" size="sm" isDisabled={isDiscovering || !search.trim()} onClick={onDiscover} />
                  <Button label={actions.discoveryReAuth} variant="secondary" size="sm" onClick={goToLogin} />
                </ActionGroup>
              </Panel>
            ) : null}
            {candidates ? (
              <section aria-labelledby="discovery-results-title" aria-live="polite">
                <h4 id="discovery-results-title">{actions.discoveryResults}</h4>
                {candidates.length === 0 ? <FieldHint>{actions.discoveryEmpty}</FieldHint> : (
                  <ul className="discovery-candidates">
                    {candidates.map((account) => (
                      <li key={account.id}>
                        <div><strong>{account.name}</strong>{account.alias ? <span> · {account.alias}</span> : null}</div>
                        <Button label={actions.useCandidate} variant="secondary" size="sm" onClick={() => onCandidateSelect(account)} />
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            ) : null}
          </>
        ) : isSessionResolved ? (
          <Panel as="div" tone="accent">
            <p>{actions.discoveryRequiresSession}</p>
            <Button label={actions.discoverySignIn} variant="primary" onClick={goToLogin} />
          </Panel>
        ) : (
          <FieldHint role="status">{actions.discoveryCheckingSession}</FieldHint>
        )}
      </section>
      ) : null}

      {/* Saving requires a fakeid. In edit mode discovery is gone, so a record that
          arrived without one (manifest import) would otherwise show a disabled Save
          with its only remedy collapsed out of sight. */}
      <Collapsible trigger={actions.technicalDetails} defaultIsOpen={mode === 'edit' && !draft.fakeid.trim()}>
        <FieldHint>{actions.fakeidHint}</FieldHint>
        <TextInput label={actions.fakeid} value={draft.fakeid} onChange={(fakeid) => onDraftChange({ ...draft, fakeid })} isRequired />
      </Collapsible>
    </FormDrawer>
  )
}
