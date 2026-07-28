import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installExportArtifactFixture, installLoopbackFixture } from './fixtures/loopback-api'
import { selectRemoteSelectorOption, selectStaticSelectorOption } from './fixtures/selectors'

test('sanitized loopback fixture covers QR login and logout UI', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'WeChat login' })).toBeVisible()
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await expect(page.getByRole('img', { name: 'QR-code login' })).toBeVisible()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await expect(page.getByText('Scanned', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Complete login' }).click()
  await expect(page.getByText('Authenticated', { exact: true })).toBeVisible()
  await selectStaticSelectorOption(page, 'Eligible account', 'Second Fixture Account')
  await expect(page.getByRole('status').filter({ hasText: 'Switched to Second Fixture Account.' })).toBeVisible()
  expect(fixture.accountSwitches).toEqual(['account-fixture-2'])
  await page.getByRole('button', { name: 'Log out' }).click()
  const logoutDialog = page.getByRole('alertdialog', { name: 'Sign out of the local session?' })
  await expect(logoutDialog).toBeVisible()
  await logoutDialog.getByRole('button', { name: 'Log out' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Signed out of the local session.' })).toBeVisible()
  await expect(page.getByText('Not signed in', { exact: true })).toBeVisible()
  expect(fixture.requests).toContain('POST /api/v1/session/logout')
  await expectOnlyLoopbackRequests(page)
})

test('login clearly reports unavailable account switching', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/session/accounts', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ apiVersion: 'v1', data: { available: false, accounts: [] } }) }))
  await page.goto('/login')
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await page.getByRole('button', { name: 'Complete login' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Account switching is not available for this local session.' })).toBeVisible()
})

test('account resources keep batch controls and column options in the table toolbar', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')

  const surface = page.locator('.presentation-data-table-surface')
  const toolbar = surface.locator('.presentation-data-table-toolbar')
  await expect(toolbar.getByRole('button', { name: 'Add account', exact: true })).toBeVisible()
  await expect(toolbar.getByRole('button', { name: 'Import account manifest', exact: true })).toBeVisible()
  await expect(toolbar.getByRole('button', { name: 'Download selected account manifest', exact: true })).toBeDisabled()
  await expect(toolbar.getByRole('button', { name: 'Delete selected account', exact: true })).toBeDisabled()
  await expect(toolbar.getByRole('button', { name: 'Sync selected account', exact: true })).toBeDisabled()

  await page.getByRole('checkbox', { name: 'Select Fixture Account' }).check()
  await expect(toolbar.getByRole('button', { name: 'Download selected account manifest', exact: true })).toBeEnabled()
  await expect(toolbar.getByRole('button', { name: 'Delete selected account', exact: true })).toBeEnabled()
  await expect(toolbar.getByRole('button', { name: 'Sync selected account', exact: true })).toBeEnabled()

  await expect(page.getByRole('navigation', { name: 'Account pagination', exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('account drawer preserves focus while drafting non-primary fields', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')
  await page.getByRole('button', { name: 'Add account', exact: true }).click()

  const overlay = page.locator('.presentation-drawer-overlay')
  await expect(overlay).toBeVisible()
  await expect.poll(() => overlay.evaluate((element) => {
    const bounds = element.getBoundingClientRect()
    return [bounds.x, bounds.y, bounds.width, bounds.height]
  })).toEqual([0, 0, 1280, 720])

  const alias = page.getByRole('textbox', { name: 'Alias' })
  await alias.fill('Fixture alias')
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
  await expect(alias).toBeFocused()
  await page.locator('a[href="#astryx-app-shell-main"]').evaluate((link) => (link as HTMLElement).focus())
  await expect(alias).toBeFocused()
  await expectOnlyLoopbackRequests(page)
})

test('account resource errors do not render pagination controls', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/accounts?offset=0&limit=25', (route) => route.fulfill({
    status: 503,
    contentType: 'application/json',
    body: JSON.stringify({ error: { message: 'fixture unavailable' } })
  }))
  await page.goto('/accounts')

  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Account pagination', exact: true })).toHaveCount(0)
  await expectOnlyLoopbackRequests(page)
})

test('mobile account empty state retains its explanation and primary action', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installLoopbackFixture(page)
  await page.route('**/api/v1/accounts?offset=0&limit=25', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      data: [],
      pagination: { page: 1, pageSize: 25, total: 0 }
    })
  }))
  await page.goto('/accounts')

  await expect(page.getByRole('heading', { name: 'No saved accounts yet' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add account' }).last()).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('sanitized account and article selections remain browser-local', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')
  await page.getByRole('checkbox', { name: 'Select Fixture Account' }).check()
  await expect(page.getByRole('checkbox', { name: 'Select account-fixture' })).toHaveCount(0)
  await expect(page.getByRole('table')).not.toContainText('account-fixture')
  await expect(page.getByRole('group', { name: 'Selected account actions' })).toBeVisible()
  await page.goto('/articles')
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized article one' }))
  await expect(page.getByRole('region', { name: 'Selected article actions' }).first()).toContainText('1 selected')
  await page.getByRole('textbox', { name: 'Search articles' }).fill('Sanitized')
  await expect(page.getByRole('cell', { name: 'Sanitized article one', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Selected article actions' }).first()).toContainText('1 selected')
  await expectOnlyLoopbackRequests(page)
})

test('article selections persist across server pages and hand off all selected stable IDs for export', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')

  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized article one' }))
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('cell', { name: 'Sanitized article three', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Selected article actions' }).first()).toContainText('1 selected')

  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized article three' }))
  await expect(page.getByRole('region', { name: 'Selected article actions' }).first()).toContainText('2 selected')
  await page.getByRole('button', { name: 'Export selected' }).click()

  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page).toHaveURL(/\/exports\?flow=[a-f0-9]{32}$/)
  expect(page.url()).not.toContain('article-fixture-1')
  expect(page.url()).not.toContain('article-fixture-3')
  await expect(page.getByRole('status').filter({ hasText: '2 selected articles' })).toContainText('Sanitized article one')
  await expect(page.getByRole('status').filter({ hasText: '2 selected articles' })).toContainText('Sanitized article three')
  await expect(page.locator('body')).not.toContainText('article-fixture-1')
  await expect(page.locator('body')).not.toContainText('article-fixture-3')
  await continueToExportDestination(page)
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  await expect.poll(() => fixture.exports).toHaveLength(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({ selection: { kind: 'explicit_ids', articleIds: ['article-fixture-1', 'article-fixture-3'] } })
  await expectOnlyLoopbackRequests(page)
})

test('changing an export scope discards an invisible prior scope before it can be queued', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.addInitScript(() => {
    window.sessionStorage.setItem('wechat-article.export-handoff.v1', JSON.stringify({
      selection: { kind: 'account', accountId: 'account-fixture' },
      label: 'Fixture Account'
    }))
  })
  await page.goto('/exports')

  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeEnabled()
  await chooseExportScope(page, 'articles')
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeDisabled()
  expect(fixture.exports).toHaveLength(0)
  await expectOnlyLoopbackRequests(page)
})

test('a consumed export handoff cannot appear on a later independent exports visit', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.addInitScript(() => {
    window.sessionStorage.setItem('wechat-article.export-handoff.v1', JSON.stringify({
      selection: { kind: 'all_matching', query: { accountId: 'account-fixture' } },
      label: 'Prior matching export',
      presentation: { matching: { total: 4, accountName: 'Unique initial handoff account' } }
    }))
  })
  await page.goto('/exports')

  await expect(page.locator('.export-scope-summary').first()).toContainText('Unique initial handoff account')
  const initialWorkflowURL = page.url()
  await expect(page).toHaveURL(/\/exports\?flow=[a-f0-9]{32}&scope=matching$/)
  await page.reload()
  await expect(page.locator('.export-scope-summary').first()).toContainText('Unique initial handoff account')
  expect(page.url()).toBe(initialWorkflowURL)
  await page.getByRole('link', { name: 'Accounts' }).click()
  await expect(page.getByRole('heading', { name: 'Accounts' })).toBeVisible()
  await page.getByRole('link', { name: 'Exports' }).click()
  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()

  await expect(page.locator('.export-scope-summary')).toHaveCount(0)
  await expect(page.getByRole('radio', { name: 'Selected articles' })).toBeChecked()
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeDisabled()
  await expectOnlyLoopbackRequests(page)
})

test('article export selector searches past an initial page and queues its opaque ID', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/exports')

  await selectRemoteSelectorOption(page, 'Selected articles', 'Later sanitized article')
  await expect(page.getByRole('status').filter({ hasText: 'Later sanitized article' })).toBeVisible()
  await continueToExportDestination(page)
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  await expect.poll(() => fixture.exports).toHaveLength(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({ selection: { kind: 'explicit_ids', articleIds: ['article-beyond-first-page'] } })
  await expectOnlyLoopbackRequests(page)
})

test('saved-query export scope filters locally, selects, and clears', async ({ page }) => {
  const fixture = await installLoopbackFixture(page, {
    savedQueries: [
      { name: 'Sanitized recent', query: { keyword: 'Sanitized' } },
      { name: 'Queued fixture', query: { state: 'queued' } }
    ]
  })
  await page.goto('/exports')
  await page.locator('input[type="radio"][value="savedQuery"]').evaluate((element) => (element as HTMLInputElement).click())

  const savedQuery = page.getByRole('combobox', { name: 'Saved query', exact: true })
  await expect.poll(() => fixture.requests.filter((request) => request === 'GET /api/v1/saved-queries').length).toBeGreaterThan(0)
  await savedQuery.evaluate((element) => {
    const input = element as HTMLInputElement
    const nativeSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    nativeSetter?.call(input, 'queued')
    input.dispatchEvent(new InputEvent('input', { bubbles: true, data: 'queued', inputType: 'insertText' }))
  })
  const savedQueryRequestsBeforeFiltering = fixture.requests.filter((request) => request === 'GET /api/v1/saved-queries').length
  const queued = page.getByRole('option').filter({ hasText: 'Queued fixture' })
  await expect(queued).toBeVisible()
  expect(fixture.requests.filter((request) => request === 'GET /api/v1/saved-queries')).toHaveLength(savedQueryRequestsBeforeFiltering)

  await queued.evaluate((element) => (element as HTMLElement).click())
  await expect(page.locator('.export-scope-summary')).toContainText('Saved query Queued fixture')
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeEnabled()
  await page.getByRole('button', { name: 'Clear Saved query' }).evaluate((element) => (element as HTMLButtonElement).click())
  await expect(savedQuery).toHaveValue('')
  await expect(page.locator('.export-scope-summary')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeDisabled()
  await expectOnlyLoopbackRequests(page)
})

test('article export selector selects and clears multiple remote articles without exposing IDs', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/exports')

  await selectRemoteSelectorOption(page, 'Selected articles', 'Sanitized article one')
  await selectRemoteSelectorOption(page, 'Selected articles', 'Sanitized article two')
  const selectedArticles = page.getByRole('combobox', { name: 'Selected articles', exact: true })
  await expect(selectedArticles).toHaveAttribute('placeholder', '2 selected')
  await expect(page.locator('.export-scope-summary')).toContainText('2 selected articles')
  await expect(page.locator('.export-scope-summary')).toContainText('Sanitized article one')
  await expect(page.locator('.export-scope-summary')).toContainText('Sanitized article two')
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeEnabled()
  await expect(page.locator('body')).not.toContainText('article-fixture-1')
  await expect(page.locator('body')).not.toContainText('article-fixture-2')

  await page.getByRole('button', { name: 'Clear Selected articles' }).evaluate((element) => (element as HTMLButtonElement).click())
  await expect(selectedArticles).toHaveAttribute('placeholder', 'Selected articles')
  await expect(page.locator('.export-scope-summary')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeDisabled()
  await expectOnlyLoopbackRequests(page)
})

test('article table presents account names rather than internal account IDs', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  const table = page.getByRole('table')
  await expect(table.getByRole('cell', { name: 'Fixture Account', exact: true }).first()).toBeVisible()
  await expect(table).not.toContainText('account-fixture')
  await expectOnlyLoopbackRequests(page)
})

test('article table reserves flexible width for titles and supports truncation', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  const title = page.getByRole('button', { name: 'Sanitized article one', exact: true })
  await expect(title).toBeVisible()
  await expect(title).toHaveCSS('text-overflow', 'ellipsis')
  const dimensions = await page.evaluate(() => {
    const titleCell = document.querySelector('.article-title-cell')?.getBoundingClientRect().width ?? 0
    const accountCell = document.querySelector('.article-account-cell')?.getBoundingClientRect().width ?? 0
    return { titleCell, accountCell }
  })
  expect(dimensions.titleCell).toBeGreaterThan(dimensions.accountCell)
  await expectOnlyLoopbackRequests(page)
})

test('account discovery keeps fakeid technical while preserving the selected candidate save contract', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/login')
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await page.getByRole('button', { name: 'Complete login' }).click()
  await expect(page.getByText('Authenticated', { exact: true })).toBeVisible()
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Account fakeid' })).toBeHidden()
  await expect(page.locator('body')).not.toContainText('fixture-account')
  await page.getByRole('textbox', { name: 'Search discovery' }).fill('fixture')
  await page.getByRole('button', { name: 'Discover account' }).click()
  const results = page.getByRole('region', { name: 'Discovery results' })
  await expect(results.getByText('Discovered Fixture Account', { exact: true })).toBeVisible()
  await expect(results).not.toContainText('fixture-discovered')
  await expect(results).not.toContainText('discovery-opaque-id')
  await results.getByRole('button', { name: 'Use candidate' }).click()

  await expect(page.getByRole('textbox', { name: 'Account name' })).toHaveValue('Discovered Fixture Account')
  await expect(page.getByRole('textbox', { name: 'Alias' })).toHaveValue('discovered')
  await expect(page.getByRole('textbox', { name: 'Account fakeid' })).toBeHidden()
  await page.getByRole('button', { name: 'Save account' }).click()
  await expect.poll(() => fixture.savedAccounts).toEqual([{ fakeid: 'fixture-discovered', name: 'Discovered Fixture Account', alias: 'discovered' }])
  await expect(page.getByText('Saved Discovered Fixture Account. You can now start synchronization.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync selected account' })).toBeEnabled()
  await expect(page.getByText('Uses the latest local sync state to refresh new and changed article-list records.')).toBeVisible()
  await page.getByRole('button', { name: 'Sync selected account' }).click()
  await expect.poll(() => fixture.accountSyncs).toEqual([{ path: '/api/v1/accounts/account-discovered/sync', incremental: true }])
  await selectStaticSelectorOption(page, 'Synchronization mode', 'Full')
  await expect(page.getByText('Fetches the available article list without relying on the local sync boundary.')).toBeVisible()
  await page.getByRole('button', { name: 'Sync selected account' }).click()
  await expect.poll(() => fixture.accountSyncs).toEqual([
    { path: '/api/v1/accounts/account-discovered/sync', incremental: true },
    { path: '/api/v1/accounts/account-discovered/sync', incremental: false }
  ])
  await expectOnlyLoopbackRequests(page)
})

