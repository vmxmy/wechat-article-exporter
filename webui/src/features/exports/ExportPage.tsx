import { Button } from '@/components/controls/Button'
import { CheckboxInput } from '@/components/controls/CheckboxInput'
import { NumberInput } from '@/components/controls/NumberInput'
import { RadioList, RadioListItem } from '@/components/controls/RadioList'
import { Selector } from '@/components/controls/Selector'
import { SearchableSelector } from '@/components/controls/SearchableSelector'
import { TextInput } from '@/components/controls/TextInput'
import { AccountRemoteSelector, ActionGroup, AlbumRemoteSelector, ArticleRemoteMultiSelector, FieldHint, FormGrid, MobileResourceRow, PageHeader, PageStack, Panel, ResponsiveDataTable, SectionHeader, StaticResponsiveDataTable, Status, TechnicalDetails } from '../../components/presentation'
import { navigationEvent } from '../../app/navigation'
import { createSelectionColumn, formatBytes, formatDateTime, formatShortIdentifier, formatStatus } from '../../lib/presentation'
import { describeArticleQuery } from '../articles/articleQueryPresentation'
import { useEffect, useMemo, useState } from 'react'
import { getCoreRowModel, useReactTable, type ColumnDef, type VisibilityState } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import {
  clearExportHandoffForMount,
  consumeExportHandoffForMount,
  getExportArtifactDownloadURL,
  openExportOutput,
  type AccountOption,
  type AlbumOption,
  type ArticleOption,
  type ExportDirectory,
  type ExportFile,
  type ExportFormat,
  type ExportManifest,
  type ExportOptions,
  type ExportRecord,
  type ExportSelection,
  type ExportVerification,
} from '../../lib/api'
import { handoffCreatedJob } from '../../lib/jobHandoff'
import {
  clearExportBrowserDraft,
  createExportWorkflowID,
  earliestValidExportStage,
  loadExportBrowserDraft,
  parseExportBrowserView,
  saveExportBrowserDraft,
  serializeExportBrowserView,
  type ExportBrowserView,
  type ExportScopeType,
  type ExportStage
} from '../../lib/browserViewState'
import { useExportManifest, useExportPage, useSavedQueryPage, useWorkspaceMutations } from '../../lib/queries'
import './export.css'

const pageSize = 25
const formats: readonly ExportFormat[] = ['markdown', 'html', 'text', 'json', 'xlsx', 'docx', 'pdf']

interface ExportPageProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
}

type Stage = ExportStage
type ScopeMode = ExportScopeType

interface ScopeChoice {
  readonly selection: ExportSelection
  readonly label: string
}

interface ExportVerificationDetail {
  path?: string
  message?: string
  expected?: string
  actual?: string
}

