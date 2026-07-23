import { Button } from '@astryxdesign/core/Button'
import { CheckboxInput } from '@astryxdesign/core/CheckboxInput'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { TextInput } from '@astryxdesign/core/TextInput'
import { useEffect, useMemo, useState } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import type { Locale, MessageCatalog } from '../../i18n'
import { consumeExportHandoff, getExportArtifactDownloadURL, openExportOutput, parseArticleQuery, type ExportFormat, type ExportManifest, type ExportRecord, type ExportSelection, type ExportVerification } from '../../lib/api'
import { useExportManifest, useExportPage, useSavedQueryPage, useWorkspaceMutations } from '../../lib/queries'

const pageSize = 25
const formats: readonly ExportFormat[] = ['markdown', 'html', 'text', 'json', 'xlsx', 'docx', 'pdf']

interface ExportPageProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
}

export function ExportPage({ locale, messages }: ExportPageProps) {
  const copy = messages.exports
  const [directory, setDirectory] = useState<{ readonly token: string; readonly label: string }>()
  const [childName, setChildName] = useState('')
  const initialHandoff = consumeExportHandoff()
  const [articleIDs, setArticleIDs] = useState(() => initialHandoff?.selection.kind === 'explicit_ids' ? initialHandoff.selection.articleIds.join('\n') : '')
  const [selection, setSelection] = useState<ExportSelection | undefined>(initialHandoff?.selection)
  const [selectionLabel, setSelectionLabel] = useState(initialHandoff?.label ?? '')
  const [accountID, setAccountID] = useState('')
  const [albumID, setAlbumID] = useState('')
  const [savedQueryID, setSavedQueryID] = useState('')
  const [matchingQueryText, setMatchingQueryText] = useState('{}')
  const [format, setFormat] = useState<ExportFormat>('markdown')
  const [subdirectory, setSubdirectory] = useState('')
  const [namingTemplate, setNamingTemplate] = useState('{published}-{title}')
  const [maximumNameBytes, setMaximumNameBytes] = useState('180')
  const [collisionPolicy, setCollisionPolicy] = useState<'fail' | 'skip' | 'replace' | 'suffix'>('fail')
  const [includeContent, setIncludeContent] = useState(true)
  const [includeMetadata, setIncludeMetadata] = useState(true)
  const [includeComments, setIncludeComments] = useState(false)
  const [htmlResourcePolicy, setHTMLResourcePolicy] = useState<'best-effort' | 'strict'>('best-effort')
  const [htmlBatchArchive, setHTMLBatchArchive] = useState('')
  const [notice, setNotice] = useState<string>()
  const [outputNotice, setOutputNotice] = useState<string>()
  const [pageIndex, setPageIndex] = useState(0)
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})
  const [openConfirmation, setOpenConfirmation] = useState('')
  const mutations = useWorkspaceMutations()
  const records = useExportPage({ page: pageIndex + 1, pageSize })
  const savedQueries = useSavedQueryPage({ page: 1, pageSize: 100 })
  const selectedIDs = Object.entries(rowSelection).filter(([, selected]) => selected).map(([id]) => id)
  const selectedID = selectedIDs.length === 1 ? selectedIDs[0] : undefined
  const manifest = useExportManifest(selectedID)
  const ids = parseArticleIDs(articleIDs)
  const confirmation = directory ? `start-export:${directory.token}` : '—'
  const isHTML = format === 'html'

  useEffect(() => {
    if (selection?.kind === 'explicit_ids') setArticleIDs(selection.articleIds.join('\n'))
  }, [selection])

  useEffect(() => {
    setRowSelection((current) => Object.fromEntries(Object.entries(current).filter(([id]) => records.data?.data.some((record) => record.id === id))))
  }, [records.data])

  useEffect(() => {
    setOpenConfirmation('')
  }, [selectedID])

  const columns = useMemo<ColumnDef<ExportRecord>[]>(() => [
    { id: 'select', header: ({ table }) => <CheckboxInput label={copy.selectAll} isLabelHidden value={table.getIsSomePageRowsSelected() ? 'indeterminate' : table.getIsAllPageRowsSelected()} onChange={() => table.toggleAllPageRowsSelected()} />, cell: ({ row }) => <CheckboxInput label={copy.selectRow(row.original.id)} isLabelHidden value={row.getIsSelected()} onChange={() => row.toggleSelected()} /> },
    { accessorKey: 'id', header: copy.columns.id, cell: ({ getValue }) => <code>{getValue<string>()}</code> },
    { accessorKey: 'format', header: copy.columns.format },
    { accessorKey: 'state', header: copy.columns.state, cell: ({ getValue }) => <State value={getValue<string>()} locale={locale} /> },
    { accessorKey: 'createdAt', header: copy.columns.created, cell: ({ getValue }) => formatDate(getValue<string>(), locale) },
    { accessorKey: 'provenanceState', header: copy.columns.provenance, cell: ({ row }) => formatProvenance(row.original) }
  ], [copy, locale])
  // TanStack table exposes a mutable table instance. It is rendered directly.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({ data: records.data ? [...records.data.data] : [], columns, getCoreRowModel: getCoreRowModel(), state: { rowSelection }, onRowSelectionChange: setRowSelection, getRowId: (row) => row.id, enableRowSelection: true })
  const totalPages = records.data ? Math.max(1, Math.ceil(records.data.pagination.total / records.data.pagination.pageSize)) : 1

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
    if (!directory) return setNotice(copy.invalidDirectory)
    const resolvedSelection = selection?.kind === 'explicit_ids' ? { kind: 'explicit_ids' as const, articleIds: ids } : selection
    if (!resolvedSelection || (resolvedSelection.kind === 'explicit_ids' && resolvedSelection.articleIds.length === 0)) return setNotice(copy.invalidSelection)
    const maximum = Number(maximumNameBytes)
    mutations.startExport.mutate({
      directoryToken: directory.token,
      subdirectory: subdirectory.trim() || undefined,
      selection: resolvedSelection,
      format,
      options: {
        namingTemplate: namingTemplate.trim() || undefined,
        maximumNameBytes: Number.isInteger(maximum) ? maximum : undefined,
        collisionPolicy,
        formatOptions: {
          content: includeContent,
          metadata: includeMetadata,
          comments: includeComments,
          htmlResourcePolicy,
          ...(isHTML && htmlBatchArchive.trim() ? { htmlBatchArchive: htmlBatchArchive.trim() } : {})
        }
      }
    }, {
      onSuccess: (result) => setNotice(copy.queued(result.jobId)),
      onError: () => setNotice(copy.actionFailed)
    })
  }

  function selectExplicitIDs(value: string) {
    setArticleIDs(value)
    const next = parseArticleIDs(value)
    setSelection(next.length ? { kind: 'explicit_ids', articleIds: next } : undefined)
    setSelectionLabel(next.length ? copy.selection.explicit(next.length) : '')
  }

  function selectAccount() {
    const value = accountID.trim()
    setSelection(value ? { kind: 'account', accountId: value } : undefined)
    setSelectionLabel(value ? copy.selection.accountLabel(value) : '')
  }

  function selectAlbum() {
    const value = albumID.trim()
    setSelection(value ? { kind: 'album', albumId: value } : undefined)
    setSelectionLabel(value ? copy.selection.albumLabel(value) : '')
  }

  function selectSavedQuery(value: string) {
    setSavedQueryID(value)
    setSelection(value ? { kind: 'saved_query', savedQueryId: value } : undefined)
    setSelectionLabel(value ? copy.selection.savedQueryLabel(value) : '')
  }

  function selectMatchingQuery() {
    try {
      const query = parseArticleQuery(JSON.parse(matchingQueryText))
      setSelection({ kind: 'all_matching', query })
      setSelectionLabel(copy.selection.matchingLabel)
      setNotice(undefined)
    } catch { setNotice(copy.invalidSelection) }
  }

  function runVerification() {
    if (!selectedID) return setNotice(copy.selectOne)
    mutations.verifyExport.mutate(selectedID, { onSuccess: () => setNotice(undefined), onError: () => setNotice(copy.actionFailed) })
  }

  function openOutputDirectory() {
    if (!selectedID) return setNotice(copy.selectOne)
    void openExportOutput(selectedID, openConfirmation).then(
      () => { setOpenConfirmation(''); setOutputNotice(copy.outputOpened) },
      () => setOutputNotice(copy.actionFailed)
    )
  }

  const expectedOpenConfirmation = selectedID ? copy.openConfirmation(selectedID) : ''

  return (
    <section aria-labelledby="exports-title">
      <header className="page-heading">
        <div><p className="eyebrow">{messages.navigation.operations}</p><h1 id="exports-title">{copy.title}</h1><p className="lede">{copy.description}</p></div>
      </header>

      <div className="export-layout">
        <section className="workspace-panel" aria-labelledby="export-directory-title">
          <h2 id="export-directory-title">{copy.setupTitle}</h2><p>{copy.setupDescription}</p>
          <div className="export-actions"><Button label={copy.authorize} variant="secondary" isLoading={mutations.authorizeDefaultExportDirectory.isPending} onClick={authorizeDirectory} /></div>
          {directory ? <div className="export-directory-summary"><p><strong>{copy.authorized(directory.label)}</strong></p><label>{copy.directoryToken}<code>{directory.token}</code></label></div> : null}
          <div className="export-inline-form"><TextInput label={copy.childName} placeholder={copy.childPlaceholder} value={childName} onChange={setChildName} /><Button label={copy.create} variant="secondary" isLoading={mutations.createExportDirectory.isPending} isDisabled={!directory || !childName.trim()} onClick={createDirectory} /></div>
        </section>

        <section className="workspace-panel" aria-labelledby="export-selection-title">
          <h2 id="export-selection-title">{copy.selectionTitle}</h2>
          <fieldset className="export-options"><legend>{copy.selection.title}</legend>
            <label className="export-textarea-label">{copy.articleIds}<textarea value={articleIDs} onChange={(event) => selectExplicitIDs(event.target.value)} aria-describedby="article-ids-help" rows={4} /></label><p id="article-ids-help" className="field-hint">{copy.articleIdsHint}</p>
            <div className="export-field-grid"><TextInput label={copy.selection.accountId} value={accountID} onChange={setAccountID} /><Button label={copy.selection.account} variant="secondary" isDisabled={!accountID.trim()} onClick={selectAccount} /><TextInput label={copy.selection.albumId} value={albumID} onChange={setAlbumID} /><Button label={copy.selection.album} variant="secondary" isDisabled={!albumID.trim()} onClick={selectAlbum} /></div>
            <label>{copy.selection.savedQuery}<select value={savedQueryID} onChange={(event) => selectSavedQuery(event.target.value)}><option value="">{copy.selection.savedQueryPlaceholder}</option>{savedQueries.data?.data.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label>
            <label className="export-textarea-label">{copy.selection.matchingQuery}<textarea value={matchingQueryText} onChange={(event) => setMatchingQueryText(event.target.value)} rows={4} /></label><Button label={copy.selection.matching} variant="secondary" onClick={selectMatchingQuery} />
            {selection ? <p className="field-hint" role="status">{copy.selection.active(selectionLabel)}</p> : null}
          </fieldset>
          <div className="export-field-grid">
            <label>{copy.format}<select value={format} onChange={(event) => setFormat(event.target.value as ExportFormat)}>{formats.map((item) => <option key={item} value={item}>{item.toUpperCase()}</option>)}</select></label>
            <TextInput label={copy.subdirectory} value={subdirectory} onChange={setSubdirectory} />
            <TextInput label={copy.namingTemplate} value={namingTemplate} onChange={setNamingTemplate} />
            <TextInput label={copy.maximumNameBytes} value={maximumNameBytes} onChange={setMaximumNameBytes} />
            <label>{copy.collision}<select value={collisionPolicy} onChange={(event) => setCollisionPolicy(event.target.value as typeof collisionPolicy)}><option value="fail">{copy.collisionFail}</option><option value="skip">{copy.collisionSkip}</option><option value="replace">{copy.collisionReplace}</option><option value="suffix">{copy.collisionSuffix}</option></select></label>
          </div>
          <p className="field-hint">{copy.subdirectoryHint}</p>
          <fieldset className="export-options"><legend>{copy.options}</legend><CheckboxInput label={copy.includeContent} value={includeContent} onChange={() => setIncludeContent((value) => !value)} /><CheckboxInput label={copy.includeMetadata} value={includeMetadata} onChange={() => setIncludeMetadata((value) => !value)} /><CheckboxInput label={copy.includeComments} value={includeComments} onChange={() => setIncludeComments((value) => !value)} /></fieldset>
          {isHTML ? <fieldset className="export-options"><legend>{copy.htmlOptions}</legend><label>{copy.resourcePolicy}<select value={htmlResourcePolicy} onChange={(event) => setHTMLResourcePolicy(event.target.value as typeof htmlResourcePolicy)}><option value="best-effort">{copy.resourceBestEffort}</option><option value="strict">{copy.resourceStrict}</option></select></label><TextInput label={copy.batchArchive} value={htmlBatchArchive} onChange={setHTMLBatchArchive} /><p className="field-hint">{copy.batchArchiveHint}</p></fieldset> : null}
          <div className="confirmation-proof"><span>{copy.confirmation}</span><code>{confirmation}</code><p>{copy.confirmationHint}</p></div>
          <div className="export-actions"><Button label={copy.start} variant="primary" isLoading={mutations.startExport.isPending} isDisabled={!directory || !selection || (selection.kind === 'explicit_ids' && ids.length === 0)} onClick={queueExport} /></div>
          {notice ? <p className="export-notice" role="status" aria-live="polite">{notice}{notice.startsWith('Export queued') || notice.startsWith('导出已加入') ? <><br /><span>{copy.queuedHint}</span></> : null}</p> : null}
        </section>
      </div>

      <section className="export-records" aria-labelledby="export-records-title">
        <header><h2 id="export-records-title">{copy.recordsTitle}</h2><p>{copy.recordsDescription}</p></header>
        {records.isLoading ? <p role="status">{copy.loading}</p> : null}
        {records.isError ? <div className="error-state" role="alert"><p>{copy.unavailable}</p><Button label={copy.retry} variant="secondary" onClick={() => void records.refetch()} /></div> : null}
        {!records.isLoading && !records.isError ? <><div className="data-table-wrap" aria-busy={records.isFetching}><table className="data-table"><thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id} scope="col">{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead><tbody>{table.getRowModel().rows.map((row) => <tr key={row.id} data-selected={row.getIsSelected() || undefined}>{row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}{table.getRowModel().rows.length === 0 ? <tr><td colSpan={table.getVisibleLeafColumns().length}>{copy.empty}</td></tr> : null}</tbody></table></div><nav className="pagination" aria-label={copy.pagination}><Button label={copy.previous} variant="secondary" size="sm" isDisabled={pageIndex === 0} onClick={() => setPageIndex((value) => value - 1)} /><span>{copy.page(pageIndex + 1, totalPages)}</span><Button label={copy.next} variant="secondary" size="sm" isDisabled={pageIndex + 1 >= totalPages} onClick={() => setPageIndex((value) => value + 1)} /></nav></> : null}
      </section>

      <section className="workspace-panel export-detail" aria-labelledby="export-detail-title">
        <h2 id="export-detail-title">{copy.detailTitle}</h2><p>{copy.detailDescription}</p>
        <div className="export-actions"><Button label={copy.loadManifest} variant="secondary" isLoading={manifest.isFetching} isDisabled={!selectedID} onClick={() => void manifest.refetch()} /><Button label={copy.verify} variant="secondary" isLoading={mutations.verifyExport.isPending} isDisabled={!selectedID} onClick={runVerification} /></div>
        {!selectedID ? <p className="field-hint">{copy.selectOne}</p> : null}
        {selectedID ? <div className="confirmation-proof"><span>{copy.confirmation}</span><code>{copy.verifyConfirmation(selectedID)}</code><p>{copy.confirmationHint}</p></div> : null}
        {manifest.isLoading ? <p role="status">{copy.manifestLoading}</p> : null}
        {manifest.isError ? <p role="alert">{copy.manifestUnavailable}</p> : null}
        {manifest.data ? <Manifest messages={copy} manifest={manifest.data} /> : null}
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

function Manifest({ messages, manifest }: { readonly messages: MessageCatalog['exports']; readonly manifest: ExportManifest }) {
  return <div className="manifest-detail"><p><strong>{manifest.exportId}</strong> · {manifest.format.toUpperCase()} · {manifest.state} · {formatProvenance(manifest)}</p><p>{messages.manifestSummary(manifest.files.length)}</p>{manifest.files.length ? <div className="data-table-wrap"><table className="data-table"><thead><tr><th scope="col">{messages.fileColumns.path}</th><th scope="col">{messages.fileColumns.size}</th><th scope="col">{messages.fileColumns.status}</th><th scope="col">{messages.fileColumns.checksum}</th><th scope="col">{messages.fileColumns.download}</th></tr></thead><tbody>{manifest.files.map((file) => <tr key={file.artifactId}><td><code>{file.path}</code></td><td>{formatBytes(file.sizeBytes)}</td><td>{file.status}</td><td><code>{file.sha256}</code></td><td><a className="artifact-download" href={getExportArtifactDownloadURL(manifest.exportId, file.artifactId)}>{messages.downloadArtifact}</a></td></tr>)}</tbody></table></div> : <p>{messages.noFiles}</p>}</div>
}

function Verification({ messages, verification }: { readonly messages: MessageCatalog['exports']; readonly verification: ExportVerification }) {
  return <section className="verification-result" aria-live="polite"><h3>{messages.verificationTitle}</h3><p>{verification.valid ? messages.verificationValid(verification.verifiedOutputs) : messages.verificationInvalid(verification.verifiedOutputs)}</p>{verification.issues.length ? <><h4>{messages.verificationIssues}</h4><ul>{verification.issues.map((issue, index) => <li key={`${issue.path ?? 'issue'}-${index}`}><code>{issue.path ?? '—'}</code>: {issue.message ?? JSON.stringify(issue)}</li>)}</ul></> : null}</section>
}

function parseArticleIDs(value: string) { return [...new Set(value.split(/[\n,]/).map((id) => id.trim()).filter(Boolean))] }
function formatDate(value: string, locale: Locale) { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date) }
function formatBytes(value: number) {
  if (value < 1_024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let amount = value / 1_024
  let unit = 0
  while (amount >= 1_024 && unit < units.length - 1) { amount /= 1_024; unit += 1 }
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(amount)} ${units[unit]}`
}
function formatProvenance(value: { readonly provenanceState?: string; readonly provenanceGeneration: number }) { return `${value.provenanceState ?? '—'} · #${value.provenanceGeneration}` }
function State({ value, locale }: { readonly value: string; readonly locale: Locale }) { const labels = locale === 'zh-CN' ? { queued: '已排队', running: '运行中', completed: '已完成', failed: '失败', cancelled: '已取消' } : { queued: 'Queued', running: 'Running', completed: 'Completed', failed: 'Failed', cancelled: 'Cancelled' }; const label = labels[value as keyof typeof labels] ?? value; const variant = value === 'completed' ? 'success' : value === 'failed' || value === 'cancelled' ? 'error' : value === 'running' ? 'warning' : 'neutral'; return <span className="article-status"><StatusDot variant={variant} label={label} />{label}</span> }
