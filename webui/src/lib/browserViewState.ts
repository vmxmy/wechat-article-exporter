import { parseArticleQuery, type ArticleOption, type ArticleQuery, type ArticleSort, type ExportFormat, type ExportSelection } from './api'

export const defaultArticleSort: ArticleSort = { field: 'publishedAt', direction: 'desc' }
export const defaultExportFormat: ExportFormat = 'markdown'
export const exportBrowserDraftStorageKey = 'wechat-article.export-browser-draft.v2'

export type ExportStage = 'scope' | 'format' | 'destination'
export type ExportScopeType = 'articles' | 'albums' | 'account' | 'album' | 'savedQuery' | 'matching'

export interface ArticleBrowserView {
  readonly query: ArticleQuery
  readonly sort: ArticleSort
  readonly page: number
}

export interface ExportBrowserView {
  readonly workflow?: string
  readonly stage: ExportStage
  readonly scope: ExportScopeType
  readonly format: ExportFormat
}

export interface ExportBrowserDraftOptions {
  readonly namingTemplate: string
  readonly maximumNameBytes: number
  readonly collisionPolicy: 'fail' | 'skip' | 'replace' | 'suffix'
  readonly includeContent: boolean
  readonly includeMetadata: boolean
  readonly includeComments: boolean
  readonly htmlResourcePolicy: 'best-effort' | 'strict'
}

export interface ExportBrowserDraft {
  readonly selection?: ExportSelection
  readonly selectionLabel?: string
  readonly selectedArticles?: readonly ArticleOption[]
  readonly options?: ExportBrowserDraftOptions
}

interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

const articleStringFields = ['accountId', 'albumId', 'keyword', 'author', 'state', 'publishedFrom', 'publishedTo'] as const
const articleBooleanFields = ['deleted', 'hasContent', 'hasComments', 'original', 'paid'] as const
const articleNumberFields = ['readMin', 'readMax', 'oldLikeMin', 'oldLikeMax', 'shareMin', 'shareMax', 'likeMin', 'likeMax', 'commentMin', 'commentMax', 'weCoinMin', 'weCoinMax', 'mediaSecondsMin', 'mediaSecondsMax'] as const
const articleRangeFields = [['readMin', 'readMax'], ['oldLikeMin', 'oldLikeMax'], ['shareMin', 'shareMax'], ['likeMin', 'likeMax'], ['commentMin', 'commentMax'], ['weCoinMin', 'weCoinMax'], ['mediaSecondsMin', 'mediaSecondsMax']] as const
const articleOwnedParameters = new Set([...articleStringFields, ...articleBooleanFields, ...articleNumberFields, 'messageType', 'sort', 'page', 'dialog', 'selection'])
const articleSortFields = new Set(['title', 'publishedAt', 'state'])
const articleStates = new Set(['ready', 'queued', 'failed'])
const exportStages = new Set<ExportStage>(['scope', 'format', 'destination'])
const exportScopeTypes = new Set<ExportScopeType>(['articles', 'albums', 'account', 'album', 'savedQuery', 'matching'])
const exportFormats = new Set<ExportFormat>(['markdown', 'html', 'text', 'json', 'xlsx', 'docx', 'pdf'])
const maximumTextLength = 512
const maximumPage = 4_001
const maximumDraftIDs = 250
const maximumDraftBytes = 64 * 1024
const workflowPattern = /^[a-f0-9]{32}$/
const defaultDraftOptions: ExportBrowserDraftOptions = {
  namingTemplate: '{published}-{title}', maximumNameBytes: 180, collisionPolicy: 'fail',
  includeContent: true, includeMetadata: true, includeComments: false, htmlResourcePolicy: 'best-effort'
}

export interface PagedBrowserView {
  readonly page: number
}

const pagedOwnedParameters = new Set(['page'])

export function parsePagedBrowserView(search: string): { readonly state: PagedBrowserView; readonly canonicalSearch: string; readonly needsReplace: boolean } {
  const source = new URLSearchParams(normalizeSearch(search))
  const page = parsePositiveInteger(oneValue(source, 'page'), maximumPage) ?? 1
  const state: PagedBrowserView = { page }
  const canonicalSearch = serializePagedBrowserView(state, search)
  return { state, canonicalSearch, needsReplace: canonicalSearch !== normalizeSearch(search) }
}

export function serializePagedBrowserView(state: PagedBrowserView, currentSearch = ''): string {
  const params = foreignParametersFor(currentSearch, pagedOwnedParameters)
  if (state.page > 1 && state.page <= maximumPage) params.set('page', String(state.page))
  return searchFrom(params)
}