test('account discovery asks for a WeChat sign-in before searching and surfaces a failed search', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  // Unauthenticated: the search box is hidden behind a sign-in call-to-action.
  await expect(page.getByRole('textbox', { name: 'Search discovery' })).toBeHidden()
  await expect(page.getByRole('button', { name: 'Sign in to WeChat' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('account discovery surfaces an explicit error when the session expires mid-search', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await page.getByRole('button', { name: 'Complete login' }).click()
  await expect(page.getByText('Authenticated', { exact: true })).toBeVisible()
  // Simulate the session expiring after the drawer opened authenticated.
  await page.route(/\/api\/v1\/accounts\/search(\?.*)?$/, (route) => route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'authentication_required', message: 'workspace session must be authenticated' } }) }))
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  await page.getByRole('textbox', { name: 'Search discovery' }).fill('fixture')
  await page.getByRole('button', { name: 'Discover account' }).click()
  await expect(page.getByText('The WeChat session may have expired. Sign in again to search for accounts.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign in again' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('account resolve fills the draft from an article link when authenticated', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await page.getByRole('button', { name: 'Complete login' }).click()
  await expect(page.getByText('Authenticated', { exact: true })).toBeVisible()
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  await page.getByRole('button', { name: 'Or paste an article link' }).click()
  await page.getByRole('textbox', { name: 'Article URL' }).fill('https://mp.weixin.qq.com/s/article-fixture')
  await page.getByRole('button', { name: 'Resolve account' }).click()

  await expect(page.getByRole('textbox', { name: 'Account name' })).toHaveValue('Resolved Account')
  await expect(page.getByRole('textbox', { name: 'Alias' })).toHaveValue('resolved')
  await expect(page.locator('body')).not.toContainText('fixture-resolved')
  await expectOnlyLoopbackRequests(page)
})

test('account resolve degrades to a name-only preview when not signed in', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  await page.getByRole('button', { name: 'Or paste an article link' }).click()
  await page.getByRole('textbox', { name: 'Article URL' }).fill('https://mp.weixin.qq.com/s/article-fixture')
  await page.getByRole('button', { name: 'Resolve account' }).click()

  await expect(page.getByText('Detected Resolved Account. Sign in to save the complete account record.')).toBeVisible()
  // The resolve panel surfaces its own sign-in button alongside the detected name.
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Sign in to WeChat' })).toHaveCount(2)
  await expectOnlyLoopbackRequests(page)
})

