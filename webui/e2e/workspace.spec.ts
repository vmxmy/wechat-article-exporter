import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installExportArtifactFixture, installLoopbackFixture } from './fixtures/loopback-api'

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
  await page.getByRole('combobox', { name: 'Eligible account' }).selectOption('account-fixture-2')
  await expect(page.getByRole('status').filter({ hasText: 'Switched to Second Fixture Account.' })).toBeVisible()
  expect(fixture.accountSwitches).toEqual(['account-fixture-2'])
  await page.getByRole('button', { name: 'Log out' }).click()
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

test('sanitized account and article selections remain browser-local', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')
  await page.getByRole('checkbox', { name: 'Select account-fixture' }).check()
  await expect(page.getByText('1 selected', { exact: false })).toBeVisible()
  await page.goto('/articles')
  await page.getByRole('checkbox', { name: 'Select Sanitized article one' }).check()
  await expect(page.getByText('1 selected', { exact: false })).toBeVisible()
  await page.getByRole('textbox', { name: 'Search articles' }).fill('Sanitized')
  await expect(page.getByRole('cell', { name: 'Sanitized article one', exact: true })).toBeVisible()
  await expect(page.getByText('0 selected', { exact: false })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('article selections persist across server pages and hand off all selected stable IDs for export', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  await page.getByRole('checkbox', { name: 'Select Sanitized article one' }).check()
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('cell', { name: 'Sanitized article three', exact: true })).toBeVisible()
  await expect(page.getByText('1 selected', { exact: false })).toBeVisible()

  await page.getByRole('checkbox', { name: 'Select Sanitized article three' }).check()
  await expect(page.getByText('2 selected', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Export selected' }).click()

  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Selection: 2 explicit article IDs' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Article IDs' })).toHaveValue('article-fixture-1\narticle-fixture-3')
  await expectOnlyLoopbackRequests(page)
})

test('discovery candidates populate the explicit account save form and choose a synchronization mode', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/accounts')

  await page.getByRole('textbox', { name: 'Search discovery' }).fill('fixture')
  await page.getByRole('button', { name: 'Discover account' }).click()
  const results = page.getByRole('region', { name: 'Discovery results' })
  await expect(results.getByText('Discovered Fixture Account', { exact: true })).toBeVisible()
  await expect(results).not.toContainText('fixture-discovered')
  await expect(results).not.toContainText('discovery-opaque-id')
  await results.getByRole('button', { name: 'Use candidate' }).click()

  await expect(page.getByRole('textbox', { name: 'Account fakeid' })).toHaveValue('fixture-discovered')
  await expect(page.getByRole('textbox', { name: 'Account name' })).toHaveValue('Discovered Fixture Account')
  await expect(page.getByRole('textbox', { name: 'Alias' })).toHaveValue('discovered')
  await page.getByRole('button', { name: 'Save account' }).click()
  await expect.poll(() => fixture.savedAccounts).toEqual([{ fakeid: 'fixture-discovered', name: 'Discovered Fixture Account', alias: 'discovered' }])
  await expect(page.getByText('Saved Discovered Fixture Account. You can now start synchronization.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync selected account' })).toBeEnabled()
  await expect(page.getByText('Uses the latest local sync state to refresh new and changed article-list records.')).toBeVisible()
  await page.getByRole('button', { name: 'Sync selected account' }).click()
  await expect.poll(() => fixture.accountSyncs).toEqual([{ path: '/api/v1/accounts/account-discovered/sync', incremental: true }])
  await page.getByRole('combobox', { name: 'Synchronization mode' }).selectOption('full')
  await expect(page.getByText('Fetches the available article list without relying on the local sync boundary.')).toBeVisible()
  await page.getByRole('button', { name: 'Sync selected account' }).click()
  await expect.poll(() => fixture.accountSyncs).toEqual([
    { path: '/api/v1/accounts/account-discovered/sync', incremental: true },
    { path: '/api/v1/accounts/account-discovered/sync', incremental: false }
  ])
  await expectOnlyLoopbackRequests(page)
})

