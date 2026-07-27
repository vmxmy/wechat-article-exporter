export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const apiBase = '/api/v1'

export interface Pagination {
  readonly page: number
  readonly pageSize: number
  readonly total: number
}

export interface PaginatedResponse<T> {
  readonly data: readonly T[]
  readonly pagination: Pagination
}

interface ApiEnvelope<T> {
  readonly apiVersion: string
  readonly data: T
}

export interface WorkspacePageResponse<T> {
  readonly items: readonly T[]
  readonly total: number
  readonly offset: number
  readonly limit: number
}

export interface PageParams {
  readonly page: number
  readonly pageSize: number
  readonly search?: string
  readonly sort?: string
  readonly direction?: 'asc' | 'desc'
}

/**
 * Bounded numbered pagination shared by local selector endpoints. Selector
 * values are intentionally small browser-safe projections rather than the
 * full account or album resource records.
 */
export interface SelectorPageParams {
  readonly page: number
  readonly pageSize: number
  readonly search?: string
}

export interface AlbumSelectorPageParams extends SelectorPageParams {
  readonly accountId?: string
}

export type ArticleSelectorPageParams = SelectorPageParams

export interface AlbumPageParams {
  readonly page: number
  readonly pageSize: number
  readonly accountId?: string
  readonly keyword?: string
}

export type AlbumTraversalOrder = 'forward' | 'reverse'

export interface RuntimeStatus {
  readonly version?: string
  readonly profileId?: string
  readonly profile?: string
  readonly session?: 'authenticated' | 'unauthenticated'
  readonly portable?: boolean
  readonly offlineReady?: boolean
  readonly secretBackend?: string
  readonly checkedAt?: string
  readonly storage?: StorageStatus
}

export interface SessionStatus {
  readonly state: string
  readonly accountId?: string
  readonly accountName?: string
  readonly avatarUrl?: string
  readonly createdAt?: string
  readonly expiresAt?: string
  readonly lastValidatedAt?: string
  readonly validation?: string
}

export interface SwitchableAccount {
  readonly id: string
  readonly name: string
  readonly alias?: string
}

export interface SwitchableAccounts {
  readonly available: boolean
  readonly accounts: readonly SwitchableAccount[]
}

export interface AccountRecord {
  readonly id: string
  readonly fakeid?: string
  readonly name: string
  readonly alias?: string
  readonly description?: string
  readonly articleCount?: number
  readonly messageCount?: number
  readonly upstreamTotal?: number
  readonly syncCursor?: number
  readonly lastSyncAt?: string
  readonly syncCompleted?: boolean
}

/** Safe, bounded projection returned by GET /selectors/accounts. */
export interface AccountOption {
  readonly id: string
  readonly displayName?: string
  readonly displayNameAvailable: boolean
  readonly alias?: string
}

export type AccountSyncMode = 'incremental' | 'full'

export interface ArticleRecord {
  readonly id: string
  readonly title: string
  readonly accountId?: string
  readonly accountName?: string
  readonly accountNameAvailable: boolean
  readonly author?: string
  readonly publishedAt: string | null
  readonly state?: string
  readonly status?: string
  readonly hasContent?: boolean
  readonly hasComments?: boolean
}

/** Safe, bounded projection returned by GET /selectors/articles. */
export interface ArticleOption {
  readonly id: string
  readonly title: string
  readonly accountName?: string
  readonly accountNameAvailable: boolean
}

export type ArticleDownloadKind = 'article' | 'metadata' | 'comments' | 'resources'
export interface ArticlePreview { readonly articleId: string; readonly title: string; readonly available: boolean; readonly documentUrl?: string }
export interface ArticleResourceSummary {
  readonly articleId: string
  readonly total: number
  readonly available: number
  readonly missing: number
  readonly complete: boolean
}
export interface ArticleMetrics {
  readonly available: boolean
  readonly readCount: number
  readonly oldLikeCount: number
  readonly likeCount: number
  readonly shareCount: number
  readonly commentCount: number
  readonly capturedAt?: string
}
export interface ArticleResourceDetail {
  readonly role: string
  readonly ordinal: number
  readonly available: boolean
}
export interface ArticleDetail {
  readonly articleId: string
  readonly metrics: ArticleMetrics
  readonly resources: WorkspacePageResponse<ArticleResourceDetail>
}
export interface ArticleComment {
  readonly id: string
  readonly authorName: string
  readonly content: string
  readonly createdAt?: string
  readonly likeCount: number
  readonly replyCount: number
  readonly replyStatus: 'complete' | 'pending'
}
export interface ArticleReply {
  readonly id: string
  readonly authorName: string
  readonly content: string
  readonly createdAt?: string
  readonly likeCount: number
}
export interface ArticleComments {
  readonly articleId: string
  readonly comments: WorkspacePageResponse<ArticleComment>
  readonly pendingReplies: number
}

export interface AlbumRecord {
  readonly id: string
  readonly accountId?: string
  readonly accountName?: string
  readonly accountNameAvailable: boolean
  readonly name: string
  readonly description?: string
  readonly articleCount: number
  readonly paid?: boolean
}

/** Safe, bounded projection returned by GET /selectors/albums. */
export interface AlbumOption {
  readonly id: string
  readonly accountId?: string
  readonly displayName?: string
  readonly displayNameAvailable: boolean
  readonly accountName?: string
  readonly accountNameAvailable: boolean
}

export interface JobRecord {
  readonly id: string
  readonly kind: string
  readonly label: string
  readonly state: string
  readonly profile?: string
  readonly createdAt: string
  readonly updatedAt: string
  readonly counts?: Readonly<Record<string, number>>
  readonly permittedActions: readonly JobControlAction[]
}

export type JobControlAction = 'cancel' | 'pause' | 'resume' | 'retry'

export interface JobItemDetail {
  readonly id: string
  readonly state: string
  readonly attemptCount: number
  readonly errorClass?: string
  readonly createdAt: string
  readonly updatedAt: string
}

export interface JobLogDetail {
  readonly id: number
  readonly itemId?: string
  readonly level: string
  readonly message: string
  readonly createdAt: string
}

export interface JobLeaseDetail {
  readonly active: boolean
  readonly expiresAt?: string
}

export interface JobDetail {
  readonly job: JobRecord
  readonly items: readonly JobItemDetail[]
  readonly itemsTotal: number
  readonly itemsLimited: boolean
  readonly logs: readonly JobLogDetail[]
  readonly lease: JobLeaseDetail
  readonly refreshedAt: string
}

export interface StorageStatus {
  readonly databaseAvailable: boolean
  readonly objectStoreReady: boolean
  readonly accounts: number
  readonly articles: number
  readonly albums: number
  readonly jobs: number
  readonly objects: number
  readonly objectBytes: number
}