export function ExportPage({ locale, messages }: ExportPageProps) {
  const copy = messages.exports
  const [exportPath] = useState(window.location.pathname)
  // Export handoff is deliberately single-use session state. Capture it once
  // per Strict Mode render pair so query/cache rerenders cannot consume and
  // erase its safe presentation labels after the initial scope is restored.
  const [initialHandoff] = useState(consumeExportHandoffForMount)
  const [initialBrowserView] = useState(() => parseExportBrowserView(window.location.search))
  const [workflow] = useState(() => initialBrowserView.state.workflow ?? createExportWorkflowID())
  const [initialDraft] = useState(() => loadExportBrowserDraft(initialBrowserView.state.workflow))
  const initialSelection = initialHandoff?.selection ?? initialDraft?.selection
  const initialScopeMode = initialHandoff
    ? scopeModeFor(initialHandoff.selection)
    : initialBrowserView.specified.scope || !initialSelection ? initialBrowserView.state.scope : scopeModeFor(initialSelection)
  const compatibleInitialSelection = initialSelection && scopeModeFor(initialSelection) === initialScopeMode ? initialSelection : undefined
  const initialScope = initialHandoff
    ? initialScopeChoice(initialHandoff, copy)
    : compatibleInitialSelection ? scopeChoiceFromDraft(compatibleInitialSelection, initialDraft?.selectionLabel, copy) : undefined
  const initialOptions = initialDraft?.options
  const initialFormatValid = Number.isInteger(initialOptions?.maximumNameBytes ?? 180) && (initialOptions?.maximumNameBytes ?? 180) > 0
  const initialStage = earliestValidExportStage(initialBrowserView.state.stage, Boolean(initialScope), initialFormatValid)
  const [stage, setStage] = useState<Stage>(initialStage)
  const [scopeMode, setScopeMode] = useState<ScopeMode>(initialScopeMode)
  const [scope, setScope] = useState<ScopeChoice | undefined>(initialScope)
  // Stable IDs remain exclusively in the export action contract. The handoff
  // presentation has only human-readable names and titles, so it can restore
  // cross-page context without exposing identifiers in the UI.
  const [selectedArticles, setSelectedArticles] = useState<readonly ArticleOption[]>(() => initialHandoff?.selection.kind === 'explicit_ids'
    ? initialHandoff.selection.articleIds.map((id, index) => articleOptionFromHandoff(id, initialHandoff.presentation?.articles?.[index], copy.workflow.selectedArticles))
    : initialDraft?.selectedArticles ?? [])
  const [albumIDs] = useState<readonly string[]>(() => compatibleInitialSelection?.kind === 'album_ids' ? compatibleInitialSelection.albumIds : [])
  const [accountID, setAccountID] = useState(() => compatibleInitialSelection?.kind === 'account' ? compatibleInitialSelection.accountId : '')
  const [albumID, setAlbumID] = useState(() => compatibleInitialSelection?.kind === 'album' ? compatibleInitialSelection.albumId : '')
  const [savedQueryID, setSavedQueryID] = useState(() => compatibleInitialSelection?.kind === 'saved_query' ? compatibleInitialSelection.savedQueryId : '')
  const [matchingScope] = useState(() => compatibleInitialSelection?.kind === 'all_matching' ? compatibleInitialSelection : undefined)
  const [format, setFormat] = useState<ExportFormat>(initialBrowserView.state.format)
  const [subdirectory, setSubdirectory] = useState('')
  const [namingTemplate, setNamingTemplate] = useState(initialOptions?.namingTemplate ?? '{published}-{title}')
  const [maximumNameBytes, setMaximumNameBytes] = useState<number | null>(initialOptions?.maximumNameBytes ?? 180)
  const [collisionPolicy, setCollisionPolicy] = useState<'fail' | 'skip' | 'replace' | 'suffix'>(initialOptions?.collisionPolicy ?? 'fail')
  const [includeContent, setIncludeContent] = useState(initialOptions?.includeContent ?? true)
  const [includeMetadata, setIncludeMetadata] = useState(initialOptions?.includeMetadata ?? true)
  const [includeComments, setIncludeComments] = useState(initialOptions?.includeComments ?? false)
  const [htmlResourcePolicy, setHTMLResourcePolicy] = useState<'best-effort' | 'strict'>(initialOptions?.htmlResourcePolicy ?? 'best-effort')
  const [htmlBatchArchive, setHTMLBatchArchive] = useState('')
  const [directory, setDirectory] = useState<ExportDirectory>()
  const [childName, setChildName] = useState('')
  const [notice, setNotice] = useState<string>()
  const [outputNotice, setOutputNotice] = useState<string>()
  const [pageIndex, setPageIndex] = useState(0)
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [openConfirmation, setOpenConfirmation] = useState('')

  const mutations = useWorkspaceMutations()
  const savedQueries = useSavedQueryPage({ page: 1, pageSize: 100 })
  const records = useExportPage({ page: pageIndex + 1, pageSize })
  const selectedIDs = Object.entries(rowSelection).filter(([, selected]) => selected).map(([id]) => id)
  const selectedID = selectedIDs.length === 1 ? selectedIDs[0] : undefined
  const manifest = useExportManifest(selectedID)
  const formatOptions = formatOptionsFor(format)
  const isScopeValid = Boolean(scope)
  const isFormatValid = Number.isInteger(maximumNameBytes) && (maximumNameBytes ?? 0) > 0
  const isDestinationValid = Boolean(directory)

  function replaceExportURL(view: ExportBrowserView) {
    const search = serializeExportBrowserView(view)
    window.history.replaceState(window.history.state, '', `${window.location.pathname}${search}${window.location.hash}`)
  }

  function commitExportView(view: ExportBrowserView, mode: 'push' | 'replace' = 'push') {
    const validStage = earliestValidExportStage(view.stage, Boolean(scope), isFormatValid)
    const next = { ...view, ...(workflow ? { workflow } : {}), stage: validStage }
    const search = serializeExportBrowserView(next)
    const href = `${window.location.pathname}${search}${window.location.hash}`
    if (`${window.location.pathname}${window.location.search}${window.location.hash}` !== href) window.history[mode === 'push' ? 'pushState' : 'replaceState'](window.history.state, '', href)
    setStage(next.stage)
    setScopeMode(next.scope)
    setFormat(next.format)
  }

  useEffect(() => {
    const canonical = serializeExportBrowserView({ ...(workflow ? { workflow } : {}), stage: initialStage, scope: initialScopeMode, format: initialBrowserView.state.format })
    if (initialBrowserView.needsReplace || canonical !== window.location.search) window.history.replaceState(window.history.state, '', `${window.location.pathname}${canonical}${window.location.hash}`)
  }, [initialBrowserView, initialScopeMode, initialStage, workflow])

  useEffect(() => {
    const restoreFromLocation = () => {
      if (window.location.pathname !== exportPath) return
      const parsed = parseExportBrowserView(window.location.search)
      const sameWorkflow = parsed.state.workflow === workflow
      const compatibleScope = sameWorkflow && scope && scopeModeFor(scope.selection) === parsed.state.scope ? scope : undefined
      const validStage = earliestValidExportStage(parsed.state.stage, Boolean(compatibleScope), isFormatValid)
      if (!compatibleScope && scope) chooseScope(undefined)
      setScopeMode(parsed.state.scope)
      setFormat(parsed.state.format)
      setStage(validStage)
      const canonical = serializeExportBrowserView({ ...parsed.state, stage: validStage })
      if (parsed.needsReplace || canonical !== window.location.search) replaceExportURL({ ...parsed.state, stage: validStage })
    }
    window.addEventListener('popstate', restoreFromLocation)
    window.addEventListener(navigationEvent, restoreFromLocation)
    return () => {
      window.removeEventListener('popstate', restoreFromLocation)
      window.removeEventListener(navigationEvent, restoreFromLocation)
    }
  }, [exportPath, isFormatValid, scope, workflow])

  useEffect(() => {
    if (!workflow) return
    saveExportBrowserDraft(workflow, {
      ...(scope ? { selection: scope.selection, selectionLabel: scope.label } : {}),
      ...(scope?.selection.kind === 'explicit_ids' ? { selectedArticles } : {}),
      options: { namingTemplate, maximumNameBytes: maximumNameBytes ?? 180, collisionPolicy, includeContent, includeMetadata, includeComments, htmlResourcePolicy }
    })
  }, [collisionPolicy, htmlResourcePolicy, includeComments, includeContent, includeMetadata, maximumNameBytes, namingTemplate, scope, selectedArticles, workflow])

  useEffect(() => {
    setRowSelection((current) => Object.fromEntries(Object.entries(current).filter(([id]) => records.data?.data.some((record) => record.id === id))))
  }, [records.data])

  useEffect(() => {
    setOpenConfirmation('')
  }, [selectedID])

  useEffect(() => {
    // The initial render pair has already captured the single-use handoff.
    // Clear its module cache after mounting and again on unmount so an
    // unrelated later visit to /exports can never inherit stale scope data.
    clearExportHandoffForMount()
    return clearExportHandoffForMount
  }, [])

  const accountNames = useMemo(() => nameMapForMatchingScope(matchingScope?.query.accountId, initialHandoff?.presentation?.matching?.accountName), [initialHandoff?.presentation?.matching?.accountName, matchingScope?.query.accountId])
  const albumNames = useMemo(() => nameMapForMatchingScope(matchingScope?.query.albumId, initialHandoff?.presentation?.matching?.albumName), [initialHandoff?.presentation?.matching?.albumName, matchingScope?.query.albumId])
  const savedQueryOptions = useMemo(() => savedQueries.data?.data.map((savedQuery) => ({ value: savedQuery.name, label: savedQuery.name })) ?? [], [savedQueries.data])
  const columns = useMemo<ColumnDef<ExportRecord>[]>(() => [
    createSelectionColumn({
      selectAllLabel: copy.selectAll,
      selectRowLabel: (row) => copy.selectRow(copy.workflow.recordLabel(row.original.format))
    }),
    { id: 'label', header: copy.recordsTitle, meta: { role: 'primaryText' }, cell: ({ row }) => <ExportRecordLabel record={row.original} locale={locale} copy={copy} /> },
    { accessorKey: 'format', header: copy.columns.format, meta: { role: 'secondaryText' }, cell: ({ getValue }) => getValue<string>().toUpperCase() },
    { accessorKey: 'state', header: copy.columns.state, meta: { role: 'status' }, cell: ({ getValue }) => <Status value={getValue<string>()} locale={locale} /> },
    { accessorKey: 'createdAt', header: copy.columns.created, meta: { role: 'dateTime' }, cell: ({ getValue }) => formatDateTime(getValue<string>(), locale) },
    { accessorKey: 'provenanceState', header: copy.columns.provenance, meta: { role: 'description' }, cell: ({ row }) => formatProvenance(row.original, locale) }
  ], [copy, locale])
  // TanStack table exposes a mutable table instance. It is rendered directly.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({ data: records.data ? [...records.data.data] : [], columns, getCoreRowModel: getCoreRowModel(), state: { rowSelection, columnVisibility }, onRowSelectionChange: setRowSelection, onColumnVisibilityChange: setColumnVisibility, getRowId: (row) => row.id, enableRowSelection: true })
  const totalPages = records.data ? Math.max(1, Math.ceil(records.data.pagination.total / records.data.pagination.pageSize)) : 1

  function chooseScope(next: ScopeChoice | undefined) {
    setScope(next)
    setNotice(undefined)
  }

  function changeScopeMode(next: ScopeMode) {
    if (next === scopeMode) return
    // A scope is an action contract, not merely a visible summary. Do not let
    // a user queue an invisible prior scope after moving to another scope
    // control; choosing the new control must be explicit.
    clearExportBrowserDraft(workflow)
    commitExportView({ stage: 'scope', scope: next, format })
    if (next === 'albums' && albumIDs.length) {
      chooseScope({ selection: { kind: 'album_ids', albumIds: albumIDs }, label: copy.selection.albumsLabel(albumIDs.length) })
      return
    }
    chooseScope(undefined)
  }

  function selectArticles(nextArticles: readonly ArticleOption[]) {
    const uniqueArticles = [...new Map(nextArticles.map((article) => [article.id, article])).values()]
    const ids = uniqueArticles.map((article) => article.id)
    setSelectedArticles(uniqueArticles)
    chooseScope(ids.length ? { selection: { kind: 'explicit_ids', articleIds: ids }, label: selectionLabelForArticles(uniqueArticles, ids, copy) } : undefined)
  }

  function selectAccount(nextID: string, option?: AccountOption) {
    setAccountID(nextID)
    const account = option
    chooseScope(nextID ? { selection: { kind: 'account', accountId: nextID }, label: account ? accountLabel(account, copy) : copy.workflow.savedAccountFallback } : undefined)
  }

  function selectAlbum(nextID: string, option?: AlbumOption) {
    setAlbumID(nextID)
    const album = option
    chooseScope(nextID ? { selection: { kind: 'album', albumId: nextID }, label: album ? albumLabel(album, copy) : copy.workflow.savedAlbumFallback } : undefined)
  }

  function selectSavedQuery(nextID: string) {
    setSavedQueryID(nextID)
    chooseScope(nextID ? { selection: { kind: 'saved_query', savedQueryId: nextID }, label: copy.selection.savedQueryLabel(nextID) } : undefined)
  }

  function selectMatchingScope() {
    if (!matchingScope) return
    chooseScope({ selection: matchingScope, label: matchingScopeLabel(initialHandoff?.presentation?.matching?.total, copy) })
  }

  function advanceStage() {
    if (stage === 'scope') {
      if (!isScopeValid) return setNotice(copy.invalidSelection)
      commitExportView({ stage: 'format', scope: scopeMode, format })
      return
    }
    if (stage === 'format') {
      if (!isFormatValid) return setNotice(copy.actionFailed)
      commitExportView({ stage: 'destination', scope: scopeMode, format })
      return
    }
    queueExport()
  }

  function authorizeDirectory() {
    mutations.authorizeDefaultExportDirectory.mutate(undefined, {
      onSuccess: (result) => { setDirectory(result); setNotice(undefined) },
      onError: () => setNotice(copy.actionFailed)
    })
  }

  function createDirectory() {
    if (!directory || !childName.trim()) return
    mutations.createExportDirectory.mutate({ parentToken: directory.token, name: childName }, {
      onSuccess: (result) => { setDirectory(result); setChildName(''); setNotice(undefined) },
      onError: () => setNotice(copy.actionFailed)
    })
  }

  function queueExport() {
    if (!scope) return setNotice(copy.invalidSelection)
    if (!directory) return setNotice(copy.invalidDirectory)
    if (!isFormatValid) return setNotice(copy.actionFailed)
    const maximum = maximumNameBytes
    if (maximum === null || !Number.isInteger(maximum) || maximum <= 0) return setNotice(copy.actionFailed)
    mutations.startExport.mutate({
      directoryToken: directory.token,
      subdirectory: subdirectory.trim() || undefined,
      selection: scope.selection,
      format,
      options: {
        namingTemplate: namingTemplate.trim() || undefined,
        maximumNameBytes: maximum,
        collisionPolicy,
        formatOptions: exportFormatOptions(format, { includeContent, includeMetadata, includeComments, htmlResourcePolicy, htmlBatchArchive })
      }
    }, {
      onSuccess: (result) => {
        clearExportBrowserDraft(workflow)
        handoffCreatedJob({ id: result.jobId })
      },
      onError: () => setNotice(copy.actionFailed)
    })
  }

  function runVerification() {
    if (!selectedID) return setNotice(copy.selectOne)
    mutations.verifyExport.mutate(selectedID, { onSuccess: () => setNotice(undefined), onError: () => setNotice(copy.actionFailed) })
  }

  function openOutputDirectory() {
    if (!selectedID) return setOutputNotice(copy.selectOne)
    void openExportOutput(selectedID, openConfirmation).then(
      () => { setOpenConfirmation(''); setOutputNotice(copy.outputOpened) },
      () => setOutputNotice(copy.actionFailed)
    )
  }

  const expectedOpenConfirmation = selectedID ? copy.openConfirmation(selectedID) : ''
  const primaryAction = stage === 'scope' ? copy.workflow.continueToFormat : stage === 'format' ? copy.workflow.continueToDestination : copy.start
  const primaryDisabled = stage === 'scope' ? !isScopeValid : stage === 'format' ? !isFormatValid : !isDestinationValid

  return (
    <PageStack aria-labelledby="exports-title">
      <PageHeader eyebrow={messages.navigation.operations} title={copy.title} titleId="exports-title" description={copy.description} />

      <div className="export-stage-shell">
        <ol className="export-stage-nav" aria-label={copy.workflow.stages}>
          <StageNavigationItem label={copy.workflow.scope} number={1} isActive={stage === 'scope'} isComplete={Boolean(scope)} onClick={() => commitExportView({ stage: 'scope', scope: scopeMode, format })} />
          <StageNavigationItem label={copy.workflow.format} number={2} isActive={stage === 'format'} isComplete={stage === 'destination'} isDisabled={!scope} onClick={() => scope && commitExportView({ stage: 'format', scope: scopeMode, format })} />
          <StageNavigationItem label={copy.workflow.destination} number={3} isActive={stage === 'destination'} isComplete={false} isDisabled={!scope || !isFormatValid} onClick={() => scope && isFormatValid && commitExportView({ stage: 'destination', scope: scopeMode, format })} />
        </ol>

        {stage === 'scope' ? (
          <Panel className="export-stage" aria-labelledby="export-scope-title">
            <SectionHeader title={copy.workflow.scope} titleId="export-scope-title" description={copy.workflow.scopeDescription} />
            <RadioList label={copy.workflow.scopeType} value={scopeMode} onChange={(value) => changeScopeMode(value as ScopeMode)} orientation="vertical">
              <RadioListItem value="articles" label={copy.workflow.selectedArticles} description={copy.workflow.selectedArticlesDescription} />
              <RadioListItem value="albums" label={copy.selection.albumsLabel(albumIDs.length)} description={copy.workflow.scopeDescription} isDisabled={albumIDs.length === 0} />
              <RadioListItem value="account" label={copy.workflow.oneAccount} description={copy.workflow.oneAccountDescription} />
              <RadioListItem value="album" label={copy.workflow.oneAlbum} description={copy.workflow.oneAlbumDescription} />
              <RadioListItem value="savedQuery" label={copy.workflow.savedQuery} description={copy.workflow.savedQueryDescription} />
              <RadioListItem value="matching" label={copy.workflow.matching} description={copy.workflow.matchingDescription} isDisabled={!matchingScope} />
            </RadioList>

            {scopeMode === 'articles' ? <ArticleRemoteMultiSelector label={copy.workflow.selectedArticles} description={copy.workflow.selectedArticlesDescription} placeholder={copy.workflow.selectedArticles} selected={selectedArticles} onChange={selectArticles} copy={remoteSelectorCopy(messages)} /> : null}
            {scopeMode === 'account' ? <AccountRemoteSelector label={copy.workflow.oneAccount} value={accountID || undefined} selectedLabel={scope?.selection.kind === 'account' ? scope.label : undefined} onChange={(next, option) => selectAccount(next ?? '', option)} placeholder={copy.workflow.chooseAccount} copy={remoteSelectorCopy(messages)} /> : null}
            {scopeMode === 'album' ? <AlbumRemoteSelector label={copy.workflow.oneAlbum} value={albumID || undefined} selectedLabel={scope?.selection.kind === 'album' ? scope.label : undefined} onChange={(next, option) => selectAlbum(next ?? '', option)} placeholder={copy.workflow.chooseAlbum} copy={remoteSelectorCopy(messages)} /> : null}
            {scopeMode === 'savedQuery' ? <SearchableSelector label={copy.workflow.savedQuery} options={savedQueryOptions} value={savedQueryID || null} onChange={(next) => selectSavedQuery(next || '')} placeholder={copy.selection.savedQueryPlaceholder} copy={messages.selectors} hasClear isLoading={savedQueries.isLoading} /> : null}
            {scopeMode === 'matching' && matchingScope ? <div className="export-scope-summary"><strong>{matchingScopeLabel(initialHandoff?.presentation?.matching?.total, copy)}</strong><p>{describeArticleQuery(matchingScope.query, locale, messages, { accounts: accountNames, albums: albumNames })}</p><Button label={copy.workflow.useCurrentResults} variant="secondary" onClick={selectMatchingScope} /></div> : null}
            {scope ? <ScopeSummary label={scope.label} articles={scope.selection.kind === 'explicit_ids' ? selectedArticles : undefined} /> : null}
          </Panel>
        ) : null}

        {stage === 'format' ? (
          <Panel className="export-stage" aria-labelledby="export-format-title">
            <SectionHeader title={copy.workflow.format} titleId="export-format-title" description={copy.workflow.formatDescription} />
            <FormGrid>
              <Selector label={copy.format} options={formats.map((item) => ({ value: item, label: item.toUpperCase() }))} value={format} onChange={(value) => commitExportView({ stage, scope: scopeMode, format: value as ExportFormat })} />
              <FormGrid direction="horizontal">
                <TextInput label={copy.namingTemplate} value={namingTemplate} htmlName="export-naming-template" onChange={setNamingTemplate} />
                <NumberInput label={copy.maximumNameBytes} value={maximumNameBytes} onChange={setMaximumNameBytes} htmlName="export-maximum-name-bytes" autoComplete="off" min={1} step={1} isIntegerOnly isRequired />
              </FormGrid>
              <Selector label={copy.collision} options={[{ value: 'fail', label: copy.collisionFail }, { value: 'skip', label: copy.collisionSkip }, { value: 'replace', label: copy.collisionReplace }, { value: 'suffix', label: copy.collisionSuffix }]} value={collisionPolicy} htmlName="export-collision-policy" onChange={(value) => setCollisionPolicy(value as typeof collisionPolicy)} />
            </FormGrid>
            {formatOptions.length ? <fieldset className="export-options"><legend>{copy.formatOptions(format.toUpperCase())}</legend>{formatOptions.includes('content') ? <CheckboxInput label={copy.includeContent} value={includeContent} onChange={() => setIncludeContent((value) => !value)} /> : null}{formatOptions.includes('metadata') ? <CheckboxInput label={copy.includeMetadata} value={includeMetadata} onChange={() => setIncludeMetadata((value) => !value)} /> : null}{formatOptions.includes('comments') ? <CheckboxInput label={copy.includeComments} value={includeComments} onChange={() => setIncludeComments((value) => !value)} /> : null}</fieldset> : null}
            {format === 'html' ? <fieldset className="export-options"><legend>{copy.htmlOptions}</legend><Selector label={copy.resourcePolicy} options={[{ value: 'best-effort', label: copy.resourceBestEffort }, { value: 'strict', label: copy.resourceStrict }]} value={htmlResourcePolicy} onChange={(value) => setHTMLResourcePolicy(value as typeof htmlResourcePolicy)} /><TextInput label={copy.batchArchive} value={htmlBatchArchive} onChange={setHTMLBatchArchive} /><FieldHint>{copy.batchArchiveHint}</FieldHint></fieldset> : null}
          </Panel>
        ) : null}

        {stage === 'destination' ? (
          <Panel className="export-stage" aria-labelledby="export-destination-title">
            <SectionHeader title={copy.workflow.destination} titleId="export-destination-title" description={copy.workflow.destinationDescription} />
            {!directory ? <div className="export-destination-default"><p>{copy.workflow.authorizeDefaultDescription}</p><Button label={copy.authorize} variant="primary" isLoading={mutations.authorizeDefaultExportDirectory.isPending} onClick={authorizeDirectory} /></div> : <div className="export-directory-summary"><p><strong>{copy.authorized(directory.label)}</strong></p><p>{copy.workflow.destinationReady}</p></div>}
            {directory ? <details className="export-destination-details"><summary>{copy.workflow.optionalDestination}</summary><div className="export-destination-advanced"><TextInput label={copy.childName} placeholder={copy.childPlaceholder} value={childName} onChange={setChildName} /><Button label={copy.create} variant="secondary" isLoading={mutations.createExportDirectory.isPending} isDisabled={!childName.trim()} onClick={createDirectory} /><TextInput label={copy.subdirectory} value={subdirectory} onChange={setSubdirectory} /><FieldHint>{copy.subdirectoryHint}</FieldHint></div></details> : null}
            {directory ? <div className="confirmation-proof"><span>{copy.confirmation}</span><code translate="no">{`start-export:${directory.token}`}</code><p>{copy.confirmationHint}</p></div> : null}
          </Panel>
        ) : null}

        {notice ? <div className="export-notice" role="status" aria-live="polite"><p>{notice}</p></div> : null}
      </div>

      <div className="export-stage-primary" aria-label={copy.workflow.currentAction}>
        {stage !== 'scope' ? <Button label={copy.workflow.back} variant="secondary" onClick={() => commitExportView({ stage: stage === 'destination' ? 'format' : 'scope', scope: scopeMode, format })} /> : null}
        <Button label={primaryAction} variant="primary" isLoading={stage === 'destination' && mutations.startExport.isPending} isDisabled={primaryDisabled} onClick={advanceStage} />
      </div>

      <ExportRecords copy={copy} locale={locale} selectorCopy={messages.selectors} records={records} table={table} pageIndex={pageIndex} totalPages={totalPages} onPageChange={setPageIndex} />

      <Panel className="export-detail" aria-labelledby="export-detail-title">
        <SectionHeader title={copy.detailTitle} titleId="export-detail-title" description={copy.detailDescription} />
        <ActionGroup align="start" gap="cluster"><Button label={copy.loadManifest} variant="secondary" isLoading={manifest.isFetching} isDisabled={!selectedID} onClick={() => void manifest.refetch()} /><Button label={copy.verify} variant="secondary" isLoading={mutations.verifyExport.isPending} isDisabled={!selectedID} onClick={runVerification} /></ActionGroup>
        {!selectedID ? <FieldHint>{copy.selectOne}</FieldHint> : null}
        {selectedID ? <><div className="confirmation-proof"><span>{copy.confirmation}</span><code translate="no">{copy.verifyConfirmation(selectedID)}</code><p>{copy.confirmationHint}</p></div><TechnicalDetails label={copy.workflow.technicalDetails} items={[{ label: copy.workflow.exportID, value: selectedID, copyLabel: copy.workflow.copyValue, copiedLabel: messages.a11y.copied, copyFailedLabel: messages.a11y.copyUnavailable }]} /></> : null}
        {manifest.isLoading ? <p role="status">{copy.manifestLoading}</p> : null}
        {manifest.isError ? <p role="alert">{copy.manifestUnavailable}</p> : null}
        {manifest.data ? <Manifest messages={messages} manifest={manifest.data} locale={locale} /> : null}
        {mutations.verifyExport.data ? <Verification messages={messages} verification={mutations.verifyExport.data} /> : null}
      </Panel>

      <Panel className="export-output-actions" aria-labelledby="artifact-actions-title">
        <SectionHeader title={copy.artifactTitle} titleId="artifact-actions-title" description={copy.artifactDescription} />
        <ActionGroup align="start" gap="cluster"><Button label={copy.openAction} variant="secondary" isDisabled={!selectedID || openConfirmation !== expectedOpenConfirmation} onClick={openOutputDirectory} /></ActionGroup>
        {selectedID ? <><div className="confirmation-proof"><span>{copy.openConfirmationLabel}</span><code translate="no">{expectedOpenConfirmation}</code><p>{copy.openConfirmationHint}</p></div><TextInput label={copy.openConfirmationInput} value={openConfirmation} onChange={setOpenConfirmation} /></> : <FieldHint>{copy.selectOne}</FieldHint>}
        {outputNotice ? <p className="export-notice" role="status" aria-live="polite">{outputNotice}</p> : null}
      </Panel>
    </PageStack>
  )
}

