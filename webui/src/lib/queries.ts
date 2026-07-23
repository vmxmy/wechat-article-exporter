import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getAccountPage,
  getAlbumPage,
  getArticlePage,
  getArticleDetail,
  getArticleResourceSummary,
  getExportManifest,
  getExportPage,
  getJobDetail,
  getJobPage,
  getRuntimeStatus,
  getSavedQueryPage,
  getSessionStatus,
  getStorageStatus,
  getWorkspaceSnapshot,
  authorizeDefaultExportDirectory, beginLogin, completeLogin, controlJob, createExportDirectory, deleteAccounts, ingestURL, logout, openExportOutput, pollLogin, saveAccount, searchAccounts, startExport, syncAccount, updateAccount, verifyExport,
  addProxy, applyGarbageCollection, commitRestore, createBackup, createDiagnosticBundle, getCredentials, getDiagnostics, getIntegrity, getPreferences, getProxies, importAccountManifest, importCredential, patchPreferences, planGarbageCollection, prepareRestore, removeCredential, removeProxy, setProxyEnabled, testProxy, uploadAccountManifest, uploadCredentialFile, uploadRestoreArchive, validateCredential, verifyBackup,
  deleteSavedQuery, downloadArticles, saveSavedQuery, traverseAlbum,
  type AccountInput,
  type AccountSyncMode,
  type AlbumPageParams,
  type AlbumTraversalOrder,
  type ArticleDownloadKind,
  type ArticlePageParams,
  type PageParams,
  type RestoreConflictPolicy
  ,type SavedQueryInput
  ,type ConfirmedJobControlAction
} from './api'

