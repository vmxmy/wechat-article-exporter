import { Button } from '@astryxdesign/core/Button'
import { Collapsible } from '@astryxdesign/core/Collapsible'
import { Dialog, DialogHeader } from '@astryxdesign/core/Dialog'
import { TextArea } from '@astryxdesign/core/TextArea'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useEffect, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { PageStack } from '../../../components/presentation'
import type { Locale, MessageCatalog } from '../../../i18n'
import { consumeArticleQueryHandoff, parseArticleQuery, type ArticleQuery, type SavedQueryRecord } from '../../../lib/api'
import { formatDateTime } from '../../../lib/presentation'
import { useAccountSelectorPage, useAlbumSelectorPage, useSavedQueryPage, useWorkspaceMutations } from '../../../lib/queries'
import { ResourceTable } from '../ResourceTable'
import { ArticleFilterEditor } from '../../articles/ArticleFilterEditor'
import { describeArticleQuery } from '../../articles/articleQueryPresentation'
import './SavedQueriesPage.css'

const pageSize = 25

export function SavedQueriesPage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const [pageIndex, setPageIndex] = useState(0)
  const [selected, setSelected] = useState<readonly string[]>([])
  const [name, setName] = useState('')
  const [initialQuery] = useState<ArticleQuery>(() => stripSorting(consumeArticleQueryHandoff() ?? {}))
  const [visualQuery, setVisualQuery] = useState<ArticleQuery>(initialQuery)
  const [rawQuery, setRawQuery] = useState(() => JSON.stringify(initialQuery, null, 2))
  const [notice, setNotice] = useState<string>()
  const [queryPendingDeletion, setQueryPendingDeletion] = useState<string>()
  const [deleteConfirmation, setDeleteConfirmation] = useState('')
  const query = useSavedQueryPage({ page: pageIndex + 1, pageSize })
  const accountSelectors = useAccountSelectorPage({ page: 1, pageSize: 100 })
  const albumSelectors = useAlbumSelectorPage({ page: 1, pageSize: 100 })
  const mutations = useWorkspaceMutations()
  const actions = messages.resources.savedQueries.actions
  const copy = messages.articles.ux
  const articleQueryNames = useMemo(() => ({
    accounts: new Map((accountSelectors.data?.data ?? []).flatMap((account) => account.displayName?.trim() ? [[account.id, account.displayName.trim()] as const] : [])),
    albums: new Map((albumSelectors.data?.data ?? []).flatMap((album) => album.displayName?.trim() ? [[album.id, album.displayName.trim()] as const] : []))
  }), [accountSelectors.data?.data, albumSelectors.data?.data])
  const selectedQuery = selected.length === 1 ? query.data?.data.find((item) => item.name === selected[0]) : undefined
  const columns = useMemo<ColumnDef<SavedQueryRecord>[]>(() => [
    { accessorKey: 'name', header: messages.resources.savedQueries.columns.name, meta: { role: 'primaryText' } },
    { accessorKey: 'query', header: messages.resources.savedQueries.columns.query, meta: { role: 'description' }, cell: ({ getValue }) => describeArticleQuery(getValue<ArticleQuery>(), locale, messages, articleQueryNames) },
    { accessorKey: 'updatedAt', header: messages.resources.savedQueries.columns.updated, meta: { role: 'dateTime' }, cell: ({ getValue }) => formatDateTime(getValue<string>(), locale) }
  ], [articleQueryNames, locale, messages])

  useEffect(() => {
    setRawQuery(JSON.stringify(visualQuery, null, 2))
  }, [visualQuery])

  const save = () => {
    const trimmedName = name.trim()
    if (!trimmedName) return setNotice(actions.invalidQuery)
    try {
      const next = parseArticleQuery(visualQuery)
      mutations.saveSavedQuery.mutate({ name: trimmedName, query: next }, {
        onSuccess: () => setNotice(actions.saved(trimmedName)),
        onError: () => setNotice(actions.actionFailed)
      })
    } catch {
      setNotice(actions.invalidQuery)
    }
  }
  const loadSelectedQuery = () => {
    if (!selectedQuery) return setNotice(actions.selectOne)
    const next = stripSorting(selectedQuery.query)
    setName(selectedQuery.name)
    setVisualQuery(next)
    setRawQuery(JSON.stringify(next, null, 2))
    setNotice(actions.editing(selectedQuery.name))
  }
  const applyTechnicalJSON = () => {
    try {
      const next = stripSorting(parseArticleQuery(JSON.parse(rawQuery)))
      setVisualQuery(next)
      setNotice(copy.savedQuery.editingVisual)
    } catch {
      setNotice(copy.savedQuery.invalidTechnical)
    }
  }
  const remove = () => {
    if (!selectedQuery) return setNotice(actions.selectOne)
    setDeleteConfirmation('')
    setQueryPendingDeletion(selectedQuery.name)
  }

  return <PageStack as="div">
    <ResourceTable eyebrow={messages.navigation.library} messages={messages.resources.savedQueries} columns={columns} query={query} pageIndex={pageIndex} onPageChange={setPageIndex} onSelectionChange={setSelected} />
    <section className="saved-query-editor" aria-labelledby="saved-query-editor-title">
      <div><h2 id="saved-query-editor-title">{copy.savedQuery.visualEditor}</h2><p>{copy.savedQuery.visualDescription}</p></div>
      <TextInput label={actions.name} value={name} onChange={setName} isRequired />
      <ArticleFilterEditor locale={locale} messages={messages} value={visualQuery} onChange={setVisualQuery} idPrefix="saved-query" />
      <p className="saved-query-summary"><strong>{copy.savedQuery.savedSummary}</strong><span>{describeArticleQuery(visualQuery, locale, messages, articleQueryNames)}</span></p>
      <div className="action-button-group"><Button label={actions.create} variant="primary" isLoading={mutations.saveSavedQuery.isPending} onClick={save} /><Button label={actions.edit} variant="secondary" isDisabled={!selectedQuery} onClick={loadSelectedQuery} /><Button label={actions.remove} variant="secondary" isLoading={mutations.deleteSavedQuery.isPending} isDisabled={!selectedQuery} onClick={remove} /></div>
      <Collapsible trigger={copy.savedQuery.technicalMode} defaultIsOpen={false}>
        <div className="saved-query-technical">
          <p>{copy.savedQuery.technicalDescription}</p>
          <TextArea label={copy.savedQuery.rawJSON} value={rawQuery} onChange={setRawQuery} rows={10} hasSpellCheck={false} />
          <Button label={copy.savedQuery.applyTechnical} variant="secondary" onClick={applyTechnicalJSON} />
        </div>
      </Collapsible>
      {notice ? <p role="status">{notice}</p> : null}
    </section>
    {queryPendingDeletion ? <TypedConfirmationDialog isOpen onOpenChange={(isOpen) => { if (!isOpen) { setQueryPendingDeletion(undefined); setDeleteConfirmation('') } }} title={actions.deleteTitle} description={actions.deleteConfirm(queryPendingDeletion)} expected={actions.deleteConfirmation(queryPendingDeletion)} inputLabel={actions.deleteConfirmationLabel} inputHint={actions.deleteConfirmationHint} actionLabel={actions.confirmDelete} cancelLabel={actions.cancelDelete} confirmation={deleteConfirmation} onConfirmationChange={setDeleteConfirmation} isActionLoading={mutations.deleteSavedQuery.isPending} onAction={() => mutations.deleteSavedQuery.mutate({ name: queryPendingDeletion, confirmation: deleteConfirmation }, { onSuccess: () => { setSelected([]); setNotice(actions.deleted(queryPendingDeletion)); setQueryPendingDeletion(undefined); setDeleteConfirmation('') }, onError: () => setNotice(actions.actionFailed) })} /> : null}
  </PageStack>
}