export function parseArticleBrowserView(search: string): { readonly state: ArticleBrowserView; readonly canonicalSearch: string; readonly needsReplace: boolean } {
  const source = new URLSearchParams(normalizeSearch(search))
  const query: Record<string, unknown> = {}

  for (const field of articleStringFields) {
    const value = oneValue(source, field)
    if (value && value.length <= maximumTextLength && (field !== 'state' || articleStates.has(value))) query[field] = value
  }
  for (const field of articleBooleanFields) {
    const value = oneValue(source, field)
    if (value === 'true' || value === 'false') query[field] = value === 'true'
  }
  for (const field of articleNumberFields) {
    const value = parseNonNegativeInteger(oneValue(source, field))
    if (value !== undefined) query[field] = value
  }

  const messageTypes = [...new Set(source.getAll('messageType').map(parseNonNegativeInteger).filter((value): value is number => value !== undefined))].sort((left, right) => left - right)
  if (messageTypes.length > 0) query.messageTypes = messageTypes

  if (!validDate(query.publishedFrom)) delete query.publishedFrom
  if (!validDate(query.publishedTo)) delete query.publishedTo
  if (typeof query.publishedFrom === 'string' && typeof query.publishedTo === 'string' && Date.parse(query.publishedFrom) > Date.parse(query.publishedTo)) {
    delete query.publishedFrom
    delete query.publishedTo
  }
  for (const [minimum, maximum] of articleRangeFields) {
    if (typeof query[minimum] === 'number' && typeof query[maximum] === 'number' && (query[minimum] as number) > (query[maximum] as number)) {
      delete query[minimum]
      delete query[maximum]
    }
  }

  const sort = parseArticleSort(oneValue(source, 'sort'))
  const page = parsePositiveInteger(oneValue(source, 'page'), maximumPage) ?? 1
  const state: ArticleBrowserView = { query: query as ArticleQuery, sort, page }
  const canonicalSearch = serializeArticleBrowserView(state, search)
  return { state, canonicalSearch, needsReplace: canonicalSearch !== normalizeSearch(search) }
}

export function serializeArticleBrowserView(state: ArticleBrowserView, currentSearch = ''): string {
  const params = foreignArticleParameters(currentSearch)
  for (const field of articleStringFields) {
    const value = state.query[field]?.trim()
    if (value) params.set(field, value)
  }
  for (const field of articleBooleanFields) if (state.query[field] !== undefined) params.set(field, String(state.query[field]))
  for (const field of articleNumberFields) {
    const value = state.query[field]
    if (typeof value === 'number' && Number.isInteger(value) && value >= 0) params.set(field, String(value))
  }
  for (const value of [...new Set(state.query.messageTypes ?? [])].filter((item) => Number.isInteger(item) && item >= 0).sort((left, right) => left - right)) params.append('messageType', String(value))
  if (!sameSort(state.sort, defaultArticleSort) && articleSortFields.has(state.sort.field)) params.set('sort', `${state.sort.field}:${state.sort.direction}`)
  if (state.page > 1 && state.page <= maximumPage) params.set('page', String(state.page))
  return searchFrom(params)
}

export function parseExportBrowserView(search: string): { readonly state: ExportBrowserView; readonly canonicalSearch: string; readonly needsReplace: boolean; readonly specified: { readonly stage: boolean; readonly scope: boolean; readonly format: boolean } } {
  const params = new URLSearchParams(normalizeSearch(search))
  const rawWorkflow = oneValue(params, 'flow')
  const rawStage = oneValue(params, 'stage')
  const rawScope = oneValue(params, 'scope')
  const rawFormat = oneValue(params, 'format')
  const state: ExportBrowserView = {
    ...(rawWorkflow && workflowPattern.test(rawWorkflow) ? { workflow: rawWorkflow } : {}),
    stage: isMember(exportStages, rawStage) ? rawStage : 'scope',
    scope: isMember(exportScopeTypes, rawScope) ? rawScope : 'articles',
    format: isMember(exportFormats, rawFormat) ? rawFormat : defaultExportFormat
  }
  const canonicalSearch = serializeExportBrowserView(state)
  return { state, canonicalSearch, needsReplace: canonicalSearch !== normalizeSearch(search), specified: { stage: params.has('stage'), scope: params.has('scope'), format: params.has('format') } }
}

export function serializeExportBrowserView(state: ExportBrowserView, currentSearch = ''): string {
  void currentSearch
  const params = new URLSearchParams()
  if (state.workflow && workflowPattern.test(state.workflow)) params.set('flow', state.workflow)
  if (state.stage !== 'scope') params.set('stage', state.stage)
  if (state.scope !== 'articles') params.set('scope', state.scope)
  if (state.format !== defaultExportFormat) params.set('format', state.format)
  return searchFrom(params)
}