export interface ArticleSort {
  readonly field: string
  readonly direction: 'asc' | 'desc'
}

export interface ArticleQuery {
  readonly accountId?: string
  readonly albumId?: string
  readonly keyword?: string
  readonly author?: string
  readonly state?: string
  readonly publishedFrom?: string
  readonly publishedTo?: string
  readonly deleted?: boolean
  readonly hasContent?: boolean
  readonly hasComments?: boolean
  readonly original?: boolean
  readonly paid?: boolean
  readonly messageTypes?: readonly number[]
  readonly readMin?: number
  readonly readMax?: number
  readonly oldLikeMin?: number
  readonly oldLikeMax?: number
  readonly shareMin?: number
  readonly shareMax?: number
  readonly likeMin?: number
  readonly likeMax?: number
  readonly commentMin?: number
  readonly commentMax?: number
  readonly weCoinMin?: number
  readonly weCoinMax?: number
  readonly mediaSecondsMin?: number
  readonly mediaSecondsMax?: number
  readonly sorts?: readonly ArticleSort[]
}

export interface SavedQueryRecord {
  readonly name: string
  readonly query: ArticleQuery
  readonly createdAt: string
  readonly updatedAt: string
}

export interface SavedQueryInput {
  readonly name: string
  readonly query: ArticleQuery
}

export type ExportFormat = 'html' | 'markdown' | 'text' | 'json' | 'xlsx' | 'docx' | 'pdf'

export interface ExportDirectory {
  readonly token: string
  readonly label: string
  readonly isDefault?: boolean
  readonly createdAt?: string
  readonly description?: string
}

export type ExportSelection =
  | { readonly kind: 'explicit_ids'; readonly articleIds: readonly string[] }
  | { readonly kind: 'account'; readonly accountId: string }
  | { readonly kind: 'album'; readonly albumId: string }
  | { readonly kind: 'album_ids'; readonly albumIds: readonly string[] }
  | { readonly kind: 'saved_query'; readonly savedQueryId: string; readonly query?: ArticleQuery }
  | { readonly kind: 'all_matching'; readonly query: ArticleQuery }

export interface ExplicitIDExportSelection {
  readonly kind: 'explicit_ids'
  readonly articleIds: readonly string[]
}

export interface ExportOptions {
  readonly namingTemplate?: string
  readonly maximumNameBytes?: number
  readonly collisionPolicy?: 'fail' | 'skip' | 'replace' | 'suffix'
  readonly formatOptions?: Readonly<Record<string, boolean | string>>
}

export interface StartExportInput {
  readonly directoryToken: string
  readonly subdirectory?: string
  readonly selection: ExportSelection
  readonly format: ExportFormat
  readonly options?: ExportOptions
}

export interface ExportStartResult { readonly jobId: string }

export interface ExportRecord {
  readonly id: string
  readonly jobId: string
  readonly format: string
  readonly state: string
  readonly createdAt: string
  readonly completedAt?: string
  readonly provenanceState?: string
  readonly provenanceGeneration: number
  readonly outputDirectory: string
}

export interface ExportFile {
  readonly artifactId: string
  readonly articleId?: string
  readonly path: string
  readonly sizeBytes: number
  readonly sha256: string
  readonly mediaType?: string
  readonly status: string
}

export interface ExportManifest {
  readonly exportId: string
  readonly format: string
  readonly state: string
  readonly provenanceState?: string
  readonly provenanceGeneration: number
  readonly files: readonly ExportFile[]
}

export interface ExportVerificationIssue {
  readonly path?: string
  readonly message?: string
  readonly expected?: string
  readonly actual?: string
  readonly [key: string]: unknown
}

export interface ExportVerification {
  readonly exportId: string
  readonly valid: boolean
  readonly verifiedOutputs: number
  readonly issues: readonly ExportVerificationIssue[]
  readonly affectedArticleIds?: readonly string[]
}

export interface WorkspaceSnapshot {
  readonly runtime: RuntimeStatus
  readonly session: SessionStatus
  readonly storage: StorageStatus
  readonly jobs?: PaginatedResponse<JobRecord>
  /** Observation metadata only; state remains authoritative in local SQLite. */
  readonly observedAt: string
  /** Monotonic within one local workspace process; increments on semantic changes. */
  readonly revision: number
}

export interface LoginFlow { readonly sessionId: string; readonly qrCode?: string; readonly expiresAt?: string }
export interface LoginPollResult { readonly state: string; readonly accountCount: number }
export interface AccountInput { readonly fakeid: string; readonly name: string; readonly alias?: string; readonly description?: string }
export interface AccountManifestImportReport { readonly added: number; readonly merged: number; readonly unchanged: number }
export interface AccountManifestImportResult { readonly report: AccountManifestImportReport }

// Maintenance responses intentionally mirror the browser-safe DTO boundary.
// Secret values are accepted by write-only inputs only and never modelled here.
export interface CredentialMetadata {
  readonly id: string
  readonly accountId: string
  readonly accountName?: string
  readonly accountNameAvailable: boolean
  readonly kind: string
  readonly status: string
  readonly validatedAt?: string
  readonly createdAt: string
  readonly updatedAt: string
}

export interface CredentialImportInput {
  readonly nickname?: string
  readonly biz?: string
  readonly uin?: string
  readonly key?: string
  readonly passTicket?: string
  readonly wapSid2?: string
  readonly appMsgToken?: string
  readonly cookie?: string
  readonly expiresAt?: string
}

// Credential validation is a non-persistent, write-only check. The response
// carries no credential fields, file metadata, local paths, or session data.
export interface CredentialValidation { readonly valid: boolean; readonly status: string }

export type ProxyTrust = 'public-only' | 'credential-trusted'
export type ProxyRequestClass = 'public_content' | 'public_resource' | 'management_session' | 'article_credential' | 'engagement_metrics' | 'comments' | 'paid_content'

export interface ProxyHealth {
  readonly state: string
  readonly consecutiveFailures: number
  readonly cooldownUntil?: string
  readonly lastSampleAt?: string
  readonly lastSuccessAt?: string
  readonly lastLatency?: number
  readonly lastStatusCode?: number
  readonly lastErrorClass?: string
}

export interface ProxyRoute {
  readonly id: string
  readonly name: string
  readonly endpoint: string
  readonly authorizationConfigured: boolean
  readonly trust: ProxyTrust
  readonly classes: readonly ProxyRequestClass[]
  readonly priority: number
  readonly enabled: boolean
  readonly health: ProxyHealth
  readonly createdAt: string
  readonly updatedAt: string
}