test('article metrics and resource details stay bounded and sanitized while resource actions queue jobs', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('checkbox', { name: 'Select Sanitized article one' }).check()
  await expect(page.getByRole('heading', { name: 'Resource availability' })).toBeVisible()
  await expect(page.getByText('4 resources · 3 available · 1 missing')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Article details' })).toBeVisible()
  await expect(page.getByText('120 reads · 3 old likes · 4 likes · 5 shares · 6 comments', { exact: false })).toBeVisible()
  await expect(page.getByText('image #1 · available locally')).toBeVisible()
  await expect(page.getByText('image #2 · missing locally')).toBeVisible()
  await expect(page.getByText('Showing 2 of 4 resources.')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('https://sensitive.example/resource')
  await expect(page.locator('body')).not.toContainText('sensitive-resource-digest')
  await expect(page.locator('body')).not.toContainText('/sensitive/resource/path')
  await expect(page.locator('body')).not.toContainText('sensitive-resource-id')
  await expect(page.locator('body')).not.toContainText('sensitive-credential')
  await page.getByRole('button', { name: 'Complete missing resources' }).click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([{ articleIds: ['article-fixture-1'], force: false }])
  await page.getByRole('button', { name: 'Re-download resources' }).click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([
    { articleIds: ['article-fixture-1'], force: false },
    { articleIds: ['article-fixture-1'], force: true }
  ])
  await expectOnlyLoopbackRequests(page)
})

test('account manifest controls download and import locally without retaining file details', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/accounts')

  const downloadLink = page.getByRole('link', { name: 'Download account manifest' })
  await expect(downloadLink).toHaveAttribute('href', '/api/v1/accounts/manifest')
  const download = await Promise.all([page.waitForEvent('download'), downloadLink.click()])
  expect(download[0].suggestedFilename()).toBe('wechat-article-accounts-manifest.json')

  const uploadRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/accounts/manifest/upload'))
  const importRequest = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/api/v1/accounts/manifest/import'))
  const manifestInput = page.getByLabel('Import account manifest')
  await manifestInput.setInputFiles({ name: 'private-accounts.json', mimeType: 'application/json', buffer: Buffer.from('{"schemaVersion":1,"accounts":[]}') })
  const upload = await uploadRequest
  expect(await upload.headerValue('content-type')).toContain('multipart/form-data')
  expect(upload.postData()).toContain('name="manifest"')
  expect(await manifestInput.inputValue()).toBe('')
  expect((await importRequest).postDataJSON()).toEqual({ uploadHandle: 'account-manifest-upload-fixture' })
  await expect(page.getByRole('alert')).toContainText('Account manifest imported: 1 added, 2 merged, 3 unchanged.')
  expect(fixture.accountManifestImports).toEqual([{ uploadHandle: 'account-manifest-upload-fixture' }])
  await expect(page.locator('body')).not.toContainText('private-accounts.json')
  await expectOnlyLoopbackRequests(page)
})

test('advanced article query and export handoff preserve typed local selections', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('textbox', { name: 'Account ID' }).fill('account-fixture')
  await page.getByRole('textbox', { name: 'Minimum reads' }).fill('10')
  await page.getByRole('button', { name: 'Apply filters' }).click()
  await expect.poll(() => fixture.requests.some((request) => request === 'GET /api/v1/articles')).toBe(true)
  await page.getByRole('button', { name: 'Export current matches' }).click()
  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Selection: Current matching filter' })).toBeVisible()
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  await expect.poll(() => fixture.exports.length).toBe(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({ selection: { kind: 'all_matching', query: { accountId: 'account-fixture', readMin: 10 } } })
  await expectOnlyLoopbackRequests(page)
})

test('selected albums export handoff queues opaque stable album IDs', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')
  const exportButton = page.getByRole('button', { name: 'Export selected albums' })
  await expect(exportButton).toBeDisabled()
  await page.getByRole('checkbox', { name: 'Select album-fixture-1' }).check()
  await expect(exportButton).toBeEnabled()
  await page.getByRole('button', { name: 'Next page' }).click()
  await page.getByRole('checkbox', { name: 'Select album-fixture-2' }).check()
  await exportButton.click()
  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Selection: 2 selected albums' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Album IDs' })).toHaveValue('album-fixture-1\nalbum-fixture-2')
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  const jobHandoff = page.waitForURL('**/jobs?job=job-export-fixture')
  await page.getByRole('button', { name: 'Queue export' }).click()
  await jobHandoff
  await expect.poll(() => fixture.exports.length).toBe(1)
  expect(JSON.parse(fixture.exports[0])).toMatchObject({ selection: { kind: 'album_ids', albumIds: ['album-fixture-1', 'album-fixture-2'] } })
  await expectOnlyLoopbackRequests(page)
})

test('album ID export input rejects a selection larger than the local bound', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  const albumIds = Array.from({ length: 51 }, (_, index) => `album-fixture-${index + 1}`)
  await page.addInitScript((selection) => {
    window.sessionStorage.setItem('wechat-article.export-handoff.v1', JSON.stringify({ selection, label: 'oversized albums' }))
  }, { kind: 'album_ids', albumIds })
  await page.goto('/exports')
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  await expect(page.getByRole('button', { name: 'Queue export' })).toBeDisabled()
  await expect.poll(() => fixture.exports).toHaveLength(0)
  await expectOnlyLoopbackRequests(page)
})

