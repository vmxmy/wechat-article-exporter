import { expect, type Page, type Route } from '@playwright/test'

const now = '2026-07-24T09:30:00.000Z'
const csrfToken = 'sanitized-e2e-csrf-token'

export interface SelectorRequest {
  readonly path: '/api/v1/selectors/accounts' | '/api/v1/selectors/albums' | '/api/v1/selectors/articles'
  readonly search: string
  readonly accountID?: string
}

export interface LoopbackFixture {
  readonly requests: readonly string[]
  readonly selectorRequests: readonly SelectorRequest[]
  readonly controls: readonly { readonly path: string; readonly confirmation?: string }[]
  readonly accountDeletions: readonly unknown[]
  readonly savedAccounts: readonly unknown[]
  readonly accountSyncs: readonly { readonly path: string; readonly incremental: boolean }[]
  readonly exports: readonly string[]
  readonly accountManifestImports: readonly unknown[]
  readonly preferencePatches: readonly unknown[]
  readonly diagnosticBundleRequests: readonly unknown[]
  readonly credentialRemovals: readonly unknown[]
  readonly credentialValidations: readonly unknown[]
  readonly proxyRemovals: readonly unknown[]
  readonly resourceDownloads: readonly unknown[]
  readonly albumTraversals: readonly unknown[]
  readonly accountSwitches: readonly string[]
}

interface LoopbackFixtureOptions {
  readonly displayLanguage?: 'en' | 'zh-CN'
  readonly savedQueries?: readonly { readonly name: string; readonly query: Record<string, unknown> }[]
}

export async function installLoopbackFixture(page: Page, options: LoopbackFixtureOptions = {}): Promise<LoopbackFixture> {
  const requests: string[] = []
  const selectorRequests: SelectorRequest[] = []
  const controls: Array<{ path: string; confirmation?: string }> = []
  const accountDeletions: unknown[] = []
  const savedAccounts: unknown[] = []
  const accountSyncs: Array<{ path: string; incremental: boolean }> = []
  const exports: string[] = []
  const accountManifestImports: unknown[] = []
  const preferencePatches: unknown[] = []
  const diagnosticBundleRequests: unknown[] = []
  const credentialRemovals: unknown[] = []
  const credentialValidations: unknown[] = []
  const proxyRemovals: unknown[] = []
  const resourceDownloads: unknown[] = []
  const albumTraversals: unknown[] = []
  const accountSwitches: string[] = []
  let loginState = 'unauthenticated'
  let directory = { token: 'dir-sanitized', label: 'Sanitized exports' }
  let backupID = ''
  let gcPlan = false
  let savedQueries: Array<{ name: string; query: Record<string, unknown>; createdAt: string; updatedAt: string }> = (options.savedQueries ?? []).map((savedQuery) => ({ ...savedQuery, createdAt: now, updatedAt: now }))

  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (url.hostname !== '127.0.0.1' && url.hostname !== 'localhost') {
      await route.abort('blockedbyclient')
      return
    }
    if (!url.pathname.startsWith('/api/v1/')) {
      await route.continue()
      return
    }

    requests.push(`${route.request().method()} ${url.pathname}`)
    await fulfillAPI(route, url, {
      loginState,
      directory,
      backupID,
      gcPlan,
      controls,
      selectorRequests,
      accountDeletions,
      savedAccounts,
      accountSyncs,
      exports,
      accountManifestImports,
      preferencePatches,
      diagnosticBundleRequests,
      credentialRemovals,
      credentialValidations,
      proxyRemovals,
      resourceDownloads,
      albumTraversals,
      accountSwitches,
      onLoginState: (state) => { loginState = state },
      onDirectory: (next) => { directory = next },
      onBackupID: (id) => { backupID = id },
      onGCPlan: (next) => { gcPlan = next },
      displayLanguage: options.displayLanguage,
      savedQueries,
      onSavedQueries: (next) => { savedQueries = next }
    })
  })
  return { requests, selectorRequests, controls, accountDeletions, savedAccounts, accountSyncs, exports, accountManifestImports, preferencePatches, diagnosticBundleRequests, credentialRemovals, credentialValidations, proxyRemovals, resourceDownloads, albumTraversals, accountSwitches }
}

export async function expectOnlyLoopbackRequests(page: Page) {
  const origins = await page.evaluate(() => performance.getEntriesByType('resource').map((entry) => new URL(entry.name).hostname))
  expect(origins.every((hostname) => hostname === '127.0.0.1' || hostname === 'localhost')).toBeTruthy()
}