export interface ProxyInput {
  readonly name: string
  readonly endpoint: string
  readonly authorization?: string
  readonly trust: ProxyTrust
  readonly classes: readonly ProxyRequestClass[]
  readonly priority: number
  readonly confirm?: string
}

export interface ProxyDisclosure { readonly required: boolean; readonly confirmation?: string; readonly secrets?: readonly string[] }
export interface ProxyProbeResult { readonly route: ProxyRoute; readonly latency: number; readonly statusCode?: number; readonly responseValid: boolean; readonly credentialEligible: boolean; readonly errorClass?: string }

export interface Preferences {
  readonly sync: { readonly range: string; readonly datePoint?: string; readonly pageDelay: number; readonly jitter: number; readonly pageSize: number; readonly incremental: boolean; readonly unsafePacingSaved: boolean }
  readonly download: { readonly concurrency: number; readonly forceContent: boolean; readonly metadataOverridesContent: boolean }
  readonly export: { readonly namingTemplate: string; readonly maximumNameBytes: number; readonly collisionPolicy: string; readonly excelIncludeContent: boolean; readonly jsonIncludeContent: boolean; readonly jsonIncludeComments: boolean; readonly htmlIncludeComments: boolean }
  readonly display: { readonly noColor: boolean; readonly ascii: boolean; readonly plain: boolean; readonly hideDeleted: boolean; readonly language?: string }
  readonly proxy: { readonly directFirst: boolean; readonly fallbackEnabled: boolean }
}

export type PreferencesPatch = Partial<Preferences>
export interface BackupReceipt { readonly id: string; readonly createdAt: string; readonly sha256: string; readonly bytes: number; readonly objects: number; readonly omitted?: readonly string[] }
export interface BackupVerification { readonly backupId: string; readonly valid: boolean; readonly sha256?: string; readonly failures?: readonly string[] }
export type RestoreConflictPolicy = 'refuse' | 'rename'
export interface RestoreUploadReceipt { readonly handle: string; readonly sizeBytes: number; readonly sha256: string; readonly expiresAt: string }
export interface RestorePreparation { readonly id: string; readonly confirmation: string; readonly conflictPolicy: RestoreConflictPolicy; readonly expiresAt: string }
export interface RestoreCompletion { readonly restoredFiles: number; readonly restoredBytes: number; readonly profiles: number }
export interface IntegrityIssue { readonly kind: string; readonly articleId?: string; readonly resourceId?: string; readonly objectDigest?: string; readonly message: string; readonly repairable: boolean; readonly recommendation?: string }
export interface IntegrityReport { readonly checkedAt: string; readonly issues: readonly IntegrityIssue[] }
export interface ReclaimableStorage { readonly count: number; readonly bytes: number }
export interface GarbageCollectionPlan { readonly id: string; readonly generatedAt: string; readonly expiresAt?: string; readonly unreferencedObjects: ReclaimableStorage; readonly temporaryFiles: ReclaimableStorage; readonly expiredDebugCaptures: ReclaimableStorage; readonly completedJobLogs: ReclaimableStorage; readonly confirmation: string }
export interface GarbageCollectionResult { readonly deletedObjects: ReclaimableStorage; readonly deletedTemporaryFiles: ReclaimableStorage; readonly deletedDebugCaptures: ReclaimableStorage; readonly deletedCompletedJobLogs: ReclaimableStorage; readonly skipped: number }
export interface DiagnosticCheck { readonly name: string; readonly status: string; readonly summary?: string }
export interface DiagnosticsReport { readonly collectedAt: string; readonly checks: readonly DiagnosticCheck[] }
export interface DiagnosticBundleReceipt { readonly handle: string; readonly createdAt: string; readonly expiresAt: string; readonly sha256: string; readonly sizeBytes: number }

export interface ArticlePageParams extends PageParams, ArticleQuery {}

/**
 * Browser-local display metadata accompanying an export action contract. It
 * deliberately carries no identifiers: stable IDs and validated queries stay
 * exclusively in ExportSelection, while this shape lets the next route render
 * a human-readable scope without re-querying arbitrary selector pages.
 */
export interface ExportHandoffPresentation {
  readonly articles?: readonly ExportHandoffArticlePresentation[]
  readonly matching?: ExportHandoffMatchingPresentation
}

export interface ExportHandoffArticlePresentation {
  readonly title: string
  readonly accountName?: string
}

export interface ExportHandoffMatchingPresentation {
  readonly total: number
  readonly accountName?: string
  readonly albumName?: string
}

export interface ExportHandoff {
  readonly selection: ExportSelection
  readonly label: string
  readonly presentation?: ExportHandoffPresentation
}

const exportHandoffStorageKey = 'wechat-article.export-handoff.v1'
let pendingExportHandoffForStrictMount: ExportHandoff | undefined
let hasPendingExportHandoffForStrictMount = false
const articleQueryHandoffStorageKey = 'wechat-article.article-query-handoff.v1'

export async function getRuntimeStatus(signal?: AbortSignal): Promise<RuntimeStatus> {
  return request<RuntimeStatus>(`${apiBase}/runtime`, { signal })
}

export async function getSessionStatus(signal?: AbortSignal): Promise<SessionStatus> {
  return request<SessionStatus>(`${apiBase}/session`, { signal })
}

export async function getSwitchableAccounts(signal?: AbortSignal): Promise<SwitchableAccounts> {
  return request<SwitchableAccounts>(`${apiBase}/session/accounts`, { signal })
}

export async function getStorageStatus(signal?: AbortSignal): Promise<StorageStatus> {
  return request<StorageStatus>(`${apiBase}/storage`, { signal })
}

export async function getWorkspaceSnapshot(signal?: AbortSignal): Promise<WorkspaceSnapshot> {
  return request<WorkspaceSnapshot>(`${apiBase}/events/snapshot`, { signal })
}

export async function getCSRFToken(): Promise<string> {
  const status = await request<{ readonly csrfToken: string }>(`${apiBase}/status`, {})
  return status.csrfToken
}