test('album selections persist across server pages and queue one multi-album traversal', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')

  await page.getByRole('checkbox', { name: 'Select album-fixture-1' }).check()
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('cell', { name: 'Sanitized album two', exact: true })).toBeVisible()
  await expect(page.getByText('1 selected', { exact: false })).toBeVisible()

  await page.getByRole('checkbox', { name: 'Select album-fixture-2' }).check()
  await expect(page.getByText('2 selected', { exact: false })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Traverse selected albums' })).toBeEnabled()
  await page.getByRole('button', { name: 'Traverse and batch download' }).click()
  await expect.poll(() => fixture.albumTraversals).toEqual([{ albumIds: ['album-fixture-1', 'album-fixture-2'], order: 'forward', download: true }])
  await expect(page.getByRole('button', { name: 'Export selected albums' })).toBeEnabled()
  await expectOnlyLoopbackRequests(page)
})

test('album filters stay server-paginated and traversal sends the selected order', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')
  await page.getByRole('textbox', { name: 'Account ID' }).fill('account-fixture')
  await page.getByRole('textbox', { name: 'Album keyword' }).fill('Sanitized')
  await expect.poll(() => fixture.requests.filter((request) => request === 'GET /api/v1/albums').length).toBeGreaterThan(1)
  await page.getByRole('checkbox', { name: 'Select album-fixture-1' }).check()
  await page.getByRole('combobox', { name: 'Traversal order' }).selectOption('reverse')
  await page.getByRole('button', { name: 'Traverse and batch download' }).click()
  await expect.poll(() => fixture.albumTraversals).toEqual([{ accountId: 'account-fixture', order: 'reverse', download: true }])
  await expectOnlyLoopbackRequests(page)
})