function StageNavigationItem({ label, number, isActive, isComplete, isDisabled, onClick }: { readonly label: string; readonly number: number; readonly isActive: boolean; readonly isComplete: boolean; readonly isDisabled?: boolean; readonly onClick: () => void }) {
  return <li><Button label={`${number}. ${label}`} variant={isActive ? 'primary' : 'ghost'} isDisabled={isDisabled} onClick={onClick} /><span aria-hidden="true">{isComplete ? '✓' : number}</span></li>
}

function ScopeSummary({ label, articles }: { readonly label: string; readonly articles?: readonly ArticleOption[] }) {
  return <div className="export-scope-summary" role="status"><strong>{label}</strong>{articles?.length ? <ul>{articles.map((article, index) => <li key={`${article.title}-${index}`}>{article.title}{article.accountName?.trim() ? ` · ${article.accountName.trim()}` : ''}</li>)}</ul> : null}</div>
}

function ExportRecordLabel({ record, locale, copy }: { readonly record: ExportRecord; readonly locale: Locale; readonly copy: MessageCatalog['exports'] }) {
  return <div className="export-record-label"><strong>{copy.workflow.recordLabel(record.format)}</strong><span>{formatDateTime(record.createdAt, locale)}</span></div>
}

function ExportRecords({ copy, locale, selectorCopy, records, table, pageIndex, totalPages, onPageChange }: { readonly copy: MessageCatalog['exports']; readonly locale: Locale; readonly selectorCopy: MessageCatalog['selectors']; readonly records: ReturnType<typeof useExportPage>; readonly table: ReturnType<typeof useReactTable<ExportRecord>>; readonly pageIndex: number; readonly totalPages: number; readonly onPageChange: (updater: number | ((value: number) => number)) => void }) {
  return <section className="export-records" aria-labelledby="export-records-title">
    <SectionHeader title={copy.recordsTitle} titleId="export-records-title" description={copy.recordsDescription} />
    {records.isLoading ? <p role="status">{copy.loading}</p> : null}
    {records.isError ? <div className="error-state" role="alert"><p>{copy.unavailable}</p><Button label={copy.retry} variant="secondary" onClick={() => void records.refetch()} /></div> : null}
    {!records.isLoading && !records.isError ? <ResponsiveDataTable
      table={table}
      ariaLabel={copy.recordsTitle}
      visibleColumnsLabel={copy.visibleColumns}
      selectorCopy={selectorCopy}
      emptyContent={copy.empty}
      isBusy={records.isFetching}
      footer={<nav className="pagination" aria-label={copy.pagination}><Button label={copy.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => onPageChange((value) => value - 1)} /><span>{copy.page(pageIndex + 1, totalPages)}</span><Button label={copy.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => onPageChange((value) => value + 1)} /></nav>}
      renderMobileRows={(rows) => rows.map((row) => <MobileResourceRow key={row.id} title={<ExportRecordLabel record={row.original} locale={locale} copy={copy} />} description={row.original.format.toUpperCase()} isSelected={row.getIsSelected()} selectionLabel={copy.selectRow(copy.workflow.recordLabel(row.original.format))} onSelectionChange={(selected) => row.toggleSelected(selected)} status={<Status value={row.original.state} locale={locale} />} metadata={[{ id: 'created', label: copy.columns.created, value: formatDateTime(row.original.createdAt, locale), fullValue: row.original.createdAt }, { id: 'provenance', label: copy.columns.provenance, value: formatProvenance(row.original, locale) }]} />)}
    /> : null}
  </section>
}