export function earliestValidExportStage(requested: ExportStage, hasScope: boolean, hasValidFormat: boolean): ExportStage {
  if (requested === 'scope' || !hasScope) return 'scope'
  if (requested === 'format' || !hasValidFormat) return 'format'
  return 'destination'
}

export function createExportWorkflowID(randomUUID = defaultRandomUUID): string {
  return randomUUID().replace(/-/g, '')
}

export function saveExportBrowserDraft(workflow: string, draft: ExportBrowserDraft, storage = getSessionStorage()): void {
  if (!storage || !workflowPattern.test(workflow)) return
  const safeDraft = projectExportDraft(draft)
  try {
    const serialized = JSON.stringify({ version: 1, ...safeDraft })
    if (encodedByteLength(serialized) <= maximumDraftBytes) storage.setItem(exportDraftKey(workflow), serialized)
    else storage.removeItem(exportDraftKey(workflow))
  } catch { /* Browser storage can be unavailable. */ }
}

export function loadExportBrowserDraft(workflow: string | undefined, storage = getSessionStorage()): ExportBrowserDraft | undefined {
  if (!storage || !workflow || !workflowPattern.test(workflow)) return undefined
  try {
    const raw = storage.getItem(exportDraftKey(workflow))
    if (!raw || encodedByteLength(raw) > maximumDraftBytes) return undefined
    const value = JSON.parse(raw) as Record<string, unknown>
    if (value.version !== 1) return undefined
    const draft = projectExportDraft(value)
    return Object.keys(draft).length > 0 ? draft : undefined
  } catch { return undefined }
}

export function clearExportBrowserDraft(workflow: string | undefined, storage = getSessionStorage()): void {
  if (!storage || !workflow || !workflowPattern.test(workflow)) return
  try { storage.removeItem(exportDraftKey(workflow)) } catch { /* Browser storage can be unavailable. */ }
}

function projectExportDraft(value: unknown): ExportBrowserDraft {
  if (!value || Array.isArray(value) || typeof value !== 'object') return {}
  const input = value as Record<string, unknown>
  const selection = projectSelection(input.selection)
  const selectionLabel = safeDisplayText(input.selectionLabel)
  const selectedArticles = selection?.kind === 'explicit_ids' ? projectArticles(input.selectedArticles, selection.articleIds) : undefined
  const options = projectOptions(input.options)
  return {
    ...(selection ? { selection } : {}),
    ...(selection && selectionLabel ? { selectionLabel } : {}),
    ...(selectedArticles ? { selectedArticles } : {}),
    ...(options ? { options } : {})
  }
}

function projectSelection(value: unknown): ExportSelection | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const input = value as Record<string, unknown>
  switch (input.kind) {
    case 'explicit_ids': {
      const articleIds = safeIDs(input.articleIds)
      return articleIds ? { kind: 'explicit_ids', articleIds } : undefined
    }
    case 'account': return safeID(input.accountId) ? { kind: 'account', accountId: input.accountId as string } : undefined
    case 'album': return safeID(input.albumId) ? { kind: 'album', albumId: input.albumId as string } : undefined
    case 'album_ids': {
      const albumIds = safeIDs(input.albumIds)
      return albumIds ? { kind: 'album_ids', albumIds } : undefined
    }
    case 'saved_query': return safeID(input.savedQueryId) ? { kind: 'saved_query', savedQueryId: input.savedQueryId as string } : undefined
    case 'all_matching': {
      const query = projectArticleQuery(input.query)
      return query ? { kind: 'all_matching', query } : undefined
    }
    default: return undefined
  }
}

function projectOptions(value: unknown): ExportBrowserDraftOptions | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const input = value as Record<string, unknown>
  const namingTemplate = safeTemplate(input.namingTemplate) ?? defaultDraftOptions.namingTemplate
  const maximumNameBytes = typeof input.maximumNameBytes === 'number' && Number.isInteger(input.maximumNameBytes) && input.maximumNameBytes > 0 && input.maximumNameBytes <= 4_096 ? input.maximumNameBytes : defaultDraftOptions.maximumNameBytes
  const collisionPolicy = ['fail', 'skip', 'replace', 'suffix'].includes(String(input.collisionPolicy)) ? input.collisionPolicy as ExportBrowserDraftOptions['collisionPolicy'] : defaultDraftOptions.collisionPolicy
  const htmlResourcePolicy = input.htmlResourcePolicy === 'strict' ? 'strict' : 'best-effort'
  return {
    namingTemplate, maximumNameBytes, collisionPolicy,
    includeContent: typeof input.includeContent === 'boolean' ? input.includeContent : defaultDraftOptions.includeContent,
    includeMetadata: typeof input.includeMetadata === 'boolean' ? input.includeMetadata : defaultDraftOptions.includeMetadata,
    includeComments: typeof input.includeComments === 'boolean' ? input.includeComments : defaultDraftOptions.includeComments,
    htmlResourcePolicy
  }
}

