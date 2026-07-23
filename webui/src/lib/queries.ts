import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { getArticlePage, getRuntimeStatus, type ArticlePageParams } from './api'

export const queryKeys = {
  runtime: ['runtime'] as const,
  articles: (params: ArticlePageParams) => ['articles', params] as const
}

export function useRuntimeStatus() {
  return useQuery({
    queryKey: queryKeys.runtime,
    queryFn: ({ signal }) => getRuntimeStatus(signal),
    refetchInterval: 10_000,
    refetchIntervalInBackground: false
  })
}

export function useArticlePage(params: ArticlePageParams) {
  return useQuery({
    queryKey: queryKeys.articles(params),
    queryFn: ({ signal }) => getArticlePage(params, signal),
    placeholderData: keepPreviousData
  })
}
