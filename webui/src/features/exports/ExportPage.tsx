import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { FormLayout } from '@astryxdesign/core/FormLayout'
import { NumberInput } from '@astryxdesign/core/NumberInput'
import { RadioList, RadioListItem } from '@astryxdesign/core/RadioList'
import { Selector } from '@astryxdesign/core/Selector'
import { TextInput } from '@astryxdesign/core/TextInput'
import { AccountRemoteSelector, AlbumRemoteSelector, ArticleRemoteMultiSelector, MobileResourceRow, Status, TechnicalDetails } from '../../components/presentation'
import { formatBytes, formatDateTime, formatShortIdentifier, formatStatus } from '../../lib/presentation'
import { describeArticleQuery } from '../articles/articleQueryPresentation'
import { useEffect, useMemo, useState } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
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
  type ExportFormat,
  type ExportManifest,
  type ExportOptions,
  type ExportRecord,
  type ExportSelection,
  type ExportVerification,
} from '../../lib/api'
import { handoffCreatedJob } from '../../lib/jobHandoff'
import { useExportManifest, useExportPage, useSavedQueryPage, useWorkspaceMutations } from '../../lib/queries'
import './export.css'

const pageSize = 25
const formats: readonly ExportFormat[] = ['markdown', 'html', 'text', 'json', 'xlsx', 'docx', 'pdf']

interface ExportPageProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
}