test('account deletion requires a typed exact proof and returns focus when cancelled', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/accounts')
  await page.getByRole('checkbox', { name: 'Select account-fixture' }).check()
  const deleteButton = page.getByRole('button', { name: 'Delete selected account' })
  await deleteButton.click()
  const dialog = page.getByRole('alertdialog', { name: 'Delete selected accounts' })
  const confirmation = dialog.getByRole('textbox', { name: 'Exact confirmation to delete these accounts' })
  const confirmButton = dialog.getByRole('button', { name: 'Delete accounts' })
  await expect(confirmation).toBeFocused()
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
  await page.getByRole('textbox', { name: 'Query JSON' }).fill('{"keyword":"fixture"}')
  await page.getByRole('button', { name: 'Save query' }).click()
  await expect(page.getByText('Saved query “fixture recent”.')).toBeVisible()
  await expect(page.getByRole('cell', { name: 'fixture recent', exact: true })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Select fixture recent' }).check()
  await page.getByRole('button', { name: 'Load selected query' }).click()
  await page.getByRole('textbox', { name: 'Query JSON' }).fill('{"keyword":"updated"}')
  await page.getByRole('button', { name: 'Save query' }).click()
  await expect(page.getByRole('button', { name: 'Load selected query' })).toBeEnabled()
  await page.getByRole('button', { name: 'Delete selected query' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Delete saved query' })
  await expect(dialog).toBeVisible()
  const confirmation = dialog.getByRole('textbox', { name: 'Exact confirmation to delete this saved query' })
  const deleteQuery = dialog.getByRole('button', { name: 'Delete query' })
  await expect(confirmation).toBeFocused()
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
  await expect(page.getByText('Running', { exact: true })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Select job-fixture-1' }).check()
  await expect(page.getByRole('heading', { name: 'Task detail' })).toBeVisible()
  await expect(page.getByText('Sanitized local progress', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Resume selected task' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Retry selected task' })).toBeDisabled()
  await page.getByRole('button', { name: 'Refresh detail' }).click()
  await page.getByRole('button', { name: 'Pause selected task' }).click()
  const pauseConfirmation = page.getByRole('alertdialog', { name: 'Pause selected task' })
  await expect(pauseConfirmation).toBeVisible()
  const pauseProof = pauseConfirmation.getByRole('textbox', { name: 'Exact confirmation for this task action' })
  const pauseButton = pauseConfirmation.getByRole('button', { name: 'Pause selected task' })
  await expect(pauseProof).toBeFocused()
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
  await cancelConfirmation.getByRole('textbox', { name: 'Exact confirmation for this task action' }).press('Escape')
  await expect(cancelConfirmation).toBeHidden()
  await expect(cancelTrigger).toBeFocused()
  await expectOnlyLoopbackRequests(page)
})

test('sanitized export flow authorizes a directory, downloads an artifact, opens output, and verifies output', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await installExportArtifactFixture(page)
  await page.goto('/exports')
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  await expect(page.getByText('Authorized directory: Sanitized exports')).toBeVisible()
  await page.getByRole('heading', { name: '2. Select articles and format' }).scrollIntoViewIfNeeded()
  const articleIDs = page.getByRole('textbox', { name: 'Article IDs' })
  await articleIDs.evaluate((element) => {
    const textarea = element as HTMLTextAreaElement
    const setValue = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set
    if (!setValue) throw new Error('textarea value setter is unavailable')
    setValue.call(textarea, 'article-fixture-1')
    textarea.dispatchEvent(new InputEvent('input', { bubbles: true, data: 'article-fixture-1', inputType: 'insertText' }))
    textarea.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await expect(articleIDs).toHaveValue('article-fixture-1')
  await page.getByRole('combobox', { name: 'Format' }).selectOption('html')
  await expect(page.getByRole('group', { name: 'HTML content options' })).toBeVisible()
  await expect(page.getByRole('group', { name: 'HTML content options' }).getByRole('checkbox', { name: 'Include locally stored comments' })).toBeVisible()
  await expect(page.getByRole('group', { name: 'HTML content options' })).not.toContainText('Include article content where supported')
  const includeComments = page.getByRole('checkbox', { name: 'Include locally stored comments' })
  await includeComments.evaluate((element) => (element as HTMLInputElement).click())
  await expect(includeComments).toBeChecked()
  await page.getByRole('combobox', { name: 'Resource handling' }).selectOption('strict')
  const batchArchive = page.getByRole('textbox', { name: 'HTML batch archive file name' })
  await batchArchive.evaluate((element) => {
    const input = element as HTMLInputElement
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    if (!setValue) throw new Error('input value setter is unavailable')
    setValue.call(input, 'articles.zip')
    input.dispatchEvent(new InputEvent('input', { bubbles: true, data: 'articles.zip', inputType: 'insertText' }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await expect(batchArchive).toHaveValue('articles.zip')
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
  const exportRecord = page.getByRole('checkbox', { name: 'Select export export-fixture-1' })
  await exportRecord.evaluate((element) => (element as HTMLInputElement).click())
  await expect(exportRecord).toBeChecked()
  await page.getByRole('button', { name: 'View manifest' }).click()
  await expect(page.getByText('sanitized-article.md')).toBeVisible()
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

test('import job creation navigates directly to the selected task', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/ingest/url', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(createdJob('job-import-fixture', 'import')) }))
  await page.goto('/import')

  await page.getByRole('textbox', { name: 'Article URL' }).fill('https://mp.weixin.qq.com/s/sanitized-import')
  const importHandoff = page.waitForURL('**/jobs?job=job-import-fixture')
  await page.getByRole('button', { name: 'Import URL' }).click()
  await importHandoff
  await expectOnlyLoopbackRequests(page)
})

function createdJob(id: string, kind: string) {
  return { id, kind, state: 'queued', profile: 'fixture-profile', createdAt: '2026-07-24T09:30:00.000Z', updatedAt: '2026-07-24T09:30:00.000Z', counts: { completed: 0, total: 1 } }
}

test('sanitized settings and storage maintenance flows do not reveal secrets', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')
  await expect(page.getByText('account-fixture', { exact: true })).toBeVisible()
  await page.getByRole('textbox', { name: 'Download concurrency' }).fill('3')
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

  await page.getByRole('combobox', { name: 'Collision policy' }).selectOption('replace')
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

  await page.getByLabel('Display language').selectOption('zh-CN')
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
  await expect(page.getByLabel('显示语言')).toHaveValue('zh-CN')
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
  await expect(page.getByRole('alert')).toContainText('Destructive action')
  await page.getByLabel('Backup archive').setInputFiles({ name: 'sanitized-backup.wab', mimeType: 'application/octet-stream', buffer: Buffer.from('sanitized restore archive') })
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
  await expect(page.getByRole('alert')).toContainText('The local article API is not available yet.')
  await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('an online reconnect invalidates a failed local snapshot and renders evolved state', async ({ page }) => {
  await installLoopbackFixture(page)
  let snapshotRequests = 0
  await page.route('**/api/v1/events/snapshot', (route) => {
    snapshotRequests += 1
    if (snapshotRequests <= 2) {
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
  await expect(page.getByText('Live local details are unavailable. The page remains read-only while the P0 API is rolling out.')).toBeVisible()
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await expect(page.getByText('Recovered Fixture Account')).toBeVisible()
  await expect(page.getByText('1 accounts · 7 articles · 0 albums · 1 jobs')).toBeVisible()
  expect(snapshotRequests).toBeGreaterThanOrEqual(3)
  await expectOnlyLoopbackRequests(page)
})