test('import success opens persistent task detail with the full ID in technical details', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/ingest/url', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      id: 'job-import-1234567890abcdef',
      kind: 'article_download',
      label: 'Import article',
      state: 'queued',
      createdAt: '2026-07-24T09:30:00.000Z',
      updatedAt: '2026-07-24T09:30:00.000Z',
      permittedActions: []
    })
  }))
  await page.goto('/import')

  await page.getByRole('textbox', { name: 'Article URL' }).fill('https://mp.weixin.qq.com/s/sanitized')
  const handoff = page.waitForURL('**/jobs?job=job-import-1234567890abcdef')
  await page.getByRole('button', { name: 'Import URL' }).click()
  await handoff
  await expect(page.getByRole('heading', { name: 'Task detail' })).toBeVisible()
  await expect(page.getByText('Import article', { exact: true }).last()).toBeVisible()
  await page.getByRole('button', { name: 'Technical details' }).click()
  await expect(page.getByRole('code').filter({ hasText: 'job-import-1234567890abcdef' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Copy ID' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('article metrics and resource details stay bounded and sanitized while resource actions queue jobs', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized article one' }))
  const details = page.getByRole('button', { name: 'Details' })
  await details.click()
  const detailDialog = page.getByRole('dialog')
  await expect(detailDialog).toBeVisible()
  await expect(detailDialog.getByRole('button', { name: 'Close dialog' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(detailDialog.locator('.presentation-detail-body')).toContainText('Resource availability')
  await expect(page.getByRole('heading', { name: 'Resource availability' })).toBeVisible()
  await expect(page.getByText('4 resources · 3 available · 1 missing')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Article details' })).toBeVisible()
  await expect(page.getByText('Reads', { exact: true })).toBeVisible()
  await expect(page.getByText('120', { exact: true })).toBeVisible()
  await expect(page.getByText('Old likes', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Latest metrics' }).getByText('3', { exact: true })).toBeVisible()
  await expect(page.getByText('image #1 · available locally')).toBeVisible()
  await expect(page.getByText('image #2 · missing locally')).toBeVisible()
  await expect(page.getByText('Showing 2 of 4 resources.')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('https://sensitive.example/resource')
  await expect(page.locator('body')).not.toContainText('sensitive-resource-digest')
  await expect(page.locator('body')).not.toContainText('/sensitive/resource/path')
  await expect(page.locator('body')).not.toContainText('sensitive-resource-id')
  await expect(page.locator('body')).not.toContainText('sensitive-credential')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('heading', { name: 'Article details' })).toBeHidden()
  await page.locator('.article-selection-more > summary').click()
  await page.getByRole('button', { name: 'Complete missing resources' }).click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([{ articleIds: ['article-fixture-1'], force: false }])
  await page.getByRole('button', { name: 'Re-download resources' }).click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([
    { articleIds: ['article-fixture-1'], force: false },
    { articleIds: ['article-fixture-1'], force: true }
  ])
  await expectOnlyLoopbackRequests(page)
})

test('account manifest controls export selected records and import locally without retaining file details', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/accounts')
  const toolbar = page.locator('.presentation-data-table-toolbar')

  const exportButton = toolbar.getByRole('button', { name: 'Download selected account manifest' })
  await expect(exportButton).toBeDisabled()
  await page.getByRole('checkbox', { name: 'Select Fixture Account' }).check()
  const exportRequest = page.waitForRequest((request) => request.method() === 'GET' && request.url().endsWith('/api/v1/accounts/manifest?accountId=account-fixture'))
  await exportButton.click()
  await exportRequest

  const uploadRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/accounts/manifest/upload'))
  const importRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/accounts/manifest/import'))
  const manifestInput = toolbar.getByRole('button', { name: 'Import account manifest' }).locator('input[type="file"]')
  await manifestInput.setInputFiles({ name: 'private-accounts.json', mimeType: 'application/json', buffer: Buffer.from('{"schemaVersion":1,"accounts":[]}') })
  const upload = await uploadRequest
  expect(await upload.headerValue('content-type')).toContain('multipart/form-data')
  expect(upload.postData()).toContain('name="manifest"')
  expect(await manifestInput.inputValue()).toBe('')
  expect((await importRequest).postDataJSON()).toEqual({ uploadHandle: 'account-manifest-upload-fixture' })
  await expect(page.getByRole('status').filter({ hasText: 'Account manifest imported: 1 added, 2 merged, 3 unchanged.' })).toBeVisible()
  expect(fixture.accountManifestImports).toEqual([{ uploadHandle: 'account-manifest-upload-fixture' }])
  await expectOnlyLoopbackRequests(page)
})

test('advanced article query and export handoff preserve typed local selections', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await selectRemoteSelectorOption(page, 'Account', 'Fixture Account')
  await page.getByRole('button', { name: 'More filters' }).click()
  await page.getByRole('spinbutton', { name: 'Minimum reads' }).fill('10')
  await page.getByRole('button', { name: 'Apply filters' }).click()
  await expect.poll(() => fixture.requests.some((request) => request === 'GET /api/v1/articles')).toBe(true)
  await page.getByRole('button', { name: 'Export current matches' }).click()
  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: '26 selected articles' })).toBeVisible()
  await expect(page.locator('.export-scope-summary p')).toContainText('Account: Fixture Account')
  await expect(page.locator('body')).not.toContainText('account-fixture')
  await continueToExportDestination(page)
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  await expect.poll(() => fixture.exports.length).toBe(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({ selection: { kind: 'all_matching', query: { accountId: 'account-fixture', readMin: 10 } } })
  await expectOnlyLoopbackRequests(page)
})