export async function beginLogin(sessionId: string): Promise<LoginFlow> { return mutate<LoginFlow>('login/begin', 'POST', { sessionId }) }
export async function pollLogin(): Promise<LoginPollResult> { return mutate<LoginPollResult>('login/poll', 'POST', {}) }
export async function completeLogin(): Promise<SessionStatus> { return mutate<SessionStatus>('login/complete', 'POST', {}) }
export async function logout(): Promise<void> { await mutate<void>('session/logout', 'POST', {}) }
export async function switchAccount(id: string): Promise<SessionStatus> {
  return mutate<SessionStatus>(`session/accounts/${encodeURIComponent(id)}/switch`, 'POST', {})
}
export async function searchAccounts(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<AccountRecord>> { return getPage<AccountRecord>('accounts/search', params, signal) }

export async function resolveAccountFromArticle(articleURL: string, signal?: AbortSignal): Promise<AccountRecord> {
  return request<AccountRecord>(`${apiBase}/accounts/resolve?url=${encodeURIComponent(articleURL)}`, { signal })
}

export async function resolveAccountName(articleURL: string, signal?: AbortSignal): Promise<string> {
  const response = await request<{ readonly name: string }>(`${apiBase}/accounts/resolve-name?url=${encodeURIComponent(articleURL)}`, { signal })
  return response.name
}
export async function getAccountSelectorPage(params: SelectorPageParams, signal?: AbortSignal): Promise<PaginatedResponse<AccountOption>> {
  const response = await request<PaginatedResponse<AccountOption> | WorkspacePageResponse<AccountOption>>(`${apiBase}/selectors/accounts?${selectorPageQuery(params).toString()}`, { signal })
  return projectPage(normalizePage(response), projectAccountSelectorOption)
}
export async function getAlbumSelectorPage(params: AlbumSelectorPageParams, signal?: AbortSignal): Promise<PaginatedResponse<AlbumOption>> {
  const searchParams = selectorPageQuery(params)
  if (params.accountId?.trim()) searchParams.set('accountId', params.accountId.trim())
  const response = await request<PaginatedResponse<AlbumOption> | WorkspacePageResponse<AlbumOption>>(`${apiBase}/selectors/albums?${searchParams.toString()}`, { signal })
  return projectPage(normalizePage(response), projectAlbumSelectorOption)
}
export async function getArticleSelectorPage(params: ArticleSelectorPageParams, signal?: AbortSignal): Promise<PaginatedResponse<ArticleOption>> {
  const response = await request<PaginatedResponse<ArticleOption> | WorkspacePageResponse<ArticleOption>>(`${apiBase}/selectors/articles?${selectorPageQuery(params).toString()}`, { signal })
  return projectPage(normalizePage(response), projectArticleSelectorOption)
}
export async function saveAccount(input: AccountInput): Promise<AccountRecord> { return mutate<AccountRecord>('accounts', 'POST', input) }
export async function updateAccount(id: string, input: AccountInput): Promise<AccountRecord> { return mutate<AccountRecord>(`accounts/${encodeURIComponent(id)}`, 'PATCH', input) }
export async function deleteAccounts(ids: readonly string[], confirmation: string): Promise<void> { await mutate('accounts', 'DELETE', { ids, confirm: confirmation }) }
export function getAccountManifestDownloadURL(ids: readonly string[] = []): string {
  const selected = ids.map((id) => id.trim()).filter(Boolean)
  if (selected.length === 0) return `${apiBase}/accounts/manifest`
  const query = new URLSearchParams()
  selected.forEach((id) => query.append('accountId', id))
  return `${apiBase}/accounts/manifest?${query.toString()}`
}
export async function uploadAccountManifest(manifest: File): Promise<RestoreUploadReceipt> {
  const csrfToken = await getCSRFToken()
  const form = new FormData()
  form.append('manifest', manifest)
  return request<RestoreUploadReceipt>(`${apiBase}/accounts/manifest/upload`, {
    method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: form
  })
}
export async function importAccountManifest(uploadHandle: string): Promise<AccountManifestImportResult> {
  return mutate<AccountManifestImportResult>('accounts/manifest/import', 'POST', { uploadHandle })
}
export async function syncAccount(id: string, mode: AccountSyncMode = 'incremental'): Promise<JobRecord> {
  return mutate<JobRecord>(`accounts/${encodeURIComponent(id)}/sync`, 'POST', { incremental: mode === 'incremental' })
}
export async function syncAccounts(ids: readonly string[], mode: AccountSyncMode = 'incremental'): Promise<JobRecord> {
  return mutate<JobRecord>('accounts/sync', 'POST', { accountIds: ids, incremental: mode === 'incremental' })
}
export async function ingestURL(url: string, force = false): Promise<JobRecord> { return mutate<JobRecord>('ingest/url', 'POST', { url, force }) }
export async function downloadArticles(articleIds: readonly string[], kind: ArticleDownloadKind, force = false): Promise<JobRecord> {
  const resource = kind === 'article' ? 'articles/download' : `articles/${kind}`
  return mutate<JobRecord>(resource, 'POST', { articleIds, force })
}
export async function getArticlePreview(articleId: string, signal?: AbortSignal): Promise<ArticlePreview> {
  return request<ArticlePreview>(`${apiBase}/articles/preview?articleId=${encodeURIComponent(articleId)}`, { signal })
}
export async function getArticleResourceSummary(articleId: string, signal?: AbortSignal): Promise<ArticleResourceSummary> {
  return request<ArticleResourceSummary>(`${apiBase}/articles/${encodeURIComponent(articleId)}/resources`, { signal })
}
export async function getArticleDetail(articleId: string, signal?: AbortSignal): Promise<ArticleDetail> {
  const params = new URLSearchParams({ offset: '0', limit: '25' })
  return request<ArticleDetail>(`${apiBase}/articles/${encodeURIComponent(articleId)}/detail?${params}`, { signal })
}
export async function getArticleComments(articleId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<ArticleComments> {
  const params = new URLSearchParams({ offset: String((page - 1) * pageSize), limit: String(pageSize) })
  return request<ArticleComments>(`${apiBase}/articles/${encodeURIComponent(articleId)}/comments?${params}`, { signal })
}
export async function getArticleCommentReplies(articleId: string, commentId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<WorkspacePageResponse<ArticleReply>> {
  const params = new URLSearchParams({ offset: String((page - 1) * pageSize), limit: String(pageSize) })
  return requestWorkspacePage<ArticleReply>(`${apiBase}/articles/${encodeURIComponent(articleId)}/comments/${encodeURIComponent(commentId)}/replies?${params}`, signal)
}
export async function saveSavedQuery(input: SavedQueryInput): Promise<SavedQueryRecord> { return mutate<SavedQueryRecord>('saved-queries', 'POST', input) }
export async function deleteSavedQuery(name: string, confirmation: string): Promise<void> {
  const csrfToken = await getCSRFToken()
  const response = await fetch(`${apiBase}/saved-queries`, {
    method: 'DELETE', credentials: 'same-origin', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({ name, confirm: confirmation })
  })
  if (!response.ok) throw new ApiError(response.status, await readErrorMessage(response))
}
export async function traverseAlbum(albumId: string, accountId: string, order: AlbumTraversalOrder, download: boolean): Promise<JobRecord> {
  return mutate<JobRecord>(`albums/${encodeURIComponent(albumId)}/traverse`, 'POST', { accountId, order, download })
}
export async function traverseAlbums(albumIds: readonly string[], order: AlbumTraversalOrder, download: boolean): Promise<JobRecord> {
  return mutate<JobRecord>('albums/traverse', 'POST', { albumIds, order, download })
}
export type ConfirmedJobControlAction = Exclude<JobControlAction, 'resume'>

export async function controlJob(id: string, action: 'resume'): Promise<JobRecord>
export async function controlJob(id: string, action: ConfirmedJobControlAction, confirmation: string): Promise<JobRecord>
export async function controlJob(id: string, action: ConfirmedJobControlAction | 'resume', confirmation?: string): Promise<JobRecord> {
  return mutate<JobRecord>(`jobs/${encodeURIComponent(id)}/${action}`, 'POST', action === 'resume' ? {} : { confirm: confirmation })
}

export async function getAccountPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<AccountRecord>> {
  return getPage<AccountRecord>('accounts', params, signal)
}

export async function getArticlePage(params: ArticlePageParams, signal?: AbortSignal): Promise<PaginatedResponse<ArticleRecord>> {
  return getArticleQueryPage(params, signal)
}

export async function getAlbumPage(params: AlbumPageParams, signal?: AbortSignal): Promise<PaginatedResponse<AlbumRecord>> {
  const searchParams = new URLSearchParams({ offset: String((params.page - 1) * params.pageSize), limit: String(params.pageSize) })
  if (params.accountId?.trim()) searchParams.set('accountId', params.accountId.trim())
  if (params.keyword?.trim()) searchParams.set('keyword', params.keyword.trim())
  const response = await request<PaginatedResponse<AlbumRecord> | WorkspacePageResponse<AlbumRecord>>(`${apiBase}/albums?${searchParams.toString()}`, { signal })
  return normalizePage(response)
}

export async function getJobPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<JobRecord>> {
  return getPage<JobRecord>('jobs', params, signal)
}

export async function getJobDetail(id: string, signal?: AbortSignal): Promise<JobDetail> {
  return request<JobDetail>(`${apiBase}/jobs/${encodeURIComponent(id)}/detail`, { signal })
}

export async function getSavedQueryPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<SavedQueryRecord>> {
  return getPage<SavedQueryRecord>('saved-queries', params, signal)
}

export async function authorizeDefaultExportDirectory(): Promise<ExportDirectory> {
  return mutate<ExportDirectory>('export-directories/authorize', 'POST', { confirm: 'authorize-default-export-directory' })
}

export async function createExportDirectory(parentToken: string, name: string): Promise<ExportDirectory> {
  const trimmedName = name.trim()
  return mutate<ExportDirectory>('export-directories', 'POST', {
    parentToken,
    name: trimmedName,
    confirm: `create-export-directory:${parentToken}:${trimmedName}`
  })
}

export async function startExport(input: StartExportInput): Promise<ExportStartResult> {
  return mutate<ExportStartResult>('exports/start', 'POST', {
    ...input,
    confirm: `start-export:${input.directoryToken}`
  })
}

export async function getExportPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<ExportRecord>> {
  return getPage<ExportRecord>('exports', params, signal)
}

export async function getExportManifest(id: string, signal?: AbortSignal): Promise<ExportManifest> {
  return request<ExportManifest>(`${apiBase}/exports/${encodeURIComponent(id)}/manifest`, { signal })
}

export function getExportArtifactDownloadURL(exportId: string, artifactId: string): string {
  return `${apiBase}/exports/${encodeURIComponent(exportId)}/artifact?artifactId=${encodeURIComponent(artifactId)}`
}

export async function verifyExport(id: string): Promise<ExportVerification> {
  return mutate<ExportVerification>(`exports/${encodeURIComponent(id)}/verify`, 'POST', { confirm: `verify-export:${id}` })
}

export async function openExportOutput(id: string, confirmation: string): Promise<void> {
  await mutate(`exports/${encodeURIComponent(id)}/open`, 'POST', { confirm: confirmation })
}

export async function getCredentials(signal?: AbortSignal): Promise<readonly CredentialMetadata[]> { return request(`${apiBase}/settings/credentials`, { signal }) }
export async function validateCredential(input: CredentialImportInput): Promise<CredentialValidation> { return mutate('settings/credentials/validate', 'POST', input) }
export async function importCredential(input: CredentialImportInput): Promise<CredentialMetadata> { return mutate('settings/credentials/import', 'POST', input) }
export async function uploadCredentialFile(credential: File): Promise<CredentialMetadata> {
  const csrfToken = await getCSRFToken()
  const form = new FormData()
  form.append('credential', credential)
  return request<CredentialMetadata>(`${apiBase}/settings/credentials/upload`, {
    method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: form
  })
}
export async function removeCredential(id: string, confirmation: string): Promise<void> { await mutate(`settings/credentials/remove`, 'POST', { id, confirm: confirmation }) }
export async function getProxies(signal?: AbortSignal): Promise<readonly ProxyRoute[]> { return request(`${apiBase}/settings/proxies`, { signal }) }
export async function getProxyDisclosure(input: ProxyInput): Promise<ProxyDisclosure> { return mutate('settings/proxies/disclosure', 'POST', input) }
export async function addProxy(input: ProxyInput): Promise<ProxyRoute> { return mutate('settings/proxies', 'POST', input) }
export async function removeProxy(id: string, confirmation: string): Promise<ProxyRoute> { return mutate(`settings/proxies/${encodeURIComponent(id)}/remove`, 'POST', { confirm: confirmation }) }
export async function setProxyEnabled(id: string, enabled: boolean): Promise<ProxyRoute> { return mutate(`settings/proxies/${encodeURIComponent(id)}/${enabled ? 'enable' : 'disable'}`, 'POST', {}) }
export async function testProxy(id: string): Promise<ProxyProbeResult> { return mutate(`settings/proxies/${encodeURIComponent(id)}/test`, 'POST', {}) }
export async function getPreferences(signal?: AbortSignal): Promise<Preferences> { return request(`${apiBase}/settings/preferences`, { signal }) }
export async function patchPreferences(patch: PreferencesPatch): Promise<Preferences> { return mutate('settings/preferences', 'PATCH', patch) }
export async function createBackup(): Promise<BackupReceipt> { return mutate('maintenance/backups', 'POST', {}) }
export async function verifyBackup(backupId: string): Promise<BackupVerification> { return mutate('maintenance/backups/verify', 'POST', { backupId }) }
export function getBackupArtifactDownloadURL(backupId: string): string { return `${apiBase}/maintenance/backups/${encodeURIComponent(backupId)}` }
export async function uploadRestoreArchive(archive: File): Promise<RestoreUploadReceipt> {
  const csrfToken = await getCSRFToken()
  const form = new FormData()
  form.append('archive', archive)
  return request<RestoreUploadReceipt>(`${apiBase}/maintenance/restore/upload`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: form
  })
}
export async function prepareRestore(uploadHandle: string, conflictPolicy: RestoreConflictPolicy): Promise<RestorePreparation> { return mutate('maintenance/restore/prepare', 'POST', { uploadHandle, conflictPolicy }) }
export async function commitRestore(preparationId: string, confirmation: string): Promise<RestoreCompletion> { return mutate('maintenance/restore/commit', 'POST', { preparationId, confirmation }) }
export async function getIntegrity(signal?: AbortSignal): Promise<IntegrityReport> { return request(`${apiBase}/maintenance/integrity`, { signal }) }
export async function getDiagnostics(signal?: AbortSignal): Promise<DiagnosticsReport> { return request(`${apiBase}/maintenance/diagnostics`, { signal }) }
export async function createDiagnosticBundle(): Promise<DiagnosticBundleReceipt> { return mutate('maintenance/diagnostic-bundles', 'POST', {}) }
export function getDiagnosticBundleDownloadURL(handle: string): string { return `${apiBase}/maintenance/diagnostic-bundles/${encodeURIComponent(handle)}` }
export async function planGarbageCollection(): Promise<GarbageCollectionPlan> { return mutate('maintenance/gc/plan', 'POST', {}) }
export async function applyGarbageCollection(planId: string, confirmation: string): Promise<GarbageCollectionResult> { return mutate('maintenance/gc/apply', 'POST', { planId, confirmation }) }

const selectorMaximumPageSize = 100

function selectorPageQuery(params: SelectorPageParams): URLSearchParams {
  if (!Number.isSafeInteger(params.page) || params.page < 1) throw new RangeError('selector page must be a positive safe integer')
  if (!Number.isSafeInteger(params.pageSize) || params.pageSize < 1 || params.pageSize > selectorMaximumPageSize) {
    throw new RangeError(`selector page size must be between 1 and ${selectorMaximumPageSize}`)
  }
  const searchParams = new URLSearchParams({ page: String(params.page), page_size: String(params.pageSize) })
  if (params.search?.trim()) searchParams.set('search', params.search.trim())
  return searchParams
}

async function getPage<T>(resource: string, params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<T>> {
  const searchParams = new URLSearchParams({
    offset: String((params.page - 1) * params.pageSize),
    limit: String(params.pageSize)
  })
  if (params.search) searchParams.set('keyword', params.search)
  if (params.sort && params.direction) searchParams.set('sort', `${params.sort}:${params.direction}`)
  const response = await request<PaginatedResponse<T> | WorkspacePageResponse<T>>(`${apiBase}/${resource}?${searchParams.toString()}`, { signal })
  return normalizePage(response)
}

async function getArticleQueryPage(params: ArticlePageParams, signal?: AbortSignal): Promise<PaginatedResponse<ArticleRecord>> {
  const searchParams = new URLSearchParams({ offset: String((params.page - 1) * params.pageSize), limit: String(params.pageSize) })
  appendArticleQuery(searchParams, params)
  const response = await request<PaginatedResponse<ArticleRecord> | WorkspacePageResponse<ArticleRecord>>(`${apiBase}/articles?${searchParams.toString()}`, { signal })
  return normalizePage(response)
}

export function appendArticleQuery(searchParams: URLSearchParams, query: ArticleQuery): void {
  const stringFields: ReadonlyArray<keyof Pick<ArticleQuery, 'accountId' | 'albumId' | 'keyword' | 'author' | 'state' | 'publishedFrom' | 'publishedTo'>> = ['accountId', 'albumId', 'keyword', 'author', 'state', 'publishedFrom', 'publishedTo']
  for (const field of stringFields) {
    const value = query[field]
    if (value?.trim()) searchParams.set(field, value.trim())
  }
  const booleanFields: ReadonlyArray<keyof Pick<ArticleQuery, 'deleted' | 'hasContent' | 'hasComments' | 'original' | 'paid'>> = ['deleted', 'hasContent', 'hasComments', 'original', 'paid']
  for (const field of booleanFields) if (query[field] !== undefined) searchParams.set(field, String(query[field]))
  const numberFields: ReadonlyArray<keyof Omit<ArticleQuery, 'accountId' | 'albumId' | 'keyword' | 'author' | 'state' | 'publishedFrom' | 'publishedTo' | 'deleted' | 'hasContent' | 'hasComments' | 'original' | 'paid' | 'messageTypes' | 'sorts'>> = ['readMin', 'readMax', 'oldLikeMin', 'oldLikeMax', 'shareMin', 'shareMax', 'likeMin', 'likeMax', 'commentMin', 'commentMax', 'weCoinMin', 'weCoinMax', 'mediaSecondsMin', 'mediaSecondsMax']
  for (const field of numberFields) {
    const value = query[field]
    if (typeof value === 'number' && Number.isInteger(value) && value >= 0) searchParams.set(field, String(value))
  }
  for (const value of query.messageTypes ?? []) if (Number.isInteger(value) && value >= 0) searchParams.append('messageType', String(value))
  for (const sort of query.sorts ?? []) searchParams.append('sort', `${sort.field}:${sort.direction}`)
}

export function withoutArticleQuerySorting(query: ArticleQuery): ArticleQuery {
  const filters = { ...query }
  delete filters.sorts
  return filters
}

export function parseArticleQuery(value: unknown): ArticleQuery {
  if (!value || Array.isArray(value) || typeof value !== 'object') throw new Error('article query must be an object')
  const input = value as Record<string, unknown>
  const stringFields = ['accountId', 'albumId', 'keyword', 'author', 'state', 'publishedFrom', 'publishedTo'] as const
  const booleanFields = ['deleted', 'hasContent', 'hasComments', 'original', 'paid'] as const
  const numberFields = ['readMin', 'readMax', 'oldLikeMin', 'oldLikeMax', 'shareMin', 'shareMax', 'likeMin', 'likeMax', 'commentMin', 'commentMax', 'weCoinMin', 'weCoinMax', 'mediaSecondsMin', 'mediaSecondsMax'] as const
  const allowed = new Set<string>([...stringFields, ...booleanFields, ...numberFields, 'messageTypes', 'sorts'])
  if (Object.keys(input).some((key) => !allowed.has(key))) throw new Error('article query contains an unsupported field')
  const query: Record<string, unknown> = {}
  for (const field of stringFields) {
    const item = input[field]
    if (item === undefined || item === '') continue
    if (typeof item !== 'string') throw new Error('article query string filter is invalid')
    const normalized = item.trim()
    if (normalized) query[field] = normalized
  }
  for (const field of booleanFields) {
    const item = input[field]
    if (item === undefined) continue
    if (typeof item !== 'boolean') throw new Error('article query boolean filter is invalid')
    query[field] = item
  }
  for (const field of numberFields) {
    const item = input[field]
    if (item === undefined || item === '') continue
    if (!Number.isInteger(item) || (item as number) < 0) throw new Error('article query number filter is invalid')
    query[field] = item
  }
  const messageTypes = input.messageTypes
  if (messageTypes !== undefined) {
    if (!Array.isArray(messageTypes) || messageTypes.some((item) => !Number.isInteger(item) || item < 0)) throw new Error('article query message types are invalid')
    query.messageTypes = [...messageTypes]
  }
  const sorts = input.sorts
  if (sorts !== undefined) {
    if (!Array.isArray(sorts) || sorts.some((item) => !item || typeof item !== 'object' || typeof (item as Record<string, unknown>).field !== 'string' || !['asc', 'desc'].includes(String((item as Record<string, unknown>).direction)))) throw new Error('article query sorting is invalid')
    query.sorts = sorts.map((item) => ({ field: (item as ArticleSort).field.trim(), direction: (item as ArticleSort).direction }))
  }
  const publishedFrom = query.publishedFrom as string | undefined
  const publishedTo = query.publishedTo as string | undefined
  if ((publishedFrom && Number.isNaN(Date.parse(publishedFrom))) || (publishedTo && Number.isNaN(Date.parse(publishedTo)))) throw new Error('article query date filter is invalid')
  if (publishedFrom && publishedTo && publishedFrom > publishedTo) throw new Error('article query date range is invalid')
  for (const [minimum, maximum] of [['readMin', 'readMax'], ['oldLikeMin', 'oldLikeMax'], ['shareMin', 'shareMax'], ['likeMin', 'likeMax'], ['commentMin', 'commentMax'], ['weCoinMin', 'weCoinMax'], ['mediaSecondsMin', 'mediaSecondsMax']] as const) {
    if (typeof query[minimum] === 'number' && typeof query[maximum] === 'number' && query[minimum] > query[maximum]) throw new Error('article query range is invalid')
  }
  return query as ArticleQuery
}

export function saveExportHandoff(handoff: ExportHandoff): void {
  // A new navigation supersedes any handoff retained only for the current
  // route's Strict Mode render pair. This prevents a rapid second handoff
  // from being shadowed by an already-mounted export route.
  clearExportHandoffForMount()
  try {
    const safeHandoff = projectExportHandoff(handoff)
    if (safeHandoff) window.sessionStorage.setItem(exportHandoffStorageKey, JSON.stringify(safeHandoff))
  } catch { /* Browser storage can be unavailable. */ }
}

export function consumeExportHandoff(): ExportHandoff | undefined {
  try {
    const raw = window.sessionStorage.getItem(exportHandoffStorageKey)
    window.sessionStorage.removeItem(exportHandoffStorageKey)
    if (!raw) return undefined
    return projectExportHandoff(JSON.parse(raw))
  } catch { return undefined }
}

/**
 * React development Strict Mode renders a new route twice before effects run.
 * Keep one already-validated, single-use handoff in memory for that render
 * pair; the session-storage value is still consumed exactly once.
 */
export function consumeExportHandoffForMount(): ExportHandoff | undefined {
  if (hasPendingExportHandoffForStrictMount) return pendingExportHandoffForStrictMount
  pendingExportHandoffForStrictMount = consumeExportHandoff()
  hasPendingExportHandoffForStrictMount = true
  return pendingExportHandoffForStrictMount
}

export function clearExportHandoffForMount(): void {
  pendingExportHandoffForStrictMount = undefined
  hasPendingExportHandoffForStrictMount = false
}

export function saveArticleQueryHandoff(query: ArticleQuery): void {
  try { window.sessionStorage.setItem(articleQueryHandoffStorageKey, JSON.stringify(query)) } catch { /* Browser storage can be unavailable. */ }
}

export function consumeArticleQueryHandoff(): ArticleQuery | undefined {
  try {
    const raw = window.sessionStorage.getItem(articleQueryHandoffStorageKey)
    window.sessionStorage.removeItem(articleQueryHandoffStorageKey)
    if (!raw) return undefined
    const value = JSON.parse(raw)
    return value && typeof value === 'object' && !Array.isArray(value) ? value as ArticleQuery : undefined
  } catch { return undefined }
}

function projectExportHandoff(value: unknown): ExportHandoff | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const input = value as { readonly selection?: unknown; readonly label?: unknown; readonly presentation?: unknown }
  if (typeof input.label !== 'string') return undefined
  const selection = projectExportSelection(input.selection)
  if (!selection) return undefined
  const presentation = projectExportHandoffPresentation(input.presentation, selection)
  return {
    selection,
    label: input.label,
    ...(presentation ? { presentation } : {})
  }
}

function projectExportSelection(value: unknown): ExportSelection | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const selection = value as { readonly kind?: unknown; readonly articleIds?: unknown; readonly accountId?: unknown; readonly albumId?: unknown; readonly albumIds?: unknown; readonly savedQueryId?: unknown; readonly query?: unknown }
  switch (selection.kind) {
    case 'explicit_ids': {
      const articleIds = safeHandoffIDs(selection.articleIds)
      return articleIds ? { kind: 'explicit_ids', articleIds } : undefined
    }
    case 'account': return safeHandoffID(selection.accountId) ? { kind: 'account', accountId: selection.accountId } : undefined
    case 'album': return safeHandoffID(selection.albumId) ? { kind: 'album', albumId: selection.albumId } : undefined
    case 'album_ids': {
      const albumIds = safeHandoffIDs(selection.albumIds)
      return albumIds ? { kind: 'album_ids', albumIds } : undefined
    }
    case 'saved_query': return safeHandoffID(selection.savedQueryId) ? { kind: 'saved_query', savedQueryId: selection.savedQueryId } : undefined
    case 'all_matching': {
      try { return { kind: 'all_matching', query: parseArticleQuery(selection.query) } } catch { return undefined }
    }
    default: return undefined
  }
}

function projectExportHandoffPresentation(value: unknown, selection: ExportSelection): ExportHandoffPresentation | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const input = value as { readonly articles?: unknown; readonly matching?: unknown }
  if (selection.kind === 'explicit_ids') {
    if (!Array.isArray(input.articles) || input.articles.length !== selection.articleIds.length) return undefined
    const articles = input.articles.map(projectExportHandoffArticlePresentation)
    return articles.every((article): article is ExportHandoffArticlePresentation => article !== undefined) ? { articles } : undefined
  }
  if (selection.kind === 'all_matching') {
    const matching = projectExportHandoffMatchingPresentation(input.matching)
    return matching ? { matching } : undefined
  }
  return undefined
}

