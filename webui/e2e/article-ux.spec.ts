import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('article workspace layers readable common and more filters, summary, selection actions, and details', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  await expect(page.getByRole('textbox', { name: 'Search articles' })).toBeVisible()
  const account = page.getByRole('combobox', { name: 'Account' })
  await expect(account).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Start of publication range' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'End of publication range' })).toBeVisible()
  await expect(page.getByRole('combobox', { name: 'State' })).toBeVisible()
  await expect(page.getByText('Account ID', { exact: true })).toHaveCount(0)

  await account.fill('Fixture Account')
  await page.getByRole('option', { name: 'Fixture Account fixture', exact: true }).click()

  await page.getByRole('button', { name: 'More filters' }).click()
  const album = page.getByRole('combobox', { name: 'Album' })
  await expect(album).toBeVisible()
  await expect(page.getByRole('combobox', { name: 'Message types' })).toBeVisible()
  await expect(page.getByRole('spinbutton', { name: 'Minimum reads' })).toBeVisible()

  await album.fill('Sanitized album')
  await page.getByRole('option', { name: 'Sanitized album Fixture Account', exact: true }).click()

  await page.getByRole('textbox', { name: 'Search articles' }).fill('Sanitized')
  await page.getByRole('button', { name: 'Apply filters' }).click()
  await expect(page.getByRole('region', { name: 'Applied filters' })).toContainText('Search articles: Sanitized')
  await page.getByRole('button', { name: 'Clear all filters' }).click()
  await expect(page.getByRole('region', { name: 'Applied filters' })).toHaveCount(0)

  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized article one' }))
  await expect(page.getByRole('region', { name: 'Selected article actions' }).first()).toContainText('1 selected')
  await expect(page.getByRole('button', { name: 'Download selected article' })).toBeVisible()
  await page.getByRole('button', { name: 'Details' }).click()
  const detail = page.getByRole('dialog', { name: 'Sanitized article one' })
  await expect(detail).toBeVisible()
  await expect(detail.getByRole('heading', { name: 'Latest metrics' })).toBeVisible()
  await expect(detail.getByText('Reads', { exact: true })).toBeVisible()
  await expect(detail.getByText('120', { exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

async function toggleCheckbox(checkbox: import('@playwright/test').Locator) {
  await checkbox.evaluate((element) => (element as HTMLInputElement).click())
}

test('article selectors share trigger geometry', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('button', { name: 'More filters' }).click()

  const geometry = async (label: string) => page.getByRole('combobox', { name: label, exact: true }).evaluate((element) => {
    const styles = getComputedStyle(element)
    return {
      height: Math.round(element.getBoundingClientRect().height),
      borderRadius: styles.borderRadius,
      fontSize: styles.fontSize,
      paddingInlineStart: styles.paddingInlineStart,
    }
  })

  const reference = await geometry('State')
  await expect(await geometry('Account')).toEqual(reference)
  await expect(await geometry('Album')).toEqual(reference)
  await expect(await geometry('Message types')).toEqual(reference)
})

test('article multi-selector filters, selects, and clears with localized controls', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('button', { name: 'More filters' }).click()

  const messageTypes = page.getByRole('combobox', { name: 'Message types', exact: true })
  await messageTypes.fill('Text')
  await page.getByRole('option', { name: 'Text message', exact: true }).click()
  await expect(messageTypes).toHaveAttribute('placeholder', 'Text message')
  await page.getByRole('button', { name: 'Clear Message types' }).click()
  await expect(messageTypes).toHaveAttribute('placeholder', 'Choose message types…')
  await expectOnlyLoopbackRequests(page)
})

test('remote article selector restores the initial results after closing a query', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/exports')

  const articles = page.getByRole('combobox', { name: 'Selected articles', exact: true })
  await articles.click()
  await articles.fill('Later sanitized article')
  await expect(page.getByRole('option', { name: 'Later sanitized article Later Fixture Account', exact: true })).toBeVisible()
  await expect.poll(() => fixture.selectorRequests.at(-1)).toMatchObject({ path: '/api/v1/selectors/articles', search: 'Later sanitized article' })

  await page.keyboard.press('Escape')
  await expect(articles).toHaveValue('')
  await articles.click()
  await expect(page.getByRole('option', { name: 'Sanitized article one Fixture Account', exact: true })).toBeVisible()
  await expect.poll(() => fixture.selectorRequests.at(-1)).toMatchObject({ path: '/api/v1/selectors/articles', search: '' })
  await expectOnlyLoopbackRequests(page)
})

test('remote account selectors distinguish unavailable results and retry the latest request', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/selectors/accounts**', (route) => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { message: 'fixture unavailable' } }) }))
  await page.goto('/articles')

  const account = page.getByRole('combobox', { name: 'Account', exact: true })
  await account.click()
  await expect(page.getByRole('alert')).toContainText('Local selector results are unavailable.')
  await page.unroute('**/api/v1/selectors/accounts**')
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByRole('option', { name: 'Fixture Account fixture', exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('remote account selector clears stale results after a failed refresh and retries the latest query', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  const account = page.getByRole('combobox', { name: 'Account', exact: true })
  await account.click()
  await expect(page.getByRole('option', { name: 'Fixture Account fixture', exact: true })).toBeVisible()

  await page.route('**/api/v1/selectors/accounts**', (route) => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { message: 'fixture unavailable' } }) }))
  await account.fill('Later')
  await expect(page.getByRole('alert')).toContainText('Local selector results are unavailable.')
  await expect(page.getByRole('option', { name: 'Fixture Account fixture', exact: true })).toHaveCount(0)

  await page.unroute('**/api/v1/selectors/accounts**')
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByRole('option', { name: 'Later Fixture Account later', exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('remote account selector removes previous results while a newer search is pending', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  const account = page.getByRole('combobox', { name: 'Account', exact: true })
  await account.click()
  await expect(page.getByRole('option', { name: 'Fixture Account fixture', exact: true })).toBeVisible()

  await page.route('**/api/v1/selectors/accounts**', async (route) => {
    if (new URL(route.request().url()).searchParams.get('search') !== 'Later') return route.fallback()
    await new Promise<void>((resolve) => { setTimeout(resolve, 300) })
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ data: [{ id: 'account-beyond-first-page', displayName: 'Later Fixture Account', displayNameAvailable: true, alias: 'later' }], pagination: { page: 1, pageSize: 25, total: 1 } }) })
  })
  await account.fill('Later')
  await expect(page.getByRole('option', { name: 'Fixture Account fixture', exact: true })).toHaveCount(0)
  await expect(page.getByRole('option', { name: 'Later Fixture Account later', exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('saved article views filter locally, apply, and clear without exposing query internals', async ({ page }) => {
  await installLoopbackFixture(page, {
    savedQueries: [
      { name: 'Sanitized recent', query: { keyword: 'Sanitized' } },
      { name: 'Queued fixture', query: { state: 'queued' } }
    ]
  })
  await page.goto('/articles')

  const savedViews = page.getByRole('combobox', { name: 'Saved views', exact: true })
  await savedViews.click()
  await page.keyboard.insertText('queued')
  const queuedView = page.getByRole('option').filter({ hasText: 'Queued fixture' })
  await expect(queuedView).toBeVisible()
  await expect(page.getByRole('option').filter({ hasText: 'Sanitized recent' })).toHaveCount(0)
  await queuedView.click()
  await expect(savedViews).toHaveValue('Queued fixture')
  await expect(page).toHaveURL(/state=queued/)

  await page.getByRole('button', { name: 'Clear Saved views' }).click()
  await expect(savedViews).toHaveValue('')
  await savedViews.click()
  await page.keyboard.insertText('missing')
  await expect(page.getByText('No matching local results.', { exact: true })).toBeVisible()
  await expect(page.locator('body')).not.toContainText('article-fixture-1')
  await expectOnlyLoopbackRequests(page)
})

test('article account changes clear the dependent album filter', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('button', { name: 'More filters' }).click()

  const account = page.getByRole('combobox', { name: 'Account', exact: true })
  await account.fill('Fixture Account')
  await page.getByRole('option', { name: 'Fixture Account fixture', exact: true }).click()

  const album = page.getByRole('combobox', { name: 'Album', exact: true })
  await album.fill('Sanitized album')
  await page.getByRole('option', { name: 'Sanitized album Fixture Account', exact: true }).click()
  await expect(album).toHaveValue('Sanitized album')

  await account.fill('Later Fixture Account')
  await page.getByRole('option', { name: 'Later Fixture Account later', exact: true }).click()
  await expect(album).toHaveValue('')
  await expect(album).toHaveAttribute('placeholder', 'Any')
  await page.waitForTimeout(250)
  expect(fixture.selectorRequests.some((request) => request.path === '/api/v1/selectors/albums' && request.accountID === 'account-beyond-first-page' && request.search === 'Sanitized album')).toBe(false)
  await expectOnlyLoopbackRequests(page)
})

test('article applied view is canonical, reloadable, shareable, and history-restorable', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles?keyword=Sanitized&page=2&sort=title%3Aasc&dialog=secret&foreign=kept')

  await expect(page.getByRole('textbox', { name: 'Search articles' })).toHaveValue('Sanitized')
  await expect(page.getByText('Page 2 of 2', { exact: true })).toBeVisible()
  await expect(page).toHaveURL(/keyword=Sanitized/)
  await expect(page).toHaveURL(/page=2/)
  await expect(page).toHaveURL(/sort=title%3Aasc/)
  await expect(page).toHaveURL(/foreign=kept/)
  await expect(page).not.toHaveURL(/dialog=/)

  await page.reload()
  await expect(page.getByRole('textbox', { name: 'Search articles' })).toHaveValue('Sanitized')
  await expect(page.getByText('Page 2 of 2', { exact: true })).toBeVisible()

  await page.getByRole('textbox', { name: 'Search articles' }).fill('Fixture')
  await page.getByRole('button', { name: 'Apply filters' }).click()
  await expect(page).toHaveURL(/keyword=Fixture/)
  await expect(page).not.toHaveURL(/page=2/)
  await page.goBack()
  await expect(page.getByRole('textbox', { name: 'Search articles' })).toHaveValue('Sanitized')
  await expect(page.getByText('Page 2 of 2', { exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('Chinese selector controls use catalog copy without English fallbacks', async ({ page }) => {
  await installLoopbackFixture(page, { displayLanguage: 'zh-CN' })
  await page.goto('/articles')
  await page.getByRole('button', { name: '更多筛选' }).click()

  const messageTypes = page.getByRole('combobox', { name: '消息类型（逗号分隔）', exact: true })
  await messageTypes.click()
  await page.getByRole('option', { name: '文字消息', exact: true }).click()
  await expect(messageTypes).toHaveAttribute('placeholder', '文字消息')
  await page.getByRole('button', { name: '清除消息类型（逗号分隔）', exact: true }).click()
  await expect(messageTypes).toHaveAttribute('placeholder', '选择消息类型…')
  await expect(page.locator('body')).not.toContainText('Select all')
  await expectOnlyLoopbackRequests(page)
})

test('legacy saved-query route uses visual filters and only reveals raw JSON in technical mode', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/saved-queries')

  await expect(page.getByRole('heading', { name: 'Saved queries', level: 1 })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Visual query editor' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Query name' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Search articles' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Query JSON' })).toBeHidden()

  await page.getByRole('textbox', { name: 'Query name' }).fill('fixture recent')
  await page.getByRole('textbox', { name: 'Search articles' }).fill('fixture')
  await expect(page.getByText('Search articles: fixture', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Save query' }).click()
  await expect(page.getByText('Saved query “fixture recent”.')).toBeVisible()

  await page.getByRole('button', { name: 'Technical query JSON' }).click()
  await expect(page.getByRole('textbox', { name: 'Query JSON' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})