test('remote account and album selectors search server pages and keep opaque IDs internal', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')

  await selectRemoteSelectorOption(page, 'Account', 'Later Fixture Account')
  await page.getByRole('button', { name: 'More filters' }).click()
  await selectRemoteSelectorOption(page, 'Album', 'Later fixture album')
  await page.getByRole('button', { name: 'Apply filters' }).click()

  await page.getByRole('button', { name: 'Export current matches' }).click()
  await expect(page.getByRole('status').filter({ hasText: '26 selected articles' })).toBeVisible()
  await expect(page.locator('.export-scope-summary p')).toContainText('Account: Later Fixture Account')
  await expect(page.locator('.export-scope-summary p')).toContainText('Album: Later fixture album')
  await expect(page.locator('body')).not.toContainText('account-beyond-first-page')
  await expect(page.locator('body')).not.toContainText('album-beyond-first-page')
  await continueToExportDestination(page)
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  expect(JSON.parse(fixture.exports.at(-1) ?? '{}')).toMatchObject({ selection: { kind: 'all_matching', query: { accountId: 'account-beyond-first-page', albumId: 'album-beyond-first-page' } } })
  await expectOnlyLoopbackRequests(page)
})

test('selected albums export handoff queues opaque stable album IDs', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized album' }))
  await expect(page.getByRole('checkbox', { name: 'Select album-fixture-1' })).toHaveCount(0)
  await expect(page.getByRole('table')).not.toContainText('album-fixture-1')
  const exportButton = page.getByRole('button', { name: 'Export selected albums' })
  await expect(exportButton).toBeEnabled()
  await page.getByRole('button', { name: 'Next page' }).click()
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized album two' }))
  await expect(page.getByRole('checkbox', { name: 'Select album-fixture-2' })).toHaveCount(0)
  await expect(page.getByRole('table')).not.toContainText('album-fixture-2')
  await exportButton.click()
  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page.getByRole('radio', { name: '2 selected albums' })).toBeChecked()
  await expect(page.getByRole('status').filter({ hasText: '2 selected albums' })).toBeVisible()
  await continueToExportDestination(page)
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  await expect.poll(() => fixture.exports.length).toBe(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({ selection: { kind: 'album_ids', albumIds: ['album-fixture-1', 'album-fixture-2'] } })
  await expectOnlyLoopbackRequests(page)
})