export const queryKeys = {
  runtime: ['runtime'] as const,
  session: ['session'] as const,
  storage: ['storage'] as const,
  snapshot: ['snapshot'] as const,
  accounts: (params: PageParams) => ['accounts', params] as const,
  articles: (params: ArticlePageParams) => ['articles', params] as const,
  articleResourceSummary: (id: string) => ['articles', id, 'resources'] as const,
  articleDetail: (id: string) => ['articles', id, 'detail'] as const,
  albums: (params: AlbumPageParams) => ['albums', params] as const,
  jobs: (params: PageParams) => ['jobs', params] as const,
  jobDetail: (id: string) => ['jobs', id, 'detail'] as const,
  exports: (params: PageParams) => ['exports', params] as const,
  exportManifest: (id: string) => ['exports', id, 'manifest'] as const,
  savedQueries: (params: PageParams) => ['saved-queries', params] as const,
  credentials: ['maintenance', 'credentials'] as const,
  proxies: ['maintenance', 'proxies'] as const,
  preferences: ['maintenance', 'preferences'] as const,
  integrity: ['maintenance', 'integrity'] as const,
  diagnostics: ['maintenance', 'diagnostics'] as const
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

export function useArticleResourceSummary(id: string | undefined) {
  return useQuery({
    queryKey: queryKeys.articleResourceSummary(id ?? ''),
    queryFn: ({ signal }) => getArticleResourceSummary(id ?? '', signal),
    enabled: Boolean(id)
  })
}

export function useArticleDetail(id: string | undefined) {
  return useQuery({
    queryKey: queryKeys.articleDetail(id ?? ''),
    queryFn: ({ signal }) => getArticleDetail(id ?? '', signal),
    enabled: Boolean(id)
  })
}

export function useAlbumPage(params: AlbumPageParams) {
  return usePageQuery(queryKeys.albums(params), ({ signal }) => getAlbumPage(params, signal))
}

export function useJobPage(params: PageParams) {
  return usePageQuery(queryKeys.jobs(params), ({ signal }) => getJobPage(params, signal), snapshotPolling)
}

export function useJobDetail(id: string | undefined) {
  return useQuery({ queryKey: queryKeys.jobDetail(id ?? ''), queryFn: ({ signal }) => getJobDetail(id ?? '', signal), enabled: Boolean(id), ...snapshotPolling })
}

export function useExportPage(params: PageParams) {
  return usePageQuery(queryKeys.exports(params), ({ signal }) => getExportPage(params, signal), snapshotPolling)
}

export function useExportManifest(id: string | undefined) {
  return useQuery({ queryKey: queryKeys.exportManifest(id ?? ''), queryFn: ({ signal }) => getExportManifest(id ?? '', signal), enabled: Boolean(id) })
}

export function useSavedQueryPage(params: PageParams) {
  return usePageQuery(queryKeys.savedQueries(params), ({ signal }) => getSavedQueryPage(params, signal))
}

export function useCredentials() { return useQuery({ queryKey: queryKeys.credentials, queryFn: ({ signal }) => getCredentials(signal) }) }
export function useProxies() { return useQuery({ queryKey: queryKeys.proxies, queryFn: ({ signal }) => getProxies(signal) }) }
export function usePreferences() { return useQuery({ queryKey: queryKeys.preferences, queryFn: ({ signal }) => getPreferences(signal) }) }
export function useIntegrity() { return useQuery({ queryKey: queryKeys.integrity, queryFn: ({ signal }) => getIntegrity(signal) }) }
export function useDiagnostics() { return useQuery({ queryKey: queryKeys.diagnostics, queryFn: ({ signal }) => getDiagnostics(signal) }) }

export function useWorkspaceMutations() {
  const client = useQueryClient()
  const refresh = () => client.invalidateQueries()
  const refreshAfterLogout = async () => {
    await client.cancelQueries()
    client.removeQueries({
      type: 'inactive',
      predicate: (query) => query.queryKey[0] !== queryKeys.session[0]
    })
    client.setQueryData(queryKeys.session, { state: 'unauthenticated' })
    await client.invalidateQueries({
      predicate: (query) => query.queryKey[0] === queryKeys.runtime[0] || query.queryKey[0] === queryKeys.snapshot[0],
      refetchType: 'active'
    })
  }
  return {
    beginLogin: useMutation({ mutationFn: beginLogin }),
    pollLogin: useMutation({ mutationFn: pollLogin }),
    completeLogin: useMutation({ mutationFn: completeLogin, onSuccess: refresh }),
    logout: useMutation({ mutationFn: logout, onSuccess: refreshAfterLogout }),
    saveAccount: useMutation({ mutationFn: (input: AccountInput) => saveAccount(input), onSuccess: refresh }),
    updateAccount: useMutation({ mutationFn: ({ id, input }: { id: string; input: AccountInput }) => updateAccount(id, input), onSuccess: refresh }),
    deleteAccounts: useMutation({ mutationFn: ({ ids, confirmation }: { ids: readonly string[]; confirmation: string }) => deleteAccounts(ids, confirmation), onSuccess: refresh }),
    uploadAccountManifest: useMutation({ mutationFn: uploadAccountManifest }),
    importAccountManifest: useMutation({ mutationFn: importAccountManifest, onSuccess: refresh }),
    syncAccount: useMutation({ mutationFn: ({ id, mode }: { id: string; mode: AccountSyncMode }) => syncAccount(id, mode), onSuccess: refresh }),
    ingestURL: useMutation({ mutationFn: ({ url, force }: { url: string; force: boolean }) => ingestURL(url, force), onSuccess: refresh }),
    downloadArticles: useMutation({ mutationFn: ({ articleIds, kind, force }: { articleIds: readonly string[]; kind: ArticleDownloadKind; force?: boolean }) => downloadArticles(articleIds, kind, force), onSuccess: refresh }),
    saveSavedQuery: useMutation({ mutationFn: (input: SavedQueryInput) => saveSavedQuery(input), onSuccess: refresh }),
    deleteSavedQuery: useMutation({ mutationFn: ({ name, confirmation }: { name: string; confirmation: string }) => deleteSavedQuery(name, confirmation), onSuccess: refresh }),
    traverseAlbum: useMutation({ mutationFn: ({ albumId, accountId, order, download }: { albumId: string; accountId: string; order: AlbumTraversalOrder; download: boolean }) => traverseAlbum(albumId, accountId, order, download), onSuccess: refresh }),
    controlJob: useMutation({ mutationFn: (input: { id: string; action: 'resume' } | { id: string; action: ConfirmedJobControlAction; confirmation: string }) => input.action === 'resume' ? controlJob(input.id, input.action) : controlJob(input.id, input.action, input.confirmation), onSuccess: refresh }),
    authorizeDefaultExportDirectory: useMutation({ mutationFn: authorizeDefaultExportDirectory }),
    createExportDirectory: useMutation({ mutationFn: ({ parentToken, name }: { parentToken: string; name: string }) => createExportDirectory(parentToken, name) }),
    startExport: useMutation({ mutationFn: startExport, onSuccess: refresh }),
    verifyExport: useMutation({ mutationFn: verifyExport, onSuccess: refresh }),
    openExportOutput: useMutation({ mutationFn: ({ id, confirmation }: { id: string; confirmation: string }) => openExportOutput(id, confirmation) })
    ,validateCredential: useMutation({ mutationFn: validateCredential })
    ,importCredential: useMutation({ mutationFn: importCredential, onSuccess: refresh })
    ,uploadCredentialFile: useMutation({ mutationFn: uploadCredentialFile, onSuccess: refresh })
    ,removeCredential: useMutation({ mutationFn: ({ id, confirmation }: { id: string; confirmation: string }) => removeCredential(id, confirmation), onSuccess: refresh })
    ,addProxy: useMutation({ mutationFn: addProxy, onSuccess: refresh })
    ,removeProxy: useMutation({ mutationFn: ({ id, confirmation }: { id: string; confirmation: string }) => removeProxy(id, confirmation), onSuccess: refresh })
    ,setProxyEnabled: useMutation({ mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => setProxyEnabled(id, enabled), onSuccess: refresh })
    ,testProxy: useMutation({ mutationFn: testProxy, onSuccess: refresh })
    ,patchPreferences: useMutation({ mutationFn: patchPreferences, onSuccess: refresh })
    ,createBackup: useMutation({ mutationFn: createBackup })
    ,verifyBackup: useMutation({ mutationFn: verifyBackup })
    ,uploadRestoreArchive: useMutation({ mutationFn: uploadRestoreArchive })
    ,prepareRestore: useMutation({ mutationFn: ({ uploadHandle, conflictPolicy }: { uploadHandle: string; conflictPolicy: RestoreConflictPolicy }) => prepareRestore(uploadHandle, conflictPolicy) })
    ,commitRestore: useMutation({ mutationFn: ({ preparationId, confirmation }: { preparationId: string; confirmation: string }) => commitRestore(preparationId, confirmation) })
    ,createDiagnosticBundle: useMutation({ mutationFn: createDiagnosticBundle })
    ,planGarbageCollection: useMutation({ mutationFn: planGarbageCollection })
    ,applyGarbageCollection: useMutation({ mutationFn: ({ planId, confirmation }: { planId: string; confirmation: string }) => applyGarbageCollection(planId, confirmation), onSuccess: refresh })
  }
}

export function useAccountSearch(params: PageParams) {
  return useQuery({ queryKey: ['account-search', params], queryFn: ({ signal }) => searchAccounts(params, signal), placeholderData: keepPreviousData, enabled: false })
}

function usePageQuery<T>(queryKey: readonly unknown[], queryFn: ({ signal }: { signal: AbortSignal }) => Promise<T>, polling?: typeof snapshotPolling) {
  return useQuery({ queryKey, queryFn, placeholderData: keepPreviousData, ...polling })
}
