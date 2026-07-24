import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  controlJob,
  deleteAccounts,
  deleteSavedQuery,
  type AccountOption,
  type ArticleOption,
  type AlbumOption,
  type PaginatedResponse,
  getAccountPage,
  getAccountSelectorPage,
  getAlbumPage,
  getAlbumSelectorPage,
  getArticleSelectorPage,
  getArticleResourceSummary,
  getArticleDetail,
  getArticleComments,
  getArticleCommentReplies,
  getDiagnosticBundleDownloadURL,
  getExportArtifactDownloadURL,
  getRuntimeStatus,
  logout,
  consumeExportHandoff,
  consumeExportHandoffForMount,
  clearExportHandoffForMount,
  saveExportHandoff,
  syncAccount,
  traverseAlbums,
  validateCredential
} from '../src/lib/api'

const fetchMock = vi.fn()

afterEach(() => {
  clearExportHandoffForMount()
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('browser API client', () => {
  it('unwraps the versioned response envelope and sends same-origin credentials', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: { version: 'test-build', session: 'authenticated' }
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getRuntimeStatus()).resolves.toEqual({ version: 'test-build', session: 'authenticated' })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/runtime', expect.objectContaining({
      credentials: 'same-origin',
      headers: { Accept: 'application/json' }
    }))
  })

  it('normalizes bounded workspace pages and preserves encoded query parameters', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      items: [{ id: 'account-1', name: 'Fixture' }],
      total: 31,
      offset: 10,
      limit: 10
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getAccountPage({ page: 2, pageSize: 10, search: 'a & b', sort: 'name', direction: 'asc' })).resolves.toEqual({
      data: [{ id: 'account-1', name: 'Fixture' }],
      pagination: { page: 2, pageSize: 10, total: 31 }
    })
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/accounts?offset=10&limit=10&keyword=a+%26+b&sort=name%3Aasc',
      expect.any(Object)
    )
  })

  it('preserves pagination while unwrapping a versioned page envelope', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: [{ id: 'account-1', name: 'Fixture' }],
      pagination: { page: 1, pageSize: 25, total: 1 },
      items: [{ id: 'account-1', name: 'Fixture' }],
      offset: 0,
      limit: 25,
      total: 1
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getAccountPage({ page: 1, pageSize: 25 })).resolves.toEqual({
      data: [{ id: 'account-1', name: 'Fixture' }],
      pagination: { page: 1, pageSize: 25, total: 1 }
    })
  })

  it('requests a server-filtered album page without loading the full collection', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      items: [{ id: 'album-1', accountId: 'account / fixture', name: 'Fixture album', articleCount: 2 }],
      total: 1,
      offset: 25,
      limit: 25
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getAlbumPage({ page: 2, pageSize: 25, accountId: ' account / fixture ', keyword: ' fixture & album ' })).resolves.toEqual({
      data: [{ id: 'album-1', accountId: 'account / fixture', name: 'Fixture album', articleCount: 2 }],
      pagination: { page: 2, pageSize: 25, total: 1 }
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/albums?offset=25&limit=25&accountId=account+%2F+fixture&keyword=fixture+%26+album', expect.any(Object))
  })

  it('loads a bounded, encoded account selector page and projects only safe fields', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: [{ id: 'account / fixture', displayName: 'Fixture Account', displayNameAvailable: true, alias: 'fixture', fakeid: 'private-fakeid' }, { id: 'account-missing', displayNameAvailable: false, avatarUrl: 'https://private.example/avatar' }],
      pagination: { page: 2, pageSize: 25, total: 27 }
    }))
    vi.stubGlobal('fetch', fetchMock)

    const selectorPage: Promise<PaginatedResponse<AccountOption>> = getAccountSelectorPage({ page: 2, pageSize: 25, search: ' fixture & account ' })
    await expect(selectorPage).resolves.toEqual({
      data: [{ id: 'account / fixture', displayName: 'Fixture Account', displayNameAvailable: true, alias: 'fixture' }, { id: 'account-missing', displayNameAvailable: false }],
      pagination: { page: 2, pageSize: 25, total: 27 }
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/selectors/accounts?page=2&page_size=25&search=fixture+%26+account', expect.objectContaining({
      credentials: 'same-origin', headers: { Accept: 'application/json' }
    }))
  })

  it('loads a bounded album selector page with account display availability', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      items: [{ id: 'album / fixture', accountId: 'account / fixture', displayName: 'Fixture album', displayNameAvailable: true, accountName: 'Fixture Account', accountNameAvailable: true, description: 'private-description' }, { id: 'album-missing', displayNameAvailable: false, accountNameAvailable: false, upstreamId: 'private-upstream' }],
      total: 26,
      offset: 25,
      limit: 25
    }))
    vi.stubGlobal('fetch', fetchMock)

    const selectorPage: Promise<PaginatedResponse<AlbumOption>> = getAlbumSelectorPage({ page: 2, pageSize: 25, accountId: ' account / fixture ', search: ' fixture & album ' })
    await expect(selectorPage).resolves.toEqual({
      data: [{ id: 'album / fixture', accountId: 'account / fixture', displayName: 'Fixture album', displayNameAvailable: true, accountName: 'Fixture Account', accountNameAvailable: true }, { id: 'album-missing', displayNameAvailable: false, accountNameAvailable: false }],
      pagination: { page: 2, pageSize: 25, total: 26 }
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/selectors/albums?page=2&page_size=25&search=fixture+%26+album&accountId=account+%2F+fixture', expect.any(Object))
  })

  it('loads an encoded article selector page with same-origin credentials and projects only safe fields', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: [{ id: 'article / fixture', title: 'Fixture article', accountName: 'Fixture Account', accountNameAvailable: true, canonicalUrl: 'https://private.example/article', digest: 'private body', readCount: 42 }, { id: 'article-missing', title: 'Fallback article', accountNameAvailable: false, author: 'private author' }],
      pagination: { page: 2, pageSize: 25, total: 27 }
    }))
    vi.stubGlobal('fetch', fetchMock)

    const selectorPage: Promise<PaginatedResponse<ArticleOption>> = getArticleSelectorPage({ page: 2, pageSize: 25, search: ' fixture & article ' })
    await expect(selectorPage).resolves.toEqual({
      data: [{ id: 'article / fixture', title: 'Fixture article', accountName: 'Fixture Account', accountNameAvailable: true }, { id: 'article-missing', title: 'Fallback article', accountNameAvailable: false }],
      pagination: { page: 2, pageSize: 25, total: 27 }
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/selectors/articles?page=2&page_size=25&search=fixture+%26+article', expect.objectContaining({
      credentials: 'same-origin', headers: { Accept: 'application/json' }
    }))
  })

  it('refuses selector page sizes beyond the backend bound before issuing a request', async () => {
    vi.stubGlobal('fetch', fetchMock)

    await expect(getArticleSelectorPage({ page: 1, pageSize: 101 })).rejects.toThrow('selector page size must be between 1 and 100')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('keeps validated action contracts separate from readable export-handoff presentation', () => {
    const sessionStorage = storage()
    vi.stubGlobal('window', { sessionStorage })

    saveExportHandoff({
      selection: { kind: 'all_matching', query: { accountId: 'account-private-id', albumId: 'album-private-id', keyword: 'fixture', sorts: [{ field: 'published', direction: 'desc' }] } },
      label: 'Current matching filter',
      presentation: {
        matching: { total: 134, accountName: 'Fixture Account', albumName: 'Fixture Album' },
        articles: [{ title: 'Must be discarded for matching scope', accountName: 'Private Account' }]
      }
    })

    expect(JSON.parse(sessionStorage.getItem('wechat-article.export-handoff.v1') ?? '{}')).toEqual({
      selection: { kind: 'all_matching', query: { accountId: 'account-private-id', albumId: 'album-private-id', keyword: 'fixture', sorts: [{ field: 'published', direction: 'desc' }] } },
      label: 'Current matching filter',
      presentation: { matching: { total: 134, accountName: 'Fixture Account', albumName: 'Fixture Album' } }
    })
    expect(consumeExportHandoff()).toEqual({
      selection: { kind: 'all_matching', query: { accountId: 'account-private-id', albumId: 'album-private-id', keyword: 'fixture', sorts: [{ field: 'published', direction: 'desc' }] } },
      label: 'Current matching filter',
      presentation: { matching: { total: 134, accountName: 'Fixture Account', albumName: 'Fixture Album' } }
    })
    expect(sessionStorage.getItem('wechat-article.export-handoff.v1')).toBeNull()
  })

  it('rejects unvalidated export-handoff display data while preserving a valid ID action scope', () => {
    const sessionStorage = storage()
    vi.stubGlobal('window', { sessionStorage })
    sessionStorage.setItem('wechat-article.export-handoff.v1', JSON.stringify({
      selection: { kind: 'explicit_ids', articleIds: ['article-one', 'article-two'] },
      label: '2 selected articles',
      presentation: {
        articles: [{ title: 'Readable article' }, { title: '' }],
        matching: { total: 10, accountName: 'should be discarded' }
      }
    }))

    expect(consumeExportHandoff()).toEqual({
      selection: { kind: 'explicit_ids', articleIds: ['article-one', 'article-two'] },
      label: '2 selected articles'
    })
  })

  it('keeps a valid export handoff across the Strict Mode render pair without reusing it after mount', () => {
    const sessionStorage = storage()
    vi.stubGlobal('window', { sessionStorage })
    const handoff = {
      selection: { kind: 'all_matching' as const, query: { accountId: 'account-private-id' } },
      label: 'Current matching filter',
      presentation: { matching: { total: 4, accountName: 'Fixture Account' } }
    }

    saveExportHandoff(handoff)

    expect(consumeExportHandoffForMount()).toEqual(handoff)
    expect(consumeExportHandoffForMount()).toEqual(handoff)
    expect(sessionStorage.getItem('wechat-article.export-handoff.v1')).toBeNull()

    clearExportHandoffForMount()
    expect(consumeExportHandoffForMount()).toBeUndefined()

    saveExportHandoff({ ...handoff, label: 'A new export handoff' })
    expect(consumeExportHandoffForMount()).toEqual({ ...handoff, label: 'A new export handoff' })
  })

  it('queues one bounded multi-album traversal without exposing account or host data', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { id: 'job-albums', kind: 'album_sync' } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(traverseAlbums(['album-2', 'album-1'], 'reverse', true)).resolves.toMatchObject({ id: 'job-albums' })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/albums/traverse', expect.objectContaining({
      method: 'POST', body: JSON.stringify({ albumIds: ['album-2', 'album-1'], order: 'reverse', download: true })
    }))
  })

  it('loads a resource summary without requesting resource URLs, digests, or paths', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: { articleId: 'article / fixture', total: 4, available: 3, missing: 1, complete: false }
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getArticleResourceSummary('article / fixture')).resolves.toEqual({ articleId: 'article / fixture', total: 4, available: 3, missing: 1, complete: false })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/articles/article%20%2F%20fixture/resources', expect.objectContaining({
      credentials: 'same-origin',
      headers: { Accept: 'application/json' }
    }))
  })

  it('loads bounded safe article metrics and resource details', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: { articleId: 'article / fixture', metrics: { available: true, readCount: 12, oldLikeCount: 3, likeCount: 4, shareCount: 5, commentCount: 6, capturedAt: '2026-07-24T10:00:00Z' }, resources: { items: [{ role: 'image', ordinal: 0, available: true }], total: 1, offset: 0, limit: 25 } }
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getArticleDetail('article / fixture')).resolves.toMatchObject({ articleId: 'article / fixture', metrics: { readCount: 12 }, resources: { items: [{ role: 'image', ordinal: 0, available: true }] } })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/articles/article%20%2F%20fixture/detail?offset=0&limit=25', expect.any(Object))
  })

  it('loads bounded stored comments and server-paginated replies without requesting remote state', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { articleId: 'article / fixture', comments: { items: [{ id: 'comment-1', authorName: 'Reader', content: 'Stored', likeCount: 2, replyCount: 1, replyStatus: 'pending' }], total: 11, offset: 10, limit: 10 }, pendingReplies: 1 } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: [{ id: 'reply-1', authorName: 'Author', content: 'Stored reply', likeCount: 1 }], total: 2, offset: 10, limit: 10 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getArticleComments('article / fixture', 2, 10)).resolves.toMatchObject({ articleId: 'article / fixture', pendingReplies: 1, comments: { total: 11, offset: 10 } })
    await expect(getArticleCommentReplies('article / fixture', 'comment / fixture', 2, 10)).resolves.toMatchObject({ total: 2, offset: 10, limit: 10, items: [{ id: 'reply-1' }] })
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/articles/article%20%2F%20fixture/comments?offset=10&limit=10', expect.any(Object))
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/articles/article%20%2F%20fixture/comments/comment%20%2F%20fixture/replies?offset=10&limit=10', expect.any(Object))
  })

  it('sends caller-supplied exact confirmations for account and saved-query deletion', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({}))
    vi.stubGlobal('fetch', fetchMock)

    await expect(deleteAccounts(['account / one'], 'user-account-proof')).resolves.toBeUndefined()
    await expect(deleteSavedQuery('draft / query', 'user-query-proof')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/accounts', expect.objectContaining({
      method: 'DELETE',
      body: JSON.stringify({ ids: ['account / one'], confirm: 'user-account-proof' })
    }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/saved-queries', {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-CSRF-Token': 'csrf-fixture'
      },
      body: JSON.stringify({ name: 'draft / query', confirm: 'user-query-proof' })
    })
  })

  it('sends caller-supplied job proofs while leaving resume confirmation-free', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { id: 'job / one' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { id: 'job / one' } }))
    vi.stubGlobal('fetch', fetchMock)

    await controlJob('job / one', 'pause', 'user-pause-proof')
    await controlJob('job / one', 'resume')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/jobs/job%20%2F%20one/pause', expect.objectContaining({ body: JSON.stringify({ confirm: 'user-pause-proof' }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/jobs/job%20%2F%20one/resume', expect.objectContaining({ body: '{}' }))
  })

  it('sends the selected account synchronization mode without exposing local state', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { id: 'job-account-sync' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { id: 'job-account-sync' } }))
    vi.stubGlobal('fetch', fetchMock)

    await syncAccount('account / one')
    await syncAccount('account / one', 'full')

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/accounts/account%20%2F%20one/sync', expect.objectContaining({
      method: 'POST', body: JSON.stringify({ incremental: true })
    }))
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/v1/accounts/account%20%2F%20one/sync', expect.objectContaining({
      method: 'POST', body: JSON.stringify({ incremental: false })
    }))
  })

  it('revokes the local session with the CSRF proof and accepts an empty response', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(logout()).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/session/logout', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-CSRF-Token': 'csrf-fixture'
      },
      body: '{}'
    })
  })

  it('surfaces safe server failures and safely encodes opaque download identifiers', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: { message: 'session expired' } }, 401))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getRuntimeStatus()).rejects.toEqual(expect.objectContaining<ApiError>({
      name: 'ApiError', status: 401, message: 'session expired'
    }))
    expect(getExportArtifactDownloadURL('export / one', 'artifact?two')).toBe('/api/v1/exports/export%20%2F%20one/artifact?artifactId=artifact%3Ftwo')
    expect(getDiagnosticBundleDownloadURL('bundle / one')).toBe('/api/v1/maintenance/diagnostic-bundles/bundle%20%2F%20one')
  })

  it('submits write-only credential validation and returns only safe metadata', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { valid: true, status: 'valid' } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(validateCredential({ biz: 'biz-secret', uin: 'uin-secret', key: 'key-secret', passTicket: 'ticket-secret', wapSid2: 'sid-secret', appMsgToken: 'token-secret', cookie: 'cookie-secret' })).resolves.toEqual({ valid: true, status: 'valid' })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/settings/credentials/validate', expect.objectContaining({
      method: 'POST', body: JSON.stringify({ biz: 'biz-secret', uin: 'uin-secret', key: 'key-secret', passTicket: 'ticket-secret', wapSid2: 'sid-secret', appMsgToken: 'token-secret', cookie: 'cookie-secret' })
    }))
  })
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

function storage(): Storage {
  const values = new Map<string, string>()
  return {
    get length() { return values.size },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => { values.delete(key) },
    setItem: (key, value) => { values.set(key, value) }
  }
}
