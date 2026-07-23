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

export interface RuntimeStatus {
  readonly profileId: string
  readonly session: 'authenticated' | 'unauthenticated'
}

export interface ArticleRecord {
  readonly id: string
  readonly title: string
  readonly accountName: string
  readonly publishedAt: string | null
  readonly status: string
}

export interface ArticlePageParams {
  readonly page: number
  readonly pageSize: number
  readonly search: string
  readonly sort: string
  readonly direction: 'asc' | 'desc'
}

export async function getRuntimeStatus(signal?: AbortSignal): Promise<RuntimeStatus> {
  return request<RuntimeStatus>(`${apiBase}/runtime`, { signal })
}

export async function getArticlePage(params: ArticlePageParams, signal?: AbortSignal): Promise<PaginatedResponse<ArticleRecord>> {
  const searchParams = new URLSearchParams({
    page: String(params.page),
    page_size: String(params.pageSize),
    search: params.search,
    sort: params.sort,
    direction: params.direction
  })
  return request<PaginatedResponse<ArticleRecord>>(`${apiBase}/articles?${searchParams.toString()}`, { signal })
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

  return response.json() as Promise<T>
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