export async function installExportArtifactFixture(page: Page) {
  await page.route('**/api/v1/exports/export-fixture-1/manifest', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ apiVersion: 'v1', data: { exportId: 'export-fixture-1', format: 'markdown', state: 'completed', provenanceState: 'complete', provenanceGeneration: 1, files: [{ artifactId: 'artifact-fixture-1', articleId: 'article-fixture-1', path: 'sanitized-article.md', sizeBytes: 42, sha256: 'a'.repeat(64), status: 'written' }] } }) }))
  await page.route('**/api/v1/exports/export-fixture-1/artifact?artifactId=artifact-fixture-1', (route) => route.fulfill({ contentType: 'text/markdown', headers: { 'content-disposition': 'attachment; filename="sanitized-article.md"' }, body: '# sanitized artifact\n' }))
  await page.route('**/api/v1/exports/export-fixture-1/open', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ apiVersion: 'v1', data: {} }) }))
}

interface State {
  readonly loginState: string
  readonly directory: { readonly token: string; readonly label: string }
  readonly backupID: string
  readonly gcPlan: boolean
  readonly displayLanguage?: 'en' | 'zh-CN'
  readonly controls: Array<{ path: string; confirmation?: string }>
  readonly selectorRequests: SelectorRequest[]
  readonly accountDeletions: unknown[]
  readonly savedAccounts: unknown[]
  readonly accountSyncs: Array<{ path: string; incremental: boolean }>
  readonly exports: string[]
  readonly accountManifestImports: unknown[]
  readonly preferencePatches: unknown[]
  readonly diagnosticBundleRequests: unknown[]
  readonly credentialRemovals: unknown[]
  readonly credentialValidations: unknown[]
  readonly proxyRemovals: unknown[]
  readonly resourceDownloads: unknown[]
  readonly albumTraversals: unknown[]
  readonly accountSwitches: string[]
  readonly onLoginState: (state: string) => void
  readonly onDirectory: (directory: { readonly token: string; readonly label: string }) => void
  readonly onBackupID: (id: string) => void
  readonly onGCPlan: (value: boolean) => void
  readonly savedQueries: readonly { name: string; query: Record<string, unknown>; createdAt: string; updatedAt: string }[]
  readonly onSavedQueries: (value: Array<{ name: string; query: Record<string, unknown>; createdAt: string; updatedAt: string }>) => void
}