function projectExportHandoffArticlePresentation(value: unknown): ExportHandoffArticlePresentation | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const item = value as { readonly title?: unknown; readonly accountName?: unknown }
  const title = safeHandoffDisplayText(item.title)
  if (!title) return undefined
  const accountName = safeHandoffDisplayText(item.accountName)
  return { title, ...(accountName ? { accountName } : {}) }
}

function projectExportHandoffMatchingPresentation(value: unknown): ExportHandoffMatchingPresentation | undefined {
  if (!value || Array.isArray(value) || typeof value !== 'object') return undefined
  const item = value as { readonly total?: unknown; readonly accountName?: unknown; readonly albumName?: unknown }
  const total = item.total
  if (typeof total !== 'number' || !Number.isSafeInteger(total) || total < 0) return undefined
  const accountName = safeHandoffDisplayText(item.accountName)
  const albumName = safeHandoffDisplayText(item.albumName)
  return { total, ...(accountName ? { accountName } : {}), ...(albumName ? { albumName } : {}) }
}

function safeHandoffIDs(value: unknown): readonly string[] | undefined {
  return Array.isArray(value) && value.length > 0 && value.every(safeHandoffID) ? [...value] : undefined
}

function safeHandoffID(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== '' && value.length <= 256
}

function safeHandoffDisplayText(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const text = value.trim()
  return text && text.length <= 512 ? text : undefined
}