type Stage = 'scope' | 'format' | 'destination'
type ScopeMode = 'articles' | 'albums' | 'account' | 'album' | 'savedQuery' | 'matching'

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
  // Export handoff is deliberately single-use session state. Capture it once
  // per Strict Mode render pair so query/cache rerenders cannot consume and
  // erase its safe presentation labels after the initial scope is restored.
  const [initialHandoff] = useState(consumeExportHandoffForMount)
  const [stage, setStage] = useState<Stage>('scope')
  const [scopeMode, setScopeMode] = useState<ScopeMode>(() => scopeModeFor(initialHandoff?.selection))
  const [scope, setScope] = useState<ScopeChoice | undefined>(() => initialHandoff ? initialScopeChoice(initialHandoff, copy) : undefined)
  // Stable IDs remain exclusively in the export action contract. The handoff
  // presentation has only human-readable names and titles, so it can restore
  // cross-page context without exposing identifiers in the UI.
  const [selectedArticles, setSelectedArticles] = useState<readonly ArticleOption[]>(() => initialHandoff?.selection.kind === 'explicit_ids'
    ? initialHandoff.selection.articleIds.map((id, index) => articleOptionFromHandoff(id, initialHandoff.presentation?.articles?.[index], copy.workflow.selectedArticles))
    : [])
  const [albumIDs] = useState<readonly string[]>(() => initialHandoff?.selection.kind === 'album_ids' ? initialHandoff.selection.albumIds : [])
  const [accountID, setAccountID] = useState(() => initialHandoff?.selection.kind === 'account' ? initialHandoff.selection.accountId : '')
  const [albumID, setAlbumID] = useState(() => initialHandoff?.selection.kind === 'album' ? initialHandoff.selection.albumId : '')
  const [savedQueryID, setSavedQueryID] = useState(() => initialHandoff?.selection.kind === 'saved_query' ? initialHandoff.selection.savedQueryId : '')
  const [matchingScope] = useState(() => initialHandoff?.selection.kind === 'all_matching' ? initialHandoff.selection : undefined)
  const [format, setFormat] = useState<ExportFormat>('markdown')
  const [subdirectory, setSubdirectory] = useState('')
  const [namingTemplate, setNamingTemplate] = useState('{published}-{title}')
  const [maximumNameBytes, setMaximumNameBytes] = useState<number | null>(180)
  const [collisionPolicy, setCollisionPolicy] = useState<'fail' | 'skip' | 'replace' | 'suffix'>('fail')
  const [includeContent, setIncludeContent] = useState(true)
  const [includeMetadata, setIncludeMetadata] = useState(true)
  const [includeComments, setIncludeComments] = useState(false)
  const [htmlResourcePolicy, setHTMLResourcePolicy] = useState<'best-effort' | 'strict'>('best-effort')
  const [htmlBatchArchive, setHTMLBatchArchive] = useState('')
  const [directory, setDirectory] = useState<ExportDirectory>()
  const [childName, setChildName] = useState('')
  const [notice, setNotice] = useState<string>()
  const [outputNotice, setOutputNotice] = useState<string>()
  const [pageIndex, setPageIndex] = useState(0)
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
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
    { id: 'select', header: ({ table }) => <CheckboxInput label={copy.selectAll} isLabelHidden value={table.getIsSomePageRowsSelected() ? 'indeterminate' : table.getIsAllPageRowsSelected()} onChange={() => table.toggleAllPageRowsSelected()} />, cell: ({ row }) => <CheckboxInput label={copy.selectRow(copy.workflow.recordLabel(row.original.format))} isLabelHidden value={row.getIsSelected()} onChange={() => row.toggleSelected()} /> },
    { id: 'label', header: copy.recordsTitle, meta: { role: 'primaryText' }, cell: ({ row }) => <ExportRecordLabel record={row.original} locale={locale} copy={copy} /> },
    { accessorKey: 'format', header: copy.columns.format, meta: { role: 'secondaryText' }, cell: ({ getValue }) => getValue<string>().toUpperCase() },
    { accessorKey: 'state', header: copy.columns.state, meta: { role: 'status' }, cell: ({ getValue }) => <Status value={getValue<string>()} locale={locale} /> },
    { accessorKey: 'createdAt', header: copy.columns.created, meta: { role: 'dateTime' }, cell: ({ getValue }) => formatDateTime(getValue<string>(), locale) },
    { accessorKey: 'provenanceState', header: copy.columns.provenance, meta: { role: 'description' }, cell: ({ row }) => formatProvenance(row.original, locale) }
  ], [copy, locale])
  // TanStack table exposes a mutable table instance. It is rendered directly.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({ data: records.data ? [...records.data.data] : [], columns, getCoreRowModel: getCoreRowModel(), state: { rowSelection }, onRowSelectionChange: setRowSelection, getRowId: (row) => row.id, enableRowSelection: true })
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
    setScopeMode(next)
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
      setStage('format')
      return
    }
    if (stage === 'format') {
      if (!isFormatValid) return setNotice(copy.actionFailed)
      setStage('destination')
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
    <section aria-labelledby="exports-title">
      <header className="page-heading">
        <div><p className="eyebrow">{messages.navigation.operations}</p><h1 id="exports-title">{copy.title}</h1><p className="lede">{copy.description}</p></div>
      </header>

      <div className="export-stage-shell">
        <ol className="export-stage-nav" aria-label={copy.workflow.stages}>
          <StageNavigationItem label={copy.workflow.scope} number={1} isActive={stage === 'scope'} isComplete={Boolean(scope)} onClick={() => setStage('scope')} />
          <StageNavigationItem label={copy.workflow.format} number={2} isActive={stage === 'format'} isComplete={stage === 'destination'} isDisabled={!scope} onClick={() => scope && setStage('format')} />
          <StageNavigationItem label={copy.workflow.destination} number={3} isActive={stage === 'destination'} isComplete={false} isDisabled={!scope || !isFormatValid} onClick={() => scope && isFormatValid && setStage('destination')} />
        </ol>

        {stage === 'scope' ? (
          <section className="workspace-panel export-stage" aria-labelledby="export-scope-title">
            <div><h2 id="export-scope-title">{copy.workflow.scope}</h2><p>{copy.workflow.scopeDescription}</p></div>
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
            {scopeMode === 'savedQuery' ? <Selector label={copy.workflow.savedQuery} options={savedQueryOptions} value={savedQueryID} onChange={(next) => selectSavedQuery(next || '')} placeholder={copy.selection.savedQueryPlaceholder} hasClear hasSearch isLoading={savedQueries.isLoading} /> : null}
            {scopeMode === 'matching' && matchingScope ? <div className="export-scope-summary"><strong>{matchingScopeLabel(initialHandoff?.presentation?.matching?.total, copy)}</strong><p>{describeArticleQuery(matchingScope.query, locale, messages, { accounts: accountNames, albums: albumNames })}</p><Button label={copy.workflow.useCurrentResults} variant="secondary" onClick={selectMatchingScope} /></div> : null}
            {scope ? <ScopeSummary label={scope.label} articles={scope.selection.kind === 'explicit_ids' ? selectedArticles : undefined} /> : null}
          </section>
        ) : null}

        {stage === 'format' ? (
          <section className="workspace-panel export-stage" aria-labelledby="export-format-title">
            <div><h2 id="export-format-title">{copy.workflow.format}</h2><p>{copy.workflow.formatDescription}</p></div>
            <FormLayout>
              <Selector label={copy.format} options={formats.map((item) => ({ value: item, label: item.toUpperCase() }))} value={format} onChange={(value) => setFormat(value as ExportFormat)} />
              <FormLayout direction="horizontal">
                <TextInput label={copy.namingTemplate} value={namingTemplate} onChange={setNamingTemplate} />
                <NumberInput label={copy.maximumNameBytes} value={maximumNameBytes} onChange={setMaximumNameBytes} min={1} step={1} isIntegerOnly isRequired />
              </FormLayout>
              <Selector label={copy.collision} options={[{ value: 'fail', label: copy.collisionFail }, { value: 'skip', label: copy.collisionSkip }, { value: 'replace', label: copy.collisionReplace }, { value: 'suffix', label: copy.collisionSuffix }]} value={collisionPolicy} onChange={(value) => setCollisionPolicy(value as typeof collisionPolicy)} />
            </FormLayout>
            {formatOptions.length ? <fieldset className="export-options"><legend>{copy.formatOptions(format.toUpperCase())}</legend>{formatOptions.includes('content') ? <CheckboxInput label={copy.includeContent} value={includeContent} onChange={() => setIncludeContent((value) => !value)} /> : null}{formatOptions.includes('metadata') ? <CheckboxInput label={copy.includeMetadata} value={includeMetadata} onChange={() => setIncludeMetadata((value) => !value)} /> : null}{formatOptions.includes('comments') ? <CheckboxInput label={copy.includeComments} value={includeComments} onChange={() => setIncludeComments((value) => !value)} /> : null}</fieldset> : null}
            {format === 'html' ? <fieldset className="export-options"><legend>{copy.htmlOptions}</legend><Selector label={copy.resourcePolicy} options={[{ value: 'best-effort', label: copy.resourceBestEffort }, { value: 'strict', label: copy.resourceStrict }]} value={htmlResourcePolicy} onChange={(value) => setHTMLResourcePolicy(value as typeof htmlResourcePolicy)} /><TextInput label={copy.batchArchive} value={htmlBatchArchive} onChange={setHTMLBatchArchive} /><p className="field-hint">{copy.batchArchiveHint}</p></fieldset> : null}
          </section>
        ) : null}

        {stage === 'destination' ? (
          <section className="workspace-panel export-stage" aria-labelledby="export-destination-title">
            <div><h2 id="export-destination-title">{copy.workflow.destination}</h2><p>{copy.workflow.destinationDescription}</p></div>
            {!directory ? <div className="export-destination-default"><p>{copy.workflow.authorizeDefaultDescription}</p><Button label={copy.authorize} variant="primary" isLoading={mutations.authorizeDefaultExportDirectory.isPending} onClick={authorizeDirectory} /></div> : <div className="export-directory-summary"><p><strong>{copy.authorized(directory.label)}</strong></p><p>{copy.workflow.destinationReady}</p></div>}
            {directory ? <details className="export-destination-details"><summary>{copy.workflow.optionalDestination}</summary><div className="export-destination-advanced"><TextInput label={copy.childName} placeholder={copy.childPlaceholder} value={childName} onChange={setChildName} /><Button label={copy.create} variant="secondary" isLoading={mutations.createExportDirectory.isPending} isDisabled={!childName.trim()} onClick={createDirectory} /><TextInput label={copy.subdirectory} value={subdirectory} onChange={setSubdirectory} /><p className="field-hint">{copy.subdirectoryHint}</p></div></details> : null}
            {directory ? <div className="confirmation-proof"><span>{copy.confirmation}</span><code>{`start-export:${directory.token}`}</code><p>{copy.confirmationHint}</p></div> : null}
          </section>
        ) : null}

        {notice ? <div className="export-notice" role="status" aria-live="polite"><p>{notice}</p></div> : null}
      </div>

      <div className="export-stage-primary" aria-label={copy.workflow.currentAction}>
        {stage !== 'scope' ? <Button label={copy.workflow.back} variant="secondary" onClick={() => setStage(stage === 'destination' ? 'format' : 'scope')} /> : null}
        <Button label={primaryAction} variant="primary" isLoading={stage === 'destination' && mutations.startExport.isPending} isDisabled={primaryDisabled} onClick={advanceStage} />
      </div>

      <ExportRecords copy={copy} locale={locale} records={records} table={table} pageIndex={pageIndex} totalPages={totalPages} onPageChange={setPageIndex} />

      <section className="workspace-panel export-detail" aria-labelledby="export-detail-title">
        <h2 id="export-detail-title">{copy.detailTitle}</h2><p>{copy.detailDescription}</p>
        <div className="export-actions"><Button label={copy.loadManifest} variant="secondary" isLoading={manifest.isFetching} isDisabled={!selectedID} onClick={() => void manifest.refetch()} /><Button label={copy.verify} variant="secondary" isLoading={mutations.verifyExport.isPending} isDisabled={!selectedID} onClick={runVerification} /></div>
        {!selectedID ? <p className="field-hint">{copy.selectOne}</p> : null}
        {selectedID ? <><div className="confirmation-proof"><span>{copy.confirmation}</span><code>{copy.verifyConfirmation(selectedID)}</code><p>{copy.confirmationHint}</p></div><TechnicalDetails label={copy.workflow.technicalDetails} items={[{ label: copy.workflow.exportID, value: selectedID, copyLabel: copy.workflow.copyValue }]} /></> : null}
        {manifest.isLoading ? <p role="status">{copy.manifestLoading}</p> : null}
        {manifest.isError ? <p role="alert">{copy.manifestUnavailable}</p> : null}
        {manifest.data ? <Manifest messages={copy} manifest={manifest.data} locale={locale} /> : null}
        {mutations.verifyExport.data ? <Verification messages={copy} verification={mutations.verifyExport.data} /> : null}
      </section>

      <section className="workspace-panel export-output-actions" aria-labelledby="artifact-actions-title">
        <div><h2 id="artifact-actions-title">{copy.artifactTitle}</h2><p>{copy.artifactDescription}</p></div>
        <div className="export-actions"><Button label={copy.openAction} variant="secondary" isDisabled={!selectedID || openConfirmation !== expectedOpenConfirmation} onClick={openOutputDirectory} /></div>
        {selectedID ? <><div className="confirmation-proof"><span>{copy.openConfirmationLabel}</span><code>{expectedOpenConfirmation}</code><p>{copy.openConfirmationHint}</p></div><TextInput label={copy.openConfirmationInput} value={openConfirmation} onChange={setOpenConfirmation} /></> : <p className="field-hint">{copy.selectOne}</p>}
        {outputNotice ? <p className="export-notice" role="status" aria-live="polite">{outputNotice}</p> : null}
      </section>
    </section>
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

function ExportRecords({ copy, locale, records, table, pageIndex, totalPages, onPageChange }: { readonly copy: MessageCatalog['exports']; readonly locale: Locale; readonly records: ReturnType<typeof useExportPage>; readonly table: ReturnType<typeof useReactTable<ExportRecord>>; readonly pageIndex: number; readonly totalPages: number; readonly onPageChange: (updater: number | ((value: number) => number)) => void }) {
  return <section className="export-records" aria-labelledby="export-records-title">
    <header><h2 id="export-records-title">{copy.recordsTitle}</h2><p>{copy.recordsDescription}</p></header>
    {records.isLoading ? <p role="status">{copy.loading}</p> : null}
    {records.isError ? <div className="error-state" role="alert"><p>{copy.unavailable}</p><Button label={copy.retry} variant="secondary" onClick={() => void records.refetch()} /></div> : null}
    {!records.isLoading && !records.isError ? <><div className="data-table-wrap export-records-table" aria-busy={records.isFetching}><table className="data-table"><thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id} scope="col">{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead><tbody>{table.getRowModel().rows.map((row) => <tr key={row.id} data-selected={row.getIsSelected() || undefined}>{row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}{table.getRowModel().rows.length === 0 ? <tr><td colSpan={table.getVisibleLeafColumns().length}>{copy.empty}</td></tr> : null}</tbody></table></div><div className="export-records-mobile" aria-label={copy.recordsTitle}>{table.getRowModel().rows.map((row) => <MobileResourceRow key={row.id} title={<ExportRecordLabel record={row.original} locale={locale} copy={copy} />} description={row.original.format.toUpperCase()} isSelected={row.getIsSelected()} selectionLabel={copy.selectRow(copy.workflow.recordLabel(row.original.format))} onSelectionChange={(selected) => row.toggleSelected(selected)} status={<Status value={row.original.state} locale={locale} />} metadata={[{ id: 'created', label: copy.columns.created, value: formatDateTime(row.original.createdAt, locale), fullValue: row.original.createdAt }, { id: 'provenance', label: copy.columns.provenance, value: formatProvenance(row.original, locale) }]} />)}</div><nav className="pagination" aria-label={copy.pagination}><Button label={copy.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => onPageChange((value) => value - 1)} /><span>{copy.page(pageIndex + 1, totalPages)}</span><Button label={copy.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => onPageChange((value) => value + 1)} /></nav></> : null}
  </section>
}

function Manifest({ messages, manifest, locale }: { readonly messages: MessageCatalog['exports']; readonly manifest: ExportManifest; readonly locale: Locale }) {
  return <div className="manifest-detail"><p><strong>{messages.workflow.recordLabel(manifest.format)}</strong> · <Status value={manifest.state} locale={locale} /> · {formatProvenance(manifest, locale)}</p><TechnicalDetails label={messages.workflow.technicalDetails} items={[{ label: messages.workflow.exportID, value: manifest.exportId, copyLabel: messages.workflow.copyValue }, { label: messages.workflow.provenanceGeneration, value: manifest.provenanceGeneration, copyLabel: messages.workflow.copyValue }]} /><p>{messages.manifestSummary(manifest.files.length)}</p>{manifest.files.length ? <div className="data-table-wrap"><table className="data-table"><thead><tr><th scope="col">{messages.fileColumns.path}</th><th scope="col">{messages.fileColumns.size}</th><th scope="col">{messages.fileColumns.status}</th><th scope="col">{messages.fileColumns.checksum}</th><th scope="col">{messages.fileColumns.download}</th></tr></thead><tbody>{manifest.files.map((file) => <tr key={file.artifactId}><td>{safeFileName(file.path)}</td><td>{formatBytes(file.sizeBytes, locale)}</td><td><Status value={file.status} locale={locale} /></td><td><code title={file.sha256}>{formatShortIdentifier(file.sha256, 8)}</code></td><td><a className="artifact-download" href={getExportArtifactDownloadURL(manifest.exportId, file.artifactId)}>{messages.downloadArtifact}</a></td></tr>)}</tbody></table></div> : <p>{messages.noFiles}</p>}<TechnicalDetails label={messages.workflow.technicalDetails} items={manifest.files.map((file) => ({ label: safeFileName(file.path), value: file.path, copyLabel: messages.workflow.copyValue }))} /></div>
}

function Verification({ messages, verification }: { readonly messages: MessageCatalog['exports']; readonly verification: ExportVerification }) {
  return <section className="verification-result" aria-live="polite"><h3>{messages.verificationTitle}</h3><p>{verification.valid ? messages.verificationValid(verification.verifiedOutputs) : messages.verificationInvalid(verification.verifiedOutputs)}</p>{verification.issues.length ? <><h4>{messages.verificationIssues}</h4><ul>{verification.issues.map((issue, index) => <li key={`${issue.path ?? 'issue'}-${index}`}>{messages.verificationIssue(index + 1)}</li>)}</ul><TechnicalDetails label={messages.workflow.technicalDetails} items={verification.issues.map((issue, index) => ({ label: messages.verificationIssue(index + 1), value: serializeVerificationIssue(issue), copyLabel: messages.workflow.copyValue }))} /></> : null}</section>
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

function accountLabel(account: AccountOption, copy: MessageCatalog['exports']) { return account.displayName?.trim() || copy.workflow.savedAccountFallback }
function albumLabel(album: AlbumOption, copy: MessageCatalog['exports']) { return album.displayName?.trim() || copy.workflow.savedAlbumFallback }
function remoteSelectorCopy(messages: MessageCatalog) {
  const copy = messages.articles.ux
  return { unavailable: copy.accountUnavailable, noResults: copy.selectorNoResults, duplicate: copy.duplicateSelection }
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