function TypedConfirmationDialog({ isOpen, onOpenChange, title, description, expected, inputLabel, inputHint, actionLabel, cancelLabel, confirmation, onConfirmationChange, isActionLoading, onAction }: {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description: string
  readonly expected: string
  readonly inputLabel: string
  readonly inputHint: string
  readonly actionLabel: string
  readonly cancelLabel: string
  readonly confirmation: string
  readonly onConfirmationChange: (value: string) => void
  readonly isActionLoading: boolean
  readonly onAction: () => void
}) {
  return <Dialog isOpen={isOpen} onOpenChange={onOpenChange} purpose="form" role="alertdialog" aria-label={title}>
    <DialogHeader title={title} subtitle={description} onOpenChange={onOpenChange} />
    <form className="typed-confirmation-dialog" onSubmit={(event) => { event.preventDefault(); if (confirmation === expected) onAction() }}>
      <div className="confirmation-proof"><strong>{inputLabel}</strong><code>{expected}</code><p>{inputHint}</p></div>
      <TextInput label={inputLabel} value={confirmation} onChange={onConfirmationChange} isRequired hasAutoFocus />
      <div className="action-button-group"><Button label={actionLabel} variant="destructive" type="submit" isLoading={isActionLoading} isDisabled={confirmation !== expected} /><Button label={cancelLabel} variant="secondary" isDisabled={isActionLoading} onClick={() => onOpenChange(false)} /></div>
    </form>
  </Dialog>
}

function stripSorting(query: ArticleQuery): ArticleQuery {
  const next = { ...query }
  delete next.sorts
  return next
}