async function fulfillAPI(route: Route, url: URL, state: State) {
  const method = route.request().method()
  const body = method === 'GET' || url.pathname === '/api/v1/maintenance/restore/upload' || url.pathname === '/api/v1/accounts/manifest/upload' ? undefined : JSON.parse(route.request().postData() || '{}') as Record<string, unknown>
  if (method !== 'GET' && url.pathname !== '/api/v1/status') {
    expect(route.request().headers()['x-csrf-token']).toBe(csrfToken)
  }

  if (url.pathname === '/api/v1/status') return json(route, { csrfToken })
  if (url.pathname === '/api/v1/runtime') return json(route, { version: 'e2e-sanitized', profileId: 'fixture-profile', session: state.loginState === 'authenticated' ? 'authenticated' : 'unauthenticated' })
  if (url.pathname === '/api/v1/session') return json(route, { state: state.loginState, accountId: state.loginState === 'authenticated' ? 'account-fixture' : undefined, accountName: state.loginState === 'authenticated' ? 'Fixture Account' : undefined })
  if (url.pathname === '/api/v1/session/accounts') return json(route, { available: true, accounts: [{ id: 'account-fixture', name: 'Fixture Account' }, { id: 'account-fixture-2', name: 'Second Fixture Account' }] })
  if (url.pathname === '/api/v1/storage') return json(route, storage())
  if (url.pathname === '/api/v1/events/snapshot') return json(route, { runtime: { version: 'e2e-sanitized', profileId: 'fixture-profile' }, session: { state: state.loginState }, storage: storage(), checkedAt: now })

  if (url.pathname === '/api/v1/login/begin') return json(route, { sessionId: 'login-fixture', qrCode: 'c2FuaXRpemVkLXFy', expiresAt: now })
  if (url.pathname === '/api/v1/login/poll') { state.onLoginState('scanned'); return json(route, { state: 'scanned', accountCount: 1 }) }
  if (url.pathname === '/api/v1/login/complete') { state.onLoginState('authenticated'); return json(route, { state: 'authenticated', accountId: 'account-fixture', accountName: 'Fixture Account' }) }
  if (url.pathname === '/api/v1/session/logout') { state.onLoginState('unauthenticated'); return route.fulfill({ status: 204, body: '' }) }
  if (url.pathname === '/api/v1/session/accounts/account-fixture-2/switch' && method === 'POST') {
    state.accountSwitches.push('account-fixture-2')
    return json(route, { state: 'authenticated', accountId: 'account-fixture-2', accountName: 'Second Fixture Account' })
  }

  if (url.pathname === '/api/v1/accounts' && method === 'DELETE') {
    if (body?.confirm !== 'delete-accounts:account-fixture') return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Confirmation required' } }) })
    state.accountDeletions.push(body)
    return route.fulfill({ status: 204, body: '' })
  }
  if (url.pathname === '/api/v1/accounts' && method === 'POST') {
    state.savedAccounts.push(body)
    return json(route, { id: 'account-discovered', fakeid: body?.fakeid, name: body?.name, alias: body?.alias, articleCount: 0, syncCompleted: false })
  }
  if (url.pathname === '/api/v1/accounts/account-discovered/sync' && method === 'POST') {
    state.accountSyncs.push({ path: url.pathname, incremental: body?.incremental === true })
    return json(route, { id: 'job-account-sync-fixture', kind: 'account_sync', label: 'Account Sync', state: 'queued', createdAt: now, updatedAt: now })
  }
  if (url.pathname === '/api/v1/selectors/accounts') {
    state.selectorRequests.push({ path: '/api/v1/selectors/accounts', search: url.searchParams.get('search')?.trim() ?? '' })
    const options = [{ id: 'account-fixture', displayName: 'Fixture Account', displayNameAvailable: true, alias: 'fixture' }, { id: 'account-beyond-first-page', displayName: 'Later Fixture Account', displayNameAvailable: true, alias: 'later' }, { id: 'account-fixture-unknown', displayNameAvailable: false }]
    return selectorPage(route, url, options.filter((item) => matchesSelectorSearch(item, url.searchParams.get('search'))))
  }
  if (url.pathname === '/api/v1/accounts') return page(route, [{ id: 'account-fixture', fakeid: 'fixture-account', name: 'Fixture Account', alias: 'fixture', articleCount: 2, lastSyncAt: now, syncCompleted: true }])
  if (url.pathname === '/api/v1/accounts/search') {
    if (state.loginState !== 'authenticated') return route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'authentication_required', message: 'workspace session must be authenticated' } }) })
    return page(route, [{ id: 'discovery-opaque-id', fakeid: 'fixture-discovered', name: 'Discovered Fixture Account', alias: 'discovered', articleCount: 0, syncCompleted: false }])
  }
  if (url.pathname === '/api/v1/accounts/resolve-name') {
    if (!url.searchParams.get('url')) return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { code: 'invalid_argument', message: 'article url is required' } }) })
    return json(route, { name: 'Resolved Account' })
  }
  if (url.pathname === '/api/v1/accounts/resolve') {
    if (!url.searchParams.get('url')) return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { code: 'invalid_argument', message: 'article url is required' } }) })
    if (state.loginState !== 'authenticated') return route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'authentication_required', message: 'workspace session must be authenticated' } }) })
    return json(route, { id: 'resolved-account', fakeid: 'fixture-resolved', name: 'Resolved Account', alias: 'resolved', articleCount: 0, syncCompleted: false })
  }
  if (url.pathname === '/api/v1/accounts/manifest') return route.fulfill({ contentType: 'application/json', headers: { 'content-disposition': 'attachment; filename="wechat-article-accounts-manifest.json"' }, body: '{"schemaVersion":1,"accounts":[]}' })
  if (url.pathname === '/api/v1/accounts/manifest/upload') return json(route, { handle: 'account-manifest-upload-fixture', sizeBytes: 24, sha256: 'e'.repeat(64), expiresAt: '2026-07-24T09:45:00.000Z' })
  if (url.pathname === '/api/v1/accounts/manifest/import') { state.accountManifestImports.push(body); return json(route, { report: { added: 1, merged: 2, unchanged: 3 } }) }
  if (url.pathname === '/api/v1/articles') {
    const articles = url.searchParams.get('offset') === '25'
      ? [{ id: 'article-fixture-3', title: 'Sanitized article three', accountName: 'Fixture Account', accountNameAvailable: true, author: 'Fixture Author', publishedAt: now, state: 'ready' }]
      : [{ id: 'article-fixture-1', title: 'Sanitized article one', accountName: 'Fixture Account', accountNameAvailable: true, author: 'Fixture Author', publishedAt: now, state: 'ready' }, { id: 'article-fixture-2', title: 'Sanitized article two', accountName: 'Fixture Account', accountNameAvailable: true, author: 'Fixture Author', publishedAt: now, state: 'queued' }]
    return page(route, articles, 26)
  }
  if (url.pathname === '/api/v1/selectors/articles') {
    state.selectorRequests.push({ path: '/api/v1/selectors/articles', search: url.searchParams.get('search')?.trim() ?? '' })
    const options = [
      { id: 'article-fixture-1', title: 'Sanitized article one', accountName: 'Fixture Account', accountNameAvailable: true },
      { id: 'article-fixture-2', title: 'Sanitized article two', accountName: 'Fixture Account', accountNameAvailable: true },
      { id: 'article-beyond-first-page', title: 'Later sanitized article', accountName: 'Later Fixture Account', accountNameAvailable: true }
    ]
    return selectorPage(route, url, options.filter((item) => matchesSelectorSearch(item, url.searchParams.get('search'))))
  }
  if (url.pathname === '/api/v1/articles/article-fixture-1/resources' && method === 'GET') return json(route, { articleId: 'article-fixture-1', total: 4, available: 3, missing: 1, complete: false, url: 'https://sensitive.example/resource', digest: 'sensitive-resource-digest', path: '/sensitive/resource/path', resourceId: 'sensitive-resource-id' })
  if (url.pathname === '/api/v1/articles/article-fixture-1/detail' && method === 'GET') return json(route, { articleId: 'article-fixture-1', metrics: { available: true, readCount: 120, oldLikeCount: 3, likeCount: 4, shareCount: 5, commentCount: 6, capturedAt: now }, resources: { items: [{ role: 'image', ordinal: 0, available: true }, { role: 'image', ordinal: 1, available: false }], total: 4, offset: 0, limit: 25 }, url: 'https://sensitive.example/resource', digest: 'sensitive-resource-digest', path: '/sensitive/resource/path', resourceId: 'sensitive-resource-id', credential: 'sensitive-credential' })
  if (url.pathname === '/api/v1/articles/article-fixture-1/comments' && method === 'GET') return json(route, { articleId: 'article-fixture-1', comments: { items: [{ id: 'comment-fixture-1', authorName: 'Fixture reader', content: 'Sanitized stored comment', createdAt: now, likeCount: 4, replyCount: 2, replyStatus: 'pending' }], total: 1, offset: 0, limit: 10 }, pendingReplies: 1, continuation: 'sensitive-continuation', credential: 'sensitive-credential', path: '/sensitive/path' })
  if (url.pathname === '/api/v1/articles/article-fixture-1/comments/comment-fixture-1/replies' && method === 'GET') return json(route, { data: [{ id: 'reply-fixture-1', authorName: 'Fixture author', content: 'Sanitized stored reply', createdAt: now, likeCount: 2 }], total: 1, offset: 0, limit: 10, continuation: 'sensitive-reply-buffer', rawRequest: 'sensitive-request-metadata' })
  if (url.pathname === '/api/v1/articles/resources' && method === 'POST') { state.resourceDownloads.push(body); return json(route, { id: 'job-resources-fixture', kind: 'resources', label: 'Resources', state: 'queued', createdAt: now, updatedAt: now }) }
  if (url.pathname === '/api/v1/albums') {
    const albums = url.searchParams.get('offset') === '25'
      ? [{ id: 'album-fixture-2', accountId: 'account-fixture', accountName: 'Fixture Account', accountNameAvailable: true, name: 'Sanitized album two', articleCount: 3, paid: false, description: 'Sanitized album two description' }]
      : [{ id: 'album-fixture-1', accountId: 'account-fixture', accountName: 'Fixture Account', accountNameAvailable: true, name: 'Sanitized album', articleCount: 2, paid: false, description: 'Sanitized album description' }]
    return page(route, albums, 26)
  }
  if (url.pathname === '/api/v1/selectors/albums') {
    const options = [{ id: 'album-fixture-1', accountId: 'account-fixture', displayName: 'Sanitized album', displayNameAvailable: true, accountName: 'Fixture Account', accountNameAvailable: true }, { id: 'album-beyond-first-page', accountId: 'account-beyond-first-page', displayName: 'Later fixture album', displayNameAvailable: true, accountName: 'Later Fixture Account', accountNameAvailable: true }, { id: 'album-fixture-unknown', displayNameAvailable: false, accountNameAvailable: false }]
    const accountID = url.searchParams.get('accountId')?.trim()
    state.selectorRequests.push({ path: '/api/v1/selectors/albums', search: url.searchParams.get('search')?.trim() ?? '', ...(accountID ? { accountID } : {}) })
    return selectorPage(route, url, options.filter((item) => (!accountID || item.accountId === accountID) && matchesSelectorSearch(item, url.searchParams.get('search'))))
  }
  if ((url.pathname === '/api/v1/albums/album-fixture-1/traverse' || url.pathname === '/api/v1/albums/traverse') && method === 'POST') { state.albumTraversals.push(body); return json(route, { id: 'job-album-fixture', kind: 'album_sync', label: 'Album Sync', state: 'queued', createdAt: now, updatedAt: now }) }
  if (url.pathname === '/api/v1/saved-queries' && method === 'GET') return page(route, state.savedQueries)
  if (url.pathname === '/api/v1/saved-queries' && method === 'POST') {
    const name = String(body?.name || '').trim()
    const query = body?.query
    if (!name || !query || Array.isArray(query) || typeof query !== 'object') return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Invalid saved query' } }) })
    const next = { name, query: query as Record<string, unknown>, createdAt: now, updatedAt: now }
    state.onSavedQueries([...state.savedQueries.filter((item) => item.name !== name), next])
    return json(route, next)
  }
  if (url.pathname === '/api/v1/saved-queries' && method === 'DELETE') {
    const name = String(body?.name || '').trim()
    if (body?.confirm !== `delete-saved-query:${name}`) return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Confirmation required' } }) })
    state.onSavedQueries(state.savedQueries.filter((item) => item.name !== name))
    return route.fulfill({ status: 204, body: '' })
  }
  if (url.pathname === '/api/v1/jobs') return page(route, [{ id: 'job-fixture-1', kind: 'export', label: 'Export', state: 'running', profile: 'fixture-profile', createdAt: now, updatedAt: now, counts: { completed: 1, total: 2 }, permittedActions: ['pause', 'cancel'] }])
  if (url.pathname === '/api/v1/jobs/job-fixture-1/detail') return json(route, { job: { id: 'job-fixture-1', kind: 'export', label: 'Export', state: 'running', profile: 'fixture-profile', createdAt: now, updatedAt: now, counts: { completed: 1, total: 2 }, permittedActions: ['pause', 'cancel'] }, items: [{ id: 'item-fixture-1', state: 'completed', attemptCount: 1, createdAt: now, updatedAt: now }, { id: 'item-fixture-2', state: 'running', attemptCount: 2, errorClass: 'network', createdAt: now, updatedAt: now }], itemsTotal: 2, itemsLimited: false, logs: [{ id: 1, itemId: 'item-fixture-2', level: 'info', message: 'Sanitized local progress', createdAt: now }], lease: { active: true, expiresAt: '2026-07-24T09:35:00.000Z' }, refreshedAt: now })
  const createdJobDetail = url.pathname.match(/^\/api\/v1\/jobs\/(job-(?:export|import)-[a-z0-9-]+)\/detail$/)
  if (createdJobDetail) {
    const id = createdJobDetail[1]
    const kind = id.startsWith('job-export-') ? 'export' : 'article_download'
    const label = kind === 'export' ? 'Export' : 'Import article'
    return json(route, { job: { id, kind, label, state: 'queued', profile: 'fixture-profile', createdAt: now, updatedAt: now, counts: { completed: 0, total: 1 }, permittedActions: ['cancel'] }, items: [], itemsTotal: 0, itemsLimited: false, logs: [], lease: { active: false }, refreshedAt: now })
  }
  if (/^\/api\/v1\/jobs\/job-fixture-1\/(pause|resume|retry|cancel)$/.test(url.pathname)) {
    const action = url.pathname.split('/').at(-1)
    if (action !== 'resume' && body?.confirm !== `${action}-job:job-fixture-1`) return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Confirmation required' } }) })
    state.controls.push({ path: url.pathname, confirmation: typeof body?.confirm === 'string' ? body.confirm : undefined })
    return json(route, { id: 'job-fixture-1', kind: 'export', label: 'Export', state: url.pathname.endsWith('cancel') ? 'cancelled' : 'running', createdAt: now, updatedAt: now })
  }

  if (url.pathname === '/api/v1/export-directories/authorize') return json(route, state.directory)
  if (url.pathname === '/api/v1/export-directories') { const name = String(body?.name || 'child'); const next = { token: `dir-${name}`, label: `Sanitized exports/${name}` }; state.onDirectory(next); return json(route, next) }
  if (url.pathname === '/api/v1/exports/start') { state.exports.push(JSON.stringify(body)); return json(route, { jobId: 'job-export-fixture' }) }
  if (url.pathname === '/api/v1/exports') return page(route, [{ id: 'export-fixture-1', jobId: 'job-fixture-1', format: 'markdown', state: 'completed', createdAt: now, completedAt: now, provenanceState: 'complete', provenanceGeneration: 1, outputDirectory: 'opaque-directory-token' }])
  if (url.pathname === '/api/v1/exports/export-fixture-1/manifest') return json(route, { exportId: 'export-fixture-1', format: 'markdown', state: 'completed', provenanceState: 'complete', provenanceGeneration: 1, files: [{ articleId: 'article-fixture-1', path: '/private/export/root/sanitized-article.md', sizeBytes: 42, sha256: 'a'.repeat(64), status: 'written' }] })
  if (url.pathname === '/api/v1/exports/export-fixture-1/verify') return json(route, { exportId: 'export-fixture-1', valid: true, verifiedOutputs: 1, issues: [] })

  if (url.pathname === '/api/v1/settings/credentials' && method === 'GET') return json(route, [{ id: 'credential-fixture', accountId: 'account-fixture', accountName: 'Fixture Account', accountNameAvailable: true, kind: 'cookie', status: 'valid', createdAt: now, updatedAt: now }])
  if (url.pathname === '/api/v1/settings/credentials/validate' && method === 'POST') { state.credentialValidations.push(body); return json(route, { valid: true, status: 'valid' }) }
  if (url.pathname === '/api/v1/settings/credentials/remove') {
    if (body?.id !== 'credential-fixture' || body?.confirm !== 'remove-credential:credential-fixture') return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Confirmation required' } }) })
    state.credentialRemovals.push(body)
    return route.fulfill({ status: 204, body: '' })
  }
  if (url.pathname === '/api/v1/settings/proxies' && method === 'GET') return json(route, [{ id: 'proxy-fixture', name: 'Sanitized proxy', endpoint: 'https://proxy.fixture/?token=%5BREDACTED%5D', authorizationConfigured: true, trust: 'public-only', classes: ['public_content'], priority: 0, enabled: true, health: { state: 'healthy' }, createdAt: now, updatedAt: now }])
  if (url.pathname === '/api/v1/settings/proxies/proxy-fixture/remove') {
    if (body?.confirm !== 'remove-proxy:proxy-fixture') return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Confirmation required' } }) })
    state.proxyRemovals.push(body)
    return json(route, { id: 'proxy-fixture', name: 'Sanitized proxy', endpoint: 'https://proxy.fixture/?token=%5BREDACTED%5D', authorizationConfigured: true, trust: 'public-only', classes: ['public_content'], priority: 0, enabled: true, health: { state: 'healthy' }, createdAt: now, updatedAt: now })
  }
  if (url.pathname === '/api/v1/settings/preferences' && method === 'GET') return json(route, preferences(state.displayLanguage))
  if (url.pathname === '/api/v1/settings/preferences' && method === 'PATCH') { state.preferencePatches.push(body); return json(route, body) }
  if (url.pathname === '/api/v1/maintenance/integrity') return json(route, { checkedAt: now, issues: [] })
  if (url.pathname === '/api/v1/maintenance/diagnostics') return json(route, { collectedAt: now, checks: [{ name: 'loopback', status: 'ok', summary: 'Sanitized local fixture' }] })
  if (url.pathname === '/api/v1/maintenance/diagnostic-bundles' && method === 'POST') { state.diagnosticBundleRequests.push(body); return json(route, { handle: 'diagnostic_sanitized-handle', createdAt: now, expiresAt: '2026-07-24T09:45:00.000Z', sha256: 'd'.repeat(64), sizeBytes: 128 }) }
  if (url.pathname === '/api/v1/maintenance/diagnostic-bundles/diagnostic_sanitized-handle' && method === 'GET') return route.fulfill({ contentType: 'application/zip', headers: { 'content-disposition': 'attachment; filename="diagnostic-bundle.zip"' }, body: 'sanitized diagnostic bundle' })
  if (url.pathname === '/api/v1/maintenance/backups') { state.onBackupID('backup-fixture'); return json(route, { id: 'backup-fixture', createdAt: now, sha256: 'b'.repeat(64), bytes: 64, objects: 1 }) }
  if (url.pathname === '/api/v1/maintenance/backups/verify') return json(route, { backupId: state.backupID || 'backup-fixture', valid: true })
  if (url.pathname === '/api/v1/maintenance/restore/upload') return json(route, { handle: 'restore-upload-fixture', sizeBytes: 24, sha256: 'c'.repeat(64), expiresAt: '2026-07-24T09:45:00.000Z' })
  if (url.pathname === '/api/v1/maintenance/restore/prepare') return json(route, { id: 'restore-preparation-fixture', confirmation: 'confirm-restore-fixture', conflictPolicy: body?.conflictPolicy, expiresAt: '2026-07-24T09:45:00.000Z' })
  if (url.pathname === '/api/v1/maintenance/restore/commit') return json(route, { restoredFiles: 2, restoredBytes: 24, profiles: 1 })
  if (url.pathname === '/api/v1/maintenance/gc/plan') { state.onGCPlan(true); return json(route, gc()) }
  if (url.pathname === '/api/v1/maintenance/gc/apply') { state.onGCPlan(false); return json(route, { deletedObjects: { count: 1, bytes: 42 }, deletedTemporaryFiles: { count: 0, bytes: 0 }, deletedDebugCaptures: { count: 0, bytes: 0 }, deletedCompletedJobLogs: { count: 0, bytes: 0 }, skipped: 0 }) }

  return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: { message: `Unmocked sanitized route: ${method} ${url.pathname}` } }) })
}