function projectArticles(value: unknown, ids: readonly string[]): readonly ArticleOption[] | undefined {
  if (!Array.isArray(value) || value.length !== ids.length) return undefined
  const articles = value.map((item, index) => {
    if (!item || Array.isArray(item) || typeof item !== 'object') return undefined
    const input = item as Record<string, unknown>
    const title = safeDisplayText(input.title)
    if (!title) return undefined
    const accountName = safeDisplayText(input.accountName)
    return { id: ids[index], title, accountNameAvailable: Boolean(accountName), ...(accountName ? { accountName } : {}) } satisfies ArticleOption
  })
  return articles.every((article): article is ArticleOption => article !== undefined) ? articles : undefined
}

function projectArticleQuery(value: unknown): ArticleQuery | undefined {
  try { return parseArticleQuery(value) } catch { return undefined }
}

function foreignArticleParameters(search: string): URLSearchParams {
  return foreignParametersFor(search, articleOwnedParameters)
}

function foreignParametersFor(search: string, owned: ReadonlySet<string>): URLSearchParams {
  const source = new URLSearchParams(normalizeSearch(search))
  const params = new URLSearchParams()
  for (const [key, value] of source) if (!owned.has(key)) params.append(key, value)
  return params
}

function parseArticleSort(value: string | undefined): ArticleSort {
  if (!value) return defaultArticleSort
  const [field, direction, extra] = value.split(':')
  if (extra !== undefined || !articleSortFields.has(field) || (direction !== 'asc' && direction !== 'desc')) return defaultArticleSort
  return { field, direction }
}

function validDate(value: unknown): boolean { return value === undefined || (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d{1,9})?)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) && !Number.isNaN(Date.parse(value))) }
function sameSort(left: ArticleSort, right: ArticleSort): boolean { return left.field === right.field && left.direction === right.direction }
function normalizeSearch(search: string): string { return search && search !== '?' ? (search.startsWith('?') ? search : `?${search}`) : '' }
function searchFrom(params: URLSearchParams): string { const value = params.toString(); return value ? `?${value}` : '' }
function oneValue(params: URLSearchParams, key: string): string | undefined { const values = params.getAll(key); return values.length === 1 ? values[0].trim() || undefined : undefined }
function parseNonNegativeInteger(value: string | undefined): number | undefined { if (!value || !/^\d+$/.test(value)) return undefined; const parsed = Number(value); return Number.isSafeInteger(parsed) ? parsed : undefined }
function parsePositiveInteger(value: string | undefined, maximum: number): number | undefined { const parsed = parseNonNegativeInteger(value); return parsed !== undefined && parsed > 0 && parsed <= maximum ? parsed : undefined }
function isMember<Value extends string>(values: ReadonlySet<Value>, value: string | undefined): value is Value { return value !== undefined && values.has(value as Value) }
function safeID(value: unknown): value is string { return typeof value === 'string' && value.trim().length > 0 && value.length <= maximumTextLength }
function safeIDs(value: unknown): readonly string[] | undefined { if (!Array.isArray(value) || value.length === 0 || value.length > maximumDraftIDs || value.some((item) => !safeID(item))) return undefined; return [...new Set(value as string[])] }
function safeDisplayText(value: unknown): string | undefined { if (typeof value !== 'string') return undefined; const text = value.trim(); return text && text.length <= maximumTextLength && !looksLikePath(text) ? text : undefined }
function safeTemplate(value: unknown): string | undefined { return typeof value === 'string' && value.trim() && value.length <= maximumTextLength && !looksLikePath(value) ? value.trim() : undefined }
function looksLikePath(value: string): boolean { return value.includes('/') || /^[A-Za-z]:[\\/]/.test(value) || value.includes('\\') }
function encodedByteLength(value: string): number { return new TextEncoder().encode(value).byteLength }
function exportDraftKey(workflow: string): string { return `${exportBrowserDraftStorageKey}.${workflow}` }
function defaultRandomUUID(): string { return crypto.randomUUID() }
function getSessionStorage(): StorageLike | undefined { if (typeof window === 'undefined') return undefined; try { return window.sessionStorage } catch { return undefined } }