function Manifest({ messages, manifest, locale }: { readonly messages: MessageCatalog; readonly manifest: ExportManifest; readonly locale: Locale }) {
  const copy = messages.exports
  const a11y = messages.a11y
  const fileColumns = useMemo<ColumnDef<ExportFile>[]>(() => [
    { accessorKey: 'path', header: copy.fileColumns.path, enableHiding: false, meta: { role: 'primaryText' }, cell: ({ row }) => safeFileName(row.original.path) },
    { accessorKey: 'sizeBytes', header: copy.fileColumns.size, meta: { role: 'numeric' }, cell: ({ getValue }) => formatBytes(getValue<number>(), locale) },
    { accessorKey: 'status', header: copy.fileColumns.status, meta: { role: 'status' }, cell: ({ getValue }) => <Status value={getValue<string>()} locale={locale} /> },
    { accessorKey: 'sha256', header: copy.fileColumns.checksum, meta: { role: 'identifier' as const }, cell: ({ getValue }) => <code title={getValue<string>()}>{formatShortIdentifier(getValue<string>(), 8)}</code> },
    { id: 'download', header: copy.fileColumns.download, enableHiding: false, meta: { role: 'actions' }, cell: ({ row }) => <a className="artifact-download" href={getExportArtifactDownloadURL(manifest.exportId, row.original.artifactId)}>{copy.downloadArtifact}</a> }
  ], [copy, locale, manifest.exportId])
  return <div className="manifest-detail"><p><strong>{copy.workflow.recordLabel(manifest.format)}</strong> · <Status value={manifest.state} locale={locale} /> · {formatProvenance(manifest, locale)}</p><TechnicalDetails label={copy.workflow.technicalDetails} items={[{ label: copy.workflow.exportID, value: manifest.exportId, copyLabel: copy.workflow.copyValue, copiedLabel: a11y.copied, copyFailedLabel: a11y.copyUnavailable }, { label: copy.workflow.provenanceGeneration, value: manifest.provenanceGeneration, copyLabel: copy.workflow.copyValue, copiedLabel: a11y.copied, copyFailedLabel: a11y.copyUnavailable }]} /><p>{copy.manifestSummary(manifest.files.length)}</p>{manifest.files.length ? <StaticResponsiveDataTable
    data={manifest.files}
    columns={fileColumns}
    ariaLabel={copy.files}
    visibleColumnsLabel={copy.visibleFileColumns}
    selectorCopy={messages.selectors}
    emptyContent={copy.noFiles}
    renderMobileRows={(rows) => rows.map((row) => <MobileResourceRow key={row.original.artifactId} title={safeFileName(row.original.path)} fullTitle={safeFileName(row.original.path)} status={<Status value={row.original.status} locale={locale} />} metadata={[{ id: 'size', label: copy.fileColumns.size, value: formatBytes(row.original.sizeBytes, locale) }, { id: 'checksum', label: copy.fileColumns.checksum, value: <code title={row.original.sha256}>{formatShortIdentifier(row.original.sha256, 8)}</code>, fullValue: row.original.sha256 }]} actions={<a className="artifact-download" href={getExportArtifactDownloadURL(manifest.exportId, row.original.artifactId)}>{copy.downloadArtifact}</a>} />)}
  /> : <p>{copy.noFiles}</p>}<TechnicalDetails label={copy.workflow.technicalDetails} items={manifest.files.map((file) => ({ label: safeFileName(file.path), value: file.path, copyLabel: copy.workflow.copyValue, copiedLabel: a11y.copied, copyFailedLabel: a11y.copyUnavailable }))} /></div>
}