function json(route: Route, body: unknown) { return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) }) }
function page(route: Route, data: readonly unknown[], total = data.length) { return json(route, { data, pagination: { page: 1, pageSize: 25, total } }) }
function selectorPage(route: Route, url: URL, data: readonly unknown[]) {
  const pageNumber = Math.max(1, Number(url.searchParams.get('page')) || 1)
  const pageSize = Math.min(100, Math.max(1, Number(url.searchParams.get('page_size')) || 50))
  const offset = (pageNumber - 1) * pageSize
  return json(route, { data: data.slice(offset, offset + pageSize), pagination: { page: pageNumber, pageSize, total: data.length } })
}
function matchesSelectorSearch(item: Record<string, unknown>, search: string | null) {
  const normalized = search?.trim().toLocaleLowerCase()
  if (!normalized) return true
  return [item.displayName, item.title, item.alias, item.accountName].some((value) => typeof value === 'string' && value.toLocaleLowerCase().includes(normalized))
}
function storage() { return { databaseAvailable: true, objectStoreReady: true, accounts: 1, articles: 2, albums: 0, jobs: 1, objects: 2, objectBytes: 84 } }
function preferences(displayLanguage: 'en' | 'zh-CN' = 'en') { return { sync: { range: 'all', pageDelay: 1, jitter: 0, pageSize: 20, incremental: true, unsafePacingSaved: false }, download: { concurrency: 2, forceContent: false, metadataOverridesContent: false }, export: { namingTemplate: '{title}', maximumNameBytes: 180, collisionPolicy: 'suffix', excelIncludeContent: true, jsonIncludeContent: true, jsonIncludeComments: false, htmlIncludeComments: false }, display: { noColor: false, ascii: false, plain: false, hideDeleted: false, language: displayLanguage }, proxy: { directFirst: true, fallbackEnabled: false } } }
function gc() { return { id: 'gc-fixture', generatedAt: now, expiresAt: '2026-07-24T10:30:00.000Z', unreferencedObjects: { count: 1, bytes: 42 }, temporaryFiles: { count: 0, bytes: 0 }, expiredDebugCaptures: { count: 0, bytes: 0 }, completedJobLogs: { count: 0, bytes: 0 }, confirmation: 'apply-gc-fixture' } }