function normalizePage<T>(response: PaginatedResponse<T> | WorkspacePageResponse<T>): PaginatedResponse<T> {
  if ('data' in response) {
    return { data: response.data, pagination: response.pagination }
  }
  return {
    data: response.items,
    pagination: {
      page: Math.floor(response.offset / Math.max(1, response.limit)) + 1,
      pageSize: response.limit,
      total: response.total
    }
  }
}

function projectPage<Input, Output>(page: PaginatedResponse<Input>, project: (item: Input) => Output): PaginatedResponse<Output> {
  return { data: page.data.map(project), pagination: page.pagination }
}

function projectAccountSelectorOption(value: AccountOption): AccountOption {
  const option = value as Partial<AccountOption>
  const result: AccountOption = {
    id: requiredSelectorID(option.id),
    displayNameAvailable: option.displayNameAvailable === true
  }
  if (typeof option.displayName === 'string') return { ...result, displayName: option.displayName, ...(typeof option.alias === 'string' ? { alias: option.alias } : {}) }
  return typeof option.alias === 'string' ? { ...result, alias: option.alias } : result
}

function projectAlbumSelectorOption(value: AlbumOption): AlbumOption {
  const option = value as Partial<AlbumOption>
  const result: AlbumOption = {
    id: requiredSelectorID(option.id),
    displayNameAvailable: option.displayNameAvailable === true,
    accountNameAvailable: option.accountNameAvailable === true
  }
  return {
    ...result,
    ...(typeof option.accountId === 'string' ? { accountId: option.accountId } : {}),
    ...(typeof option.displayName === 'string' ? { displayName: option.displayName } : {}),
    ...(typeof option.accountName === 'string' ? { accountName: option.accountName } : {})
  }
}