function Verification({ messages, verification }: { readonly messages: MessageCatalog; readonly verification: ExportVerification }) {
  const copy = messages.exports
  const a11y = messages.a11y
  return <section className="verification-result" aria-live="polite"><h3>{copy.verificationTitle}</h3><p>{verification.valid ? copy.verificationValid(verification.verifiedOutputs) : copy.verificationInvalid(verification.verifiedOutputs)}</p>{verification.issues.length ? <><h4>{copy.verificationIssues}</h4><ul>{verification.issues.map((issue, index) => <li key={`${issue.path ?? 'issue'}-${index}`}>{copy.verificationIssue(index + 1)}</li>)}</ul><TechnicalDetails label={copy.workflow.technicalDetails} items={verification.issues.map((issue, index) => ({ label: copy.verificationIssue(index + 1), value: serializeVerificationIssue(issue), copyLabel: copy.workflow.copyValue, copiedLabel: a11y.copied, copyFailedLabel: a11y.copyUnavailable }))} /></> : null}</section>
}

function scopeModeFor(selection: ExportSelection | undefined): ScopeMode {
  switch (selection?.kind) {
    case 'account': return 'account'
    case 'album': return 'album'
    case 'album_ids': return 'albums'
    case 'saved_query': return 'savedQuery'
    case 'all_matching': return 'matching'
    default: return 'articles'
  }
}