test('album handoffs are staged through the one-album scope before queueing', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.addInitScript((selection) => {
    window.sessionStorage.setItem('wechat-article.export-handoff.v1', JSON.stringify({ selection, label: 'Sanitized album' }))
  }, { kind: 'album', albumId: 'album-fixture-1' })
  await page.goto('/exports')
  await expect(page.getByRole('radio', { name: 'One album' })).toBeChecked()
  await expect(page.getByRole('status').filter({ hasText: 'Sanitized album' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Continue to format' })).toBeEnabled()
  await expect.poll(() => fixture.exports).toHaveLength(0)
  await expectOnlyLoopbackRequests(page)
})

test('album selections persist across server pages and queue one multi-album traversal', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')

  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized album' }))
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('table').getByText('Sanitized album two', { exact: true })).toBeVisible()
  await expect(page.getByText('1 selected', { exact: false })).toBeVisible()

  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized album two' }))
  await expect(page.getByText('2 selected', { exact: false })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Traverse selected albums' })).toBeEnabled()
  await page.getByRole('button', { name: 'Traverse and batch download' }).click()
  await expect.poll(() => fixture.albumTraversals).toEqual([{ albumIds: ['album-fixture-1', 'album-fixture-2'], order: 'forward', download: true }])
  await expect(page.getByRole('link', { name: 'Exports' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('album filters stay server-paginated and traversal sends the selected order', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')
  await selectRemoteSelectorOption(page, 'Account', 'Fixture Account')
  await page.getByRole('textbox', { name: 'Album keyword' }).fill('Sanitized')
  await expect.poll(() => fixture.requests.filter((request) => request === 'GET /api/v1/albums').length).toBeGreaterThan(1)
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized album' }))
  await selectStaticSelectorOption(page, 'Traversal order', 'Reverse')
  await page.getByRole('button', { name: 'Traverse and batch download' }).click()
  await expect.poll(() => fixture.albumTraversals).toEqual([{ accountId: 'account-fixture', order: 'reverse', download: true }])
  await expectOnlyLoopbackRequests(page)
})

test('account deletion requires a typed exact proof and returns focus when cancelled', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/accounts')
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Fixture Account' }))
  const deleteButton = page.getByRole('button', { name: 'Delete selected account' })
  await deleteButton.click()
  const dialog = page.getByRole('alertdialog', { name: 'Delete selected accounts' })
  const confirmation = dialog.getByRole('textbox', { name: 'Exact confirmation to delete these accounts' })
  const confirmButton = dialog.getByRole('button', { name: 'Delete accounts' })
  await expect(confirmation).toBeVisible()
  await expect(confirmButton).toBeDisabled()
  await confirmation.fill('delete-accounts:wrong')
  await expect(confirmButton).toBeDisabled()
  await dialog.getByRole('button', { name: 'Keep accounts' }).click()
  await expect(dialog).toBeHidden()
  await expect(deleteButton).toBeFocused()
  await deleteButton.click()
  await confirmation.fill('delete-accounts:account-fixture')
  const request = page.waitForRequest((candidate) => candidate.method() === 'DELETE' && candidate.url().endsWith('/api/v1/accounts'))
  await confirmButton.click()
  expect((await request).postDataJSON()).toEqual({ ids: ['account-fixture'], confirm: 'delete-accounts:account-fixture' })
  await expect.poll(() => fixture.accountDeletions).toEqual([{ ids: ['account-fixture'], confirm: 'delete-accounts:account-fixture' }])
  await expectOnlyLoopbackRequests(page)
})

test('saved queries are created, updated, and deleted with typed scoped confirmation', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/saved-queries')
  await page.getByRole('textbox', { name: 'Query name' }).fill('fixture recent')
  await page.getByRole('textbox', { name: 'Search articles' }).fill('fixture')
  await page.getByRole('button', { name: 'Save query' }).click()
  await expect(page.getByText('Saved query “fixture recent”.')).toBeVisible()
  await expect(page.getByRole('cell', { name: 'fixture recent', exact: true })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Select fixture recent' }).check()
  await page.getByRole('button', { name: 'Load selected query' }).click()
  await page.getByRole('textbox', { name: 'Search articles' }).fill('updated')
  await page.getByRole('button', { name: 'Save query' }).click()
  await expect(page.getByRole('button', { name: 'Load selected query' })).toBeEnabled()
  await page.getByRole('button', { name: 'Delete selected query' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Delete saved query' })
  await expect(dialog).toBeVisible()
  const confirmation = dialog.getByRole('textbox', { name: 'Exact confirmation to delete this saved query' })
  const deleteQuery = dialog.getByRole('button', { name: 'Delete query' })
  await expect(confirmation).toBeVisible()
  await expect(deleteQuery).toBeDisabled()
  await confirmation.fill('delete-saved-query:wrong')
  await expect(deleteQuery).toBeDisabled()
  await confirmation.fill('delete-saved-query:fixture recent')
  const deletion = page.waitForRequest((candidate) => candidate.method() === 'DELETE' && candidate.url().endsWith('/api/v1/saved-queries'))
  await deleteQuery.click()
  expect((await deletion).postDataJSON()).toEqual({ name: 'fixture recent', confirm: 'delete-saved-query:fixture recent' })
  await expect(page.getByText('Deleted saved query “fixture recent”.')).toBeVisible()
  expect(fixture.requests.filter((request) => request.includes('/api/v1/saved-queries')).length).toBeGreaterThanOrEqual(4)
  await expectOnlyLoopbackRequests(page)
})

test('job controls require typed proofs and disable unavailable actions', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/jobs')
  await expect(page.getByRole('table').getByText('Running', { exact: true })).toBeVisible()
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Export' }))
  await expect(page.getByRole('checkbox', { name: 'Select job-fixture-1' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'Task detail' })).toBeVisible()
  await expect(page.locator('table')).not.toContainText('job-fixture-1')
  await expect(page.getByText('Sanitized local progress', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Resume selected task' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Retry selected task' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Refresh detail' }).click()
  await page.getByRole('button', { name: 'Pause selected task' }).click()
  const pauseConfirmation = page.getByRole('alertdialog', { name: 'Pause selected task' })
  await expect(pauseConfirmation).toBeVisible()
  const pauseProof = pauseConfirmation.getByRole('textbox', { name: 'Exact confirmation for this task action' })
  const pauseButton = pauseConfirmation.getByRole('button', { name: 'Pause selected task' })
  await expect(pauseProof).toBeVisible()
  await expect(pauseButton).toBeDisabled()
  await pauseProof.fill('pause-job:job-fixture-1')
  const pauseRequest = page.waitForRequest((candidate) => candidate.method() === 'POST' && candidate.url().endsWith('/api/v1/jobs/job-fixture-1/pause'))
  await pauseButton.click()
  expect((await pauseRequest).postDataJSON()).toEqual({ confirm: 'pause-job:job-fixture-1' })
  await expect.poll(() => fixture.controls.length).toBe(1)
  expect(fixture.controls[0]).toEqual({ path: '/api/v1/jobs/job-fixture-1/pause', confirmation: 'pause-job:job-fixture-1' })

  const cancelTrigger = page.getByRole('button', { name: 'Cancel selected task' })
  await cancelTrigger.click()
  const cancelConfirmation = page.getByRole('alertdialog', { name: 'Cancel selected task' })
  await expect(cancelConfirmation).toBeVisible()
  await cancelConfirmation.getByRole('textbox', { name: 'Exact confirmation for this task action' }).press('Escape')
  await expect(cancelConfirmation).toBeHidden()
  await expect(page.getByRole('dialog', { name: 'Task detail' })).toBeVisible()
  await expect(cancelTrigger).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: 'Task detail' })).toBeHidden()
  await expectOnlyLoopbackRequests(page)
})

test('sanitized export flow authorizes a directory, downloads an artifact, opens output, and verifies output', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await installExportArtifactFixture(page)
  await page.goto('/exports')
  await selectRemoteSelectorOption(page, 'Selected articles', 'Sanitized article one')
  await clickButton(page, 'Continue to format')
  await expect(page.getByRole('heading', { name: 'Format and options' })).toBeVisible()
  await selectStaticSelectorOption(page, 'Format', 'HTML')
  await expect(page.getByRole('group', { name: 'HTML content options' })).toBeVisible()
  await expect(page.getByRole('group', { name: 'HTML content options' }).getByRole('checkbox', { name: 'Include locally stored comments' })).toBeVisible()
  await expect(page.getByRole('group', { name: 'HTML content options' })).not.toContainText('Include article content where supported')
  const includeComments = page.getByRole('checkbox', { name: 'Include locally stored comments' })
  await includeComments.evaluate((element) => (element as HTMLInputElement).click())
  await expect(includeComments).toBeChecked()
  await selectStaticSelectorOption(page, 'Resource handling', 'Strict (fail when resources cannot be made local)')
  const batchArchive = page.getByRole('textbox', { name: 'HTML batch archive file name' })
  await setInputValue(batchArchive, 'articles.zip')
  await expect(batchArchive).toHaveValue('articles.zip')
  await clickButton(page, 'Continue to destination')
  await expect(page.getByRole('heading', { name: 'Destination and confirmation' })).toBeVisible()
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  await expect(page.getByText('Authorized directory: Sanitized exports')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Queue export' })).toBeEnabled()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  expect(fixture.exports).toHaveLength(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({
    directoryToken: 'dir-sanitized', format: 'html',
    options: { formatOptions: { comments: true, htmlResourcePolicy: 'strict', htmlBatchArchive: 'articles.zip' } }
  })
  expect(JSON.parse(fixture.exports[0]).options.formatOptions).not.toHaveProperty('content')
  expect(JSON.parse(fixture.exports[0]).options.formatOptions).not.toHaveProperty('metadata')
  await page.goto('/exports')
  const exportRecord = page.getByRole('checkbox', { name: /^Select export / })
  await exportRecord.evaluate((element) => (element as HTMLInputElement).click())
  await expect(exportRecord).toBeChecked()
  await page.getByRole('button', { name: 'View manifest' }).click()
  await expect(page.getByRole('cell', { name: 'sanitized-article.md', exact: true })).toBeVisible()
  await expect(page.locator('body')).not.toContainText('/private/export/root/sanitized-article.md')
  const manifestDetails = page.locator('.manifest-detail').getByRole('button', { name: 'Technical details' }).last()
  await manifestDetails.evaluate((element) => (element as HTMLElement).click())
  await expect(page.locator('.manifest-detail .presentation-technical-list').last().getByRole('code')).toContainText('sanitized-article.md')
  await page.getByRole('heading', { name: 'Artifact download and output folder' }).scrollIntoViewIfNeeded()
  const downloadLink = page.getByRole('link', { name: 'Download' })
  await expect(downloadLink).toHaveAttribute('href', '/api/v1/exports/export-fixture-1/artifact?artifactId=artifact-fixture-1')
  const download = await Promise.all([page.waitForEvent('download'), downloadLink.click()])
  expect(download[0].suggestedFilename()).toBe('sanitized-article.md')
  const openConfirmation = page.getByRole('textbox', { name: 'Confirmation value' })
  await openConfirmation.evaluate((element) => {
    const input = element as HTMLInputElement
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    if (!setValue) throw new Error('input value setter is unavailable')
    setValue.call(input, 'open-export-output:export-fixture-1')
    input.dispatchEvent(new InputEvent('input', { bubbles: true, data: 'open-export-output:export-fixture-1', inputType: 'insertText' }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await expect(openConfirmation).toHaveValue('open-export-output:export-fixture-1')
  await expect(page.getByRole('button', { name: 'Open output folder' })).toBeEnabled()
  const openRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/exports/export-fixture-1/open'))
  await page.getByRole('button', { name: 'Open output folder' }).click()
  await expect((await openRequest).postDataJSON()).toEqual({ confirm: 'open-export-output:export-fixture-1' })
  await expect(page.getByText('The selected export output folder was opened.')).toBeVisible()
  await page.getByRole('button', { name: 'Verify export' }).click()
  await expect(page.getByText('Valid: 1 output verified.')).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('export verification keeps raw diagnostics inside technical details', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/exports/export-fixture-1/verify', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      apiVersion: 'v1',
      data: {
        exportId: 'export-fixture-1',
        valid: false,
        verifiedOutputs: 0,
        issues: [{
          path: '/private/export/root/sensitive-article.md',
          message: 'private verification diagnostic',
          expected: 'private expected checksum',
          actual: 'private actual checksum'
        }]
      }
    })
  }))
  await page.goto('/exports')
  const exportRecord = page.getByRole('checkbox', { name: /^Select export / })
  await exportRecord.evaluate((element) => (element as HTMLInputElement).click())
  await page.getByRole('button', { name: 'Verify export' }).click()

  const verification = page.locator('.verification-result')
  await expect(verification).toContainText('Verification found issues after checking 0 outputs.')
  const publicIssues = verification.getByRole('list').first()
  await expect(publicIssues).toContainText('Output 1')
  await expect(publicIssues).not.toContainText('private verification diagnostic')
  await expect(publicIssues).not.toContainText('/private/export/root/sensitive-article.md')
  await expect(publicIssues).not.toContainText('private expected checksum')
  await expect(publicIssues).not.toContainText('private actual checksum')

  await verification.getByRole('button', { name: 'Technical details' }).evaluate((element) => {
    element.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }))
  })
  await expect(verification.locator('.presentation-technical-list code')).toContainText('private verification diagnostic')
  await expectOnlyLoopbackRequests(page)
})