function projectArticleSelectorOption(value: ArticleOption): ArticleOption {
  const option = value as Partial<ArticleOption>
  const id = requiredSelectorID(option.id)
  if (typeof option.title !== 'string' || !option.title.trim()) throw new Error('selector response contains an invalid article title')
  return {
    id,
    title: option.title,
    accountNameAvailable: option.accountNameAvailable === true,
    ...(typeof option.accountName === 'string' ? { accountName: option.accountName } : {})
  }
}

function requiredSelectorID(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) throw new Error('selector response contains an invalid identifier')
  return value
}

async function requestWorkspacePage<T>(path: string, signal?: AbortSignal): Promise<WorkspacePageResponse<T>> {
  const response = await fetch(path, { signal, credentials: 'same-origin', headers: { Accept: 'application/json' } })
  if (!response.ok) throw new ApiError(response.status, await readErrorMessage(response))
  const body = await response.json() as WorkspacePageResponse<T> & { readonly data?: readonly T[]; readonly apiVersion?: string }
  if (Array.isArray(body.data) && typeof body.total === 'number' && typeof body.offset === 'number' && typeof body.limit === 'number') {
    return { items: body.data, total: body.total, offset: body.offset, limit: body.limit }
  }
  return body
}

async function request<T>(path: string, init: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...init.headers
    }
  })

  if (!response.ok) {
    throw new ApiError(response.status, await readErrorMessage(response))
  }

  if (response.status === 204) return undefined as T

  const body = await response.json() as T | ApiEnvelope<T>
  // Page responses use the same versioned envelope as single-resource
  // responses, but their pagination metadata deliberately stays alongside
  // the data array. Keep that shape intact so normalizePage can read both.
  if (isPagedApiEnvelope(body)) return body as T
  return isApiEnvelope(body) ? body.data : body
}

async function mutate<T>(resource: string, method: 'POST' | 'PATCH' | 'DELETE', body: unknown): Promise<T> {
  const csrfToken = await getCSRFToken()
  return request<T>(`${apiBase}/${resource}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(body)
  })
}

function isApiEnvelope<T>(value: T | ApiEnvelope<T>): value is ApiEnvelope<T> {
  return typeof value === 'object' && value !== null && 'data' in value && 'apiVersion' in value
}

function isPagedApiEnvelope(value: unknown): value is ApiEnvelope<readonly unknown[]> & PaginatedResponse<unknown> {
  if (!isApiEnvelope(value)) return false
  const page = value as ApiEnvelope<readonly unknown[]> & { readonly pagination?: Partial<Pagination> }
  if (!Array.isArray(page.data) || typeof page.pagination !== 'object' || page.pagination === null) return false
  const pagination = page.pagination
  return typeof pagination.page === 'number' && typeof pagination.pageSize === 'number' && typeof pagination.total === 'number'
}

async function readErrorMessage(response: Response): Promise<string> {
  const fallback = `Request failed with status ${response.status}`
  try {
    const body = await response.json() as { readonly error?: { readonly message?: string } }
    return body.error?.message ?? fallback
  } catch {
    return fallback
  }
}