function initialScopeChoice(handoff: NonNullable<ReturnType<typeof consumeExportHandoffForMount>>, copy: MessageCatalog['exports']): ScopeChoice {
  const { selection } = handoff
  switch (selection.kind) {
    case 'explicit_ids': return { selection, label: selectionLabelForHandoffArticles(handoff.presentation?.articles, selection.articleIds.length, handoff.label, copy) }
    case 'account': return { selection, label: handoff.label || copy.workflow.savedAccountFallback }
    case 'album': return { selection, label: handoff.label || copy.workflow.savedAlbumFallback }
    case 'album_ids': return { selection, label: handoff.label || copy.selection.albumsLabel(selection.albumIds.length) }
    case 'saved_query': return { selection, label: handoff.label || copy.selection.savedQueryLabel(selection.savedQueryId) }
    case 'all_matching': return { selection, label: matchingScopeLabel(handoff.presentation?.matching?.total, copy) }
  }
}

function scopeChoiceFromDraft(selection: ExportSelection, label: string | undefined, copy: MessageCatalog['exports']): ScopeChoice {
  switch (selection.kind) {
    case 'explicit_ids': return { selection, label: label || copy.workflow.selectedArticlesLabel(selection.articleIds.length) }
    case 'account': return { selection, label: label || copy.workflow.savedAccountFallback }
    case 'album': return { selection, label: label || copy.workflow.savedAlbumFallback }
    case 'album_ids': return { selection, label: label || copy.selection.albumsLabel(selection.albumIds.length) }
    case 'saved_query': return { selection, label: label || copy.selection.savedQueryLabel(selection.savedQueryId) }
    case 'all_matching': return { selection, label: label || copy.workflow.matchingSummary }
  }
}

