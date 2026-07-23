import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  getAccountPage,
  getAlbumPage,
  getArticlePage,
  getJobPage,
  getRuntimeStatus,
  getSavedQueryPage,
  getSessionStatus,
  getStorageStatus,
  getWorkspaceSnapshot,
  type ArticlePageParams,
  type PageParams
} from './api'

export const queryKeys = {
  runtime: ['runtime'] as const,
  session: ['session'] as const,
  storage: ['storage'] as const,
  snapshot: ['snapshot'] as const,
  accounts: (params: PageParams) => ['accounts', params] as const,
  articles: (params: ArticlePageParams) => ['articles', params] as const,
  albums: (params: PageParams) => ['albums', params] as const,
  jobs: (params: PageParams) => ['jobs', params] as const,
  savedQueries: (params: PageParams) => ['saved-queries', params] as const
}

const snapshotPolling = { refetchInterval: 5_000, refetchIntervalInBackground: false } as const

export function useRuntimeStatus() {
  return useQuery({ queryKey: queryKeys.runtime, queryFn: ({ signal }) => getRuntimeStatus(signal), ...snapshotPolling })
}

export function useSessionStatus() {
  return useQuery({ queryKey: queryKeys.session, queryFn: ({ signal }) => getSessionStatus(signal), ...snapshotPolling })
}

export function useStorageStatus() {
  return useQuery({ queryKey: queryKeys.storage, queryFn: ({ signal }) => getStorageStatus(signal), ...snapshotPolling })
}

export function useWorkspaceSnapshot() {
  return useQuery({ queryKey: queryKeys.snapshot, queryFn: ({ signal }) => getWorkspaceSnapshot(signal), ...snapshotPolling })
}

export function useAccountPage(params: PageParams) {
  return usePageQuery(queryKeys.accounts(params), ({ signal }) => getAccountPage(params, signal))
}

export function useArticlePage(params: ArticlePageParams) {
  return usePageQuery(queryKeys.articles(params), ({ signal }) => getArticlePage(params, signal))
}

export function useAlbumPage(params: PageParams) {
  return usePageQuery(queryKeys.albums(params), ({ signal }) => getAlbumPage(params, signal))
}

export function useJobPage(params: PageParams) {
  return usePageQuery(queryKeys.jobs(params), ({ signal }) => getJobPage(params, signal), snapshotPolling)
}

export function useSavedQueryPage(params: PageParams) {
  return usePageQuery(queryKeys.savedQueries(params), ({ signal }) => getSavedQueryPage(params, signal))
}

function usePageQuery<T>(queryKey: readonly unknown[], queryFn: ({ signal }: { signal: AbortSignal }) => Promise<T>, polling?: typeof snapshotPolling) {
  return useQuery({ queryKey, queryFn, placeholderData: keepPreviousData, ...polling })
}