test('every supported export format queues only its allowed local options', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  const cases = [
    { format: 'markdown', options: { metadata: true, comments: false } },
    { format: 'html', options: { comments: false, htmlResourcePolicy: 'best-effort' } },
    { format: 'text', options: { metadata: true, comments: false } },
    { format: 'json', options: { content: true, metadata: true, comments: false } },
    { format: 'xlsx', options: { content: true } },
    { format: 'docx', options: { comments: false } },
    { format: 'pdf', options: { comments: false } }
  ] as const

  for (const [index, testCase] of cases.entries()) {
    await page.goto('/exports')
    await selectRemoteSelectorOption(page, 'Selected articles', 'Sanitized article one')
    await clickButton(page, 'Continue to format')
    await selectStaticSelectorOption(page, 'Format', testCase.format.toUpperCase())
    await clickButton(page, 'Continue to destination')
    await page.getByRole('button', { name: 'Authorize default directory' }).click()
    await expect(page.getByRole('button', { name: 'Queue export' })).toBeEnabled()
    const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
    await page.getByRole('button', { name: 'Queue export' }).click()
    await jobHandoff
    await expect(page.getByRole('heading', { name: 'Task detail' })).toBeVisible()
    await expect.poll(() => fixture.exports).toHaveLength(index + 1)

    const request = JSON.parse(fixture.exports[index])
    expect(request).toMatchObject({
      directoryToken: 'dir-sanitized',
      format: testCase.format,
      options: { formatOptions: testCase.options }
    })
    expect(request.options.formatOptions).toEqual(testCase.options)
  }

  await expectOnlyLoopbackRequests(page)
})

