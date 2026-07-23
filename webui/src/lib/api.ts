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
  readonly version: string
  readonly data: T
}

interface WorkspacePageResponse<T> {
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

export interface AccountRecord {
  readonly id: string
  readonly name: string
  readonly alias?: string
  readonly description?: string
  readonly articleCount?: number
  readonly lastSyncAt?: string
  readonly syncCompleted?: boolean
}

export interface ArticleRecord {
  readonly id: string
  readonly title: string
  readonly accountId?: string
  readonly accountName?: string
  readonly author?: string
  readonly publishedAt: string | null
  readonly state?: string
  readonly status?: string
  readonly hasContent?: boolean
  readonly hasComments?: boolean
}

export interface AlbumRecord {
  readonly id: string
  readonly accountId?: string
  readonly name: string
  readonly description?: string
  readonly articleCount: number
  readonly paid?: boolean
}

export interface JobRecord {
  readonly id: string
  readonly kind: string
  readonly state: string
  readonly profile?: string
  readonly createdAt: string
  readonly updatedAt: string
  readonly counts?: Readonly<Record<string, number>>
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

export interface SavedQueryRecord {
  readonly name: string
  readonly query: Readonly<Record<string, unknown>>
  readonly createdAt: string
  readonly updatedAt: string
}

export interface WorkspaceSnapshot {
  readonly runtime: RuntimeStatus
  readonly session: SessionStatus
  readonly storage: StorageStatus
  readonly jobs?: PaginatedResponse<JobRecord>
  readonly checkedAt?: string
}

export interface ArticlePageParams extends PageParams {
  readonly search: string
  readonly sort: string
  readonly direction: 'asc' | 'desc'
}

export async function getRuntimeStatus(signal?: AbortSignal): Promise<RuntimeStatus> {
  return request<RuntimeStatus>(`${apiBase}/runtime`, { signal })
}

export async function getSessionStatus(signal?: AbortSignal): Promise<SessionStatus> {
  return request<SessionStatus>(`${apiBase}/session`, { signal })
}

export async function getStorageStatus(signal?: AbortSignal): Promise<StorageStatus> {
  return request<StorageStatus>(`${apiBase}/storage`, { signal })
}

export async function getWorkspaceSnapshot(signal?: AbortSignal): Promise<WorkspaceSnapshot> {
  return request<WorkspaceSnapshot>(`${apiBase}/events/snapshot`, { signal })
}

export async function getAccountPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<AccountRecord>> {
  return getPage<AccountRecord>('accounts', params, signal)
}

export async function getArticlePage(params: ArticlePageParams, signal?: AbortSignal): Promise<PaginatedResponse<ArticleRecord>> {
  return getPage<ArticleRecord>('articles', params, signal)
}

export async function getAlbumPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<AlbumRecord>> {
  return getPage<AlbumRecord>('albums', params, signal)
}

export async function getJobPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<JobRecord>> {
  return getPage<JobRecord>('jobs', params, signal)
}

export async function getSavedQueryPage(params: PageParams, signal?: AbortSignal): Promise<PaginatedResponse<SavedQueryRecord>> {
  return getPage<SavedQueryRecord>('saved-queries', params, signal)
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

function normalizePage<T>(response: PaginatedResponse<T> | WorkspacePageResponse<T>): PaginatedResponse<T> {
  if ('data' in response) return response
  return {
    data: response.items,
    pagination: {
      page: Math.floor(response.offset / Math.max(1, response.limit)) + 1,
      pageSize: response.limit,
      total: response.total
    }
  }
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

  const body = await response.json() as T | ApiEnvelope<T>
  return isApiEnvelope(body) ? body.data : body
}

function isApiEnvelope<T>(value: T | ApiEnvelope<T>): value is ApiEnvelope<T> {
  return typeof value === 'object' && value !== null && 'data' in value && 'version' in value
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