function accountLabel(account: AccountOption, copy: MessageCatalog['exports']) { return account.displayName?.trim() || copy.workflow.savedAccountFallback }
function albumLabel(album: AlbumOption, copy: MessageCatalog['exports']) { return album.displayName?.trim() || copy.workflow.savedAlbumFallback }
function remoteSelectorCopy(messages: MessageCatalog) {
  return messages.selectors
}
function selectionLabelForArticles(articles: readonly ArticleOption[], ids: readonly string[], copy: MessageCatalog['exports']) { return articles.length === 1 ? articles[0].title : copy.workflow.selectedArticlesLabel(ids.length) }
function formatProvenance(value: { readonly provenanceState?: string; readonly provenanceGeneration: number }, locale: Locale) { return `${formatStatus(value.provenanceState, locale).label} · #${value.provenanceGeneration}` }

function articleOptionFromHandoff(id: string, presentation: { readonly title: string; readonly accountName?: string } | undefined, fallbackTitle: string): ArticleOption {
  const title = presentation?.title.trim() || fallbackTitle
  const accountName = presentation?.accountName?.trim()
  return { id, title, accountNameAvailable: Boolean(accountName), ...(accountName ? { accountName } : {}) }
}

function nameMapForMatchingScope(id: string | undefined, name: string | undefined): ReadonlyMap<string, string> {
  const displayName = name?.trim()
  return id && displayName ? new Map([[id, displayName]]) : new Map()
}