test('import job creation navigates directly to the selected task', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/ingest/url', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(createdJob('job-import-fixture', 'import')) }))
  await page.goto('/import')

  await page.getByRole('textbox', { name: 'Article URL' }).fill('https://mp.weixin.qq.com/s/sanitized-import')
  const importHandoff = page.waitForURL('**/jobs?job=job-import-fixture')
  await page.getByRole('button', { name: 'Import URL' }).click()
  await importHandoff
  await expect(page.getByRole('heading', { name: 'Task detail' })).toBeVisible()
  await expect(page.getByText('Import article', { exact: true }).last()).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

function createdJob(id: string, kind: string) {
  return { id, kind, state: 'queued', profile: 'fixture-profile', createdAt: '2026-07-24T09:30:00.000Z', updatedAt: '2026-07-24T09:30:00.000Z', counts: { completed: 0, total: 1 } }
}

async function continueToExportDestination(page: import('@playwright/test').Page) {
  await clickButton(page, 'Continue to format')
  await expect(page.getByRole('heading', { name: 'Format and options' })).toBeVisible()
  await clickButton(page, 'Continue to destination')
  await expect(page.getByRole('heading', { name: 'Destination and confirmation' })).toBeVisible()
}

async function chooseExportScope(page: import('@playwright/test').Page, value: 'articles' | 'account') {
  await page.locator(`input[type="radio"][value="${value}"]`).evaluate((element) => (element as HTMLInputElement).click())
}

async function toggleCheckbox(checkbox: import('@playwright/test').Locator) {
  await checkbox.evaluate((element) => (element as HTMLInputElement).click())
}

async function setInputValue(input: import('@playwright/test').Locator, value: string) {
  await input.evaluate((element, nextValue) => {
    const nativeSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    nativeSetter?.call(element, nextValue)
    element.dispatchEvent(new Event('input', { bubbles: true }))
    element.dispatchEvent(new Event('change', { bubbles: true }))
  }, value)
}

async function clickButton(page: import('@playwright/test').Page, name: string) {
  await page.getByRole('button', { name, exact: true }).evaluate((element) => (element as HTMLButtonElement).click())
}

test('sanitized settings and storage maintenance flows do not reveal secrets', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'Settings and maintenance' })).toBeVisible()
  await page.getByRole('spinbutton', { name: 'Download concurrency' }).fill('3')
  await page.getByRole('button', { name: 'Save preferences' }).click()
  await expect(page.getByText('Preferences saved.')).toBeVisible()
  expect(fixture.preferencePatches).toHaveLength(1)
  await page.getByRole('button', { name: 'Create backup' }).click()
  await expect(page.getByRole('textbox', { name: 'Backup ID' })).toHaveValue('backup-fixture')
  await page.getByRole('button', { name: 'Verify backup' }).click()
  await expect(page.getByText('Backup verification passed.')).toBeVisible()
  await page.getByRole('button', { name: 'Generate GC plan' }).click()
  await page.getByRole('textbox', { name: 'One-time exact confirmation' }).fill('apply-gc-fixture')
  await page.getByRole('button', { name: 'Apply this plan once' }).click()
  await expect(page.getByText('GC completed.')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('real-cookie')
  await expectOnlyLoopbackRequests(page)
})

test('export defaults persist locally without changing sync settings or starting an export', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  await selectStaticSelectorOption(page, 'Collision policy', 'Append suffix', 'Replace existing output')
  await page.getByRole('checkbox', { name: 'Excel: include article content' }).uncheck()
  await page.getByRole('checkbox', { name: 'JSON: include article content' }).uncheck()
  await page.getByRole('checkbox', { name: 'JSON: include stored comments' }).check()
  await page.getByRole('checkbox', { name: 'HTML: include stored comments' }).check()
  await page.getByRole('button', { name: 'Save preferences' }).click()

  await expect(page.getByText('Preferences saved.')).toBeVisible()
  expect(fixture.preferencePatches).toEqual([expect.objectContaining({
    export: {
      namingTemplate: '{title}',
      maximumNameBytes: 180,
      collisionPolicy: 'replace',
      excelIncludeContent: false,
      jsonIncludeContent: false,
      jsonIncludeComments: true,
      htmlIncludeComments: true
    }
  })])
  expect(fixture.preferencePatches[0]).not.toHaveProperty('sync')
  expect(fixture.exports).toHaveLength(0)
  await expect(page.locator('body')).not.toContainText('Sanitized exports')
  await expectOnlyLoopbackRequests(page)
})

test('credential validation checks write-only values before allowing import', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')
  await page.getByRole('textbox', { name: 'Business ID (write-only)' }).fill('biz-secret')
  await page.getByRole('textbox', { name: 'UIN (write-only)' }).fill('uin-secret')
  await page.getByRole('textbox', { name: 'Key (write-only)' }).fill('key-secret')
  await page.getByRole('textbox', { name: 'Pass ticket (write-only)' }).fill('ticket-secret')
  await page.getByRole('textbox', { name: 'WAP SID2 (write-only)' }).fill('sid-secret')
  await page.getByRole('textbox', { name: 'App message token (write-only)' }).fill('token-secret')
  const importButton = page.getByRole('button', { name: 'Import credential' })
  await expect(importButton).toBeDisabled()
  const validationRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/settings/credentials/validate'))
  await page.getByRole('button', { name: 'Validate credential' }).click()
  expect((await validationRequest).postDataJSON()).toMatchObject({ biz: 'biz-secret', uin: 'uin-secret', key: 'key-secret', passTicket: 'ticket-secret', wapSid2: 'sid-secret', appMsgToken: 'token-secret' })
  await expect(page.getByText('Credential validation passed. You can import these entered values.')).toBeVisible()
  await expect(importButton).toBeEnabled()
  expect(fixture.credentialValidations).toHaveLength(1)
  await expect(page.locator('body')).not.toContainText('/Users/')
  await expectOnlyLoopbackRequests(page)
})

test('saving Chinese display language updates the UI immediately and persists the profile preference', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  await selectStaticSelectorOption(page, 'Display language', 'English', 'Chinese (Simplified)')
  await page.getByRole('button', { name: 'Save preferences' }).click()

  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.getByRole('heading', { name: '设置与维护' })).toBeVisible()
  await expect(page.getByText('偏好设置已保存。')).toBeVisible()
  expect(fixture.preferencePatches).toHaveLength(1)
  expect(fixture.preferencePatches[0]).toMatchObject({ display: { language: 'zh-CN' } })
  await expectOnlyLoopbackRequests(page)
})

test('profile display language takes precedence when the workspace first loads', async ({ page }) => {
  await page.addInitScript(() => window.localStorage.setItem('wechat-article.display.language', 'en'))
  await installLoopbackFixture(page, { displayLanguage: 'zh-CN' })
  await page.goto('/settings')

  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.getByRole('heading', { name: '设置与维护' })).toBeVisible()
  await expect(page.getByRole('combobox', { name: '显示语言' })).toHaveText('简体中文')
  await expectOnlyLoopbackRequests(page)
})

test('settings removal requires localized exact confirmation for credentials and proxies', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  await page.getByRole('button', { name: 'Remove' }).first().click()
  const credentialConfirmation = page.getByRole('textbox', { name: 'Exact confirmation to remove this credential' })
  const removeCredential = page.getByRole('button', { name: 'Remove credential' })
  await expect(removeCredential).toBeDisabled()
  await credentialConfirmation.fill('remove-credential:wrong')
  await expect(removeCredential).toBeDisabled()
  await credentialConfirmation.fill('remove-credential:credential-fixture')
  await expect(removeCredential).toBeEnabled()
  const credentialRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/settings/credentials/remove'))
  await removeCredential.click()
  expect((await credentialRequest).postDataJSON()).toEqual({ id: 'credential-fixture', confirm: 'remove-credential:credential-fixture' })
  await expect.poll(() => fixture.credentialRemovals).toEqual([{ id: 'credential-fixture', confirm: 'remove-credential:credential-fixture' }])

  await page.getByRole('button', { name: 'Remove' }).last().click()
  const proxyConfirmation = page.getByRole('textbox', { name: 'Exact confirmation to remove this proxy route' })
  const removeProxy = page.getByRole('button', { name: 'Remove proxy' })
  await expect(removeProxy).toBeDisabled()
  await proxyConfirmation.fill('remove-proxy:proxy-fixture')
  await expect(removeProxy).toBeEnabled()
  const proxyRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/settings/proxies/proxy-fixture/remove'))
  await removeProxy.click()
  expect((await proxyRequest).postDataJSON()).toEqual({ confirm: 'remove-proxy:proxy-fixture' })
  await expect.poll(() => fixture.proxyRemovals).toEqual([{ confirm: 'remove-proxy:proxy-fixture' }])
  await expect(page.locator('body')).not.toContainText('real-cookie')
  await expect(page.locator('body')).not.toContainText('/Users/')
  await expectOnlyLoopbackRequests(page)
})

test('sanitized diagnostic bundle creation posts no paths and downloads through an opaque handle', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  const createRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/maintenance/diagnostic-bundles'))
  await page.getByRole('button', { name: 'Create diagnostic bundle' }).click()
  expect((await createRequest).postDataJSON()).toEqual({})
  await expect.poll(() => fixture.diagnosticBundleRequests).toEqual([{}])
  await expect(page.getByRole('status').filter({ hasText: 'Diagnostic bundle is ready to download.' })).toBeVisible()
  await expect(page.locator('body')).not.toContainText('/Users/')

  const downloadLink = page.getByRole('link', { name: 'Download diagnostic bundle' })
  await expect(downloadLink).toHaveAttribute('href', '/api/v1/maintenance/diagnostic-bundles/diagnostic_sanitized-handle')
  const download = await Promise.all([page.waitForEvent('download'), downloadLink.click()])
  expect(download[0].suggestedFilename()).toBe('diagnostic-bundle.zip')
  await expectOnlyLoopbackRequests(page)
})

test('sanitized restore stages one archive, prepares explicit confirmation, and closes the workspace', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/settings')
  await expect(page.getByRole('region', { name: 'Restore' }).getByRole('alert')).toContainText('Destructive action')
  await page.getByRole('button', { name: 'Backup archive' }).locator('input[type="file"]').setInputFiles({ name: 'sanitized-backup.wab', mimeType: 'application/octet-stream', buffer: Buffer.from('sanitized restore archive') })
  const uploadRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/maintenance/restore/upload'))
  const prepareRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/maintenance/restore/prepare'))
  await page.getByRole('button', { name: 'Stage archive for restore' }).click()
  const upload = await uploadRequest
  expect(await upload.headerValue('content-type')).toContain('multipart/form-data')
  expect(upload.postData()).toContain('name="archive"')
  expect(upload.postData()).not.toContain('name="uploadHandle"')
  expect((await prepareRequest).postDataJSON()).toEqual({ uploadHandle: 'restore-upload-fixture', conflictPolicy: 'refuse' })
  await expect(page.getByRole('textbox', { name: 'Exact one-time restore confirmation' })).toBeVisible()
  await page.getByRole('textbox', { name: 'Exact one-time restore confirmation' }).fill('confirm-restore-fixture')
  const commitRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/maintenance/restore/commit'))
  await page.getByRole('button', { name: 'Restore and close workspace' }).click()
  expect((await commitRequest).postDataJSON()).toEqual({ preparationId: 'restore-preparation-fixture', confirmation: 'confirm-restore-fixture' })
  await expect(page.getByRole('status').filter({ hasText: 'The local workspace has closed.' })).toContainText('The local workspace has closed. Run wechat-article web again to open it.')
  await expect(page.locator('body')).not.toContainText('/Users/')
  await expectOnlyLoopbackRequests(page)
})

test('failure states remain usable with sanitized local errors', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/articles?**', (route) => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Sanitized fixture unavailable' } }) }))
  await page.goto('/articles')
  await expect(page.getByRole('alert').filter({ hasText: 'The local article API is not available yet.' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('an online reconnect invalidates a failed local snapshot and renders evolved state', async ({ page }) => {
  await installLoopbackFixture(page)
  let snapshotRequests = 0
  await page.route('**/api/v1/events/snapshot', (route) => {
    snapshotRequests += 1
    if (snapshotRequests <= 3) {
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Sanitized snapshot temporarily unavailable' } }) })
    }
    return route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        apiVersion: 'v1',
        data: {
          runtime: { version: 'e2e-sanitized', profileId: 'fixture-profile' },
          session: { state: 'authenticated', accountName: 'Recovered Fixture Account' },
          storage: { databaseAvailable: true, objectStoreReady: true, accounts: 1, articles: 7, albums: 0, jobs: 1, objects: 2, objectBytes: 84 },
          jobs: { items: [], total: 0, offset: 0, limit: 100 },
          observedAt: '2026-07-24T09:31:00.000Z',
          revision: 2
        }
      })
    })
  })

  await page.goto('/')
  await expect(page.getByRole('alert')).toContainText('Live local details are unavailable. Check that the local workspace is still running.')
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await expect(page.getByText('Recovered Fixture Account')).toBeVisible()
  await expect(page.getByText('1 accounts · 7 articles · 0 albums · 1 jobs')).toBeVisible()
  expect(snapshotRequests).toBeGreaterThanOrEqual(3)
  await expectOnlyLoopbackRequests(page)
})