function matchingScopeLabel(total: number | undefined, copy: MessageCatalog['exports']): string {
  return typeof total === 'number' ? copy.workflow.selectedArticlesLabel(total) : copy.workflow.matchingSummary
}

function selectionLabelForHandoffArticles(articles: readonly { readonly title: string }[] | undefined, count: number, fallbackLabel: string, copy: MessageCatalog['exports']): string {
  return count === 1 && articles?.[0]?.title.trim() ? articles[0].title.trim() : fallbackLabel || copy.workflow.selectedArticlesLabel(count)
}

function safeFileName(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean)
  return parts.at(-1)?.trim() || '—'
}

function serializeVerificationIssue(issue: ExportVerification['issues'][number]): string {
  const detail: ExportVerificationDetail = {}
  if (issue.path) detail.path = issue.path
  if (issue.message) detail.message = issue.message
  if (issue.expected) detail.expected = issue.expected
  if (issue.actual) detail.actual = issue.actual
  return JSON.stringify(detail)
}

type ExportToggle = 'content' | 'metadata' | 'comments'

function formatOptionsFor(format: ExportFormat): readonly ExportToggle[] {
  switch (format) {
    case 'html': return ['comments']
    case 'markdown':
    case 'text': return ['metadata', 'comments']
    case 'json': return ['content', 'metadata', 'comments']
    case 'xlsx': return ['content']
    case 'docx':
    case 'pdf': return ['comments']
  }
}

function exportFormatOptions(format: ExportFormat, values: { readonly includeContent: boolean; readonly includeMetadata: boolean; readonly includeComments: boolean; readonly htmlResourcePolicy: 'best-effort' | 'strict'; readonly htmlBatchArchive: string }): ExportOptions['formatOptions'] {
  const options: Record<string, boolean | string> = {}
  const toggleValues: Record<ExportToggle, boolean> = { content: values.includeContent, metadata: values.includeMetadata, comments: values.includeComments }
  for (const option of formatOptionsFor(format)) options[option] = toggleValues[option]
  if (format === 'html') {
    options.htmlResourcePolicy = values.htmlResourcePolicy
    if (values.htmlBatchArchive.trim()) options.htmlBatchArchive = values.htmlBatchArchive.trim()
  }
  return options
}
