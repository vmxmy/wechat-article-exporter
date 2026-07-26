import { expect, test, type Locator, type Page } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'
import { selectRemoteSelectorOption } from './fixtures/selectors'

test('keyboard-only navigation preserves skip focus and live login status', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')

  const skip = page.getByRole('link', { name: 'Skip to content' })
  await focusWithKeyboard(page, skip)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('main')).toBeFocused()

  await focusWithKeyboard(page, page.getByRole('button', { name: 'Start QR login' }))
  await page.keyboard.press('Enter')
  const qrCode = page.getByRole('img', { name: 'QR-code login' })
  await expect(qrCode).toBeVisible()
  await expect(qrCode).toHaveAttribute('width', '256')
  await expect(qrCode).toHaveAttribute('height', '256')

  await focusWithKeyboard(page, page.getByRole('button', { name: 'Poll login status' }))
  await page.keyboard.press('Enter')
  await expect(page.getByRole('status').filter({ hasText: 'Scanned' })).toBeVisible()

  await focusWithKeyboard(page, page.getByRole('button', { name: 'Complete login' }))
  await page.keyboard.press('Enter')
  await expect(page.getByRole('status').filter({ hasText: 'Completed' })).toBeVisible()
  await expect(page.getByText('Authenticated', { exact: true })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('SPA navigation keeps one main landmark and moves focus to each destination heading', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')

  await expect(page.getByRole('main')).toHaveCount(1)
  await page.getByRole('link', { name: 'Articles', exact: true }).click()
  await expect(page).toHaveURL(/\/articles$/)
  await expect(page.getByRole('heading', { name: 'Articles', level: 1 })).toBeFocused()

  await page.goBack()
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: 'WeChat login', level: 1 })).toBeFocused()

  await page.goForward()
  await expect(page).toHaveURL(/\/articles$/)
  await expect(page.getByRole('heading', { name: 'Articles', level: 1 })).toBeFocused()
  await expect(page.getByRole('main')).toHaveCount(1)
  await expectOnlyLoopbackRequests(page)
})

test('article and album export handoffs use SPA navigation and focus the export heading', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')

  await page.getByRole('button', { name: 'Export current matches' }).click()
  await expect(page).toHaveURL(/\/exports\?flow=[a-f0-9]{32}&scope=matching$/)
  await expect(page.getByRole('heading', { name: 'Export articles', level: 1 })).toBeFocused()

  await page.goto('/albums')
  await page.getByRole('checkbox', { name: 'Select Sanitized album' }).check()
  await expect(page.getByRole('checkbox', { name: 'Select album-fixture-1' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Export selected albums' }).click()
  await expect(page).toHaveURL(/\/exports\?flow=[a-f0-9]{32}&scope=album$/)
  await expect(page.getByRole('heading', { name: 'Export articles', level: 1 })).toBeFocused()
  await expect(page.getByRole('main')).toHaveCount(1)
  await expectOnlyLoopbackRequests(page)
})

test('keyboard-only exact confirmation gates destructive garbage collection', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  await focusWithKeyboard(page, page.getByRole('button', { name: 'Generate GC plan' }))
  await page.keyboard.press('Enter')
  const apply = page.getByRole('button', { name: 'Apply this plan once' })
  await expect(apply).toBeDisabled()
  expect(fixture.requests.filter((request) => request === 'POST /api/v1/maintenance/gc/apply')).toHaveLength(0)

  await focusWithKeyboard(page, page.getByRole('textbox', { name: 'One-time exact confirmation' }))
  await page.keyboard.insertText('apply-gc-fixture')
  await expect(apply).toBeEnabled()
  await focusWithKeyboard(page, apply)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('status').filter({ hasText: 'GC completed.' })).toBeVisible()
  expect(fixture.requests.filter((request) => request === 'POST /api/v1/maintenance/gc/apply')).toHaveLength(1)
  await expectOnlyLoopbackRequests(page)
})

test('keyboard-only destructive task confirmation can be cancelled without a native dialog', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/jobs')
  await page.getByRole('checkbox', { name: 'Select Export' }).check()
  await expect(page.getByRole('checkbox', { name: 'Select job-fixture-1' })).toHaveCount(0)

  const pause = page.getByRole('button', { name: 'Pause selected task' }).first()
  await focusWithKeyboard(page, pause)
  await page.keyboard.press('Enter')
  const confirmation = page.getByRole('alertdialog', { name: 'Pause selected task' })
  await expect(confirmation).toBeVisible()
  const keepRunning = confirmation.getByRole('button', { name: 'Keep task running' })
  await focusWithKeyboard(page, keepRunning)
  await page.keyboard.press('Enter')
  await expect(confirmation).toBeHidden()
  await expect(pause).toBeFocused()
  expect(fixture.controls).toHaveLength(0)
  await expectOnlyLoopbackRequests(page)
})

test('390px resource views use readable mobile projections without whole-document horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installLoopbackFixture(page)

  for (const view of [
    { path: '/accounts', content: 'Fixture Account' },
    { path: '/articles', content: 'Sanitized article one' },
    { path: '/albums', content: 'Sanitized album' },
    { path: '/jobs', content: 'Export' }
  ]) {
    await page.goto(view.path)
    const mobileTable = page.locator('.presentation-data-table-mobile')
    await expect(mobileTable).toBeVisible()
    await expect(mobileTable).toContainText(view.content)
    await expect(page.locator('.presentation-data-table-desktop')).toBeHidden()
    await expect(page.locator('.presentation-data-table-toolbar')).toBeHidden()
    await expectNoDocumentOverflow(page)
  }
  await expectOnlyLoopbackRequests(page)
})

test('390px export and settings tables retain safe mobile identity, status, and actions', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installLoopbackFixture(page)

  await page.goto('/exports')
  const exportRecords = page.locator('.presentation-data-table-mobile').filter({ hasText: 'MARKDOWN export' })
  await expect(exportRecords).toBeVisible()
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select export MARKDOWN export' }))
  await page.getByRole('button', { name: 'View manifest' }).click()
  await expect(page.locator('.presentation-data-table-mobile').filter({ hasText: 'sanitized-article.md' })).toBeVisible()
  await expect(page.getByText('/private/export/root/sanitized-article.md', { exact: true })).toHaveCount(0)
  await expectNoDocumentOverflow(page)

  await page.goto('/settings')
  const mobileTables = page.locator('.presentation-data-table-mobile')
  await expect(mobileTables).toHaveCount(3)
  await expect(mobileTables.nth(0)).toContainText('Fixture Account')
  await expect(mobileTables.nth(1)).toContainText('Sanitized proxy')
  await expect(mobileTables.nth(2)).toContainText('Local connection')
  await expect(mobileTables.nth(0).getByRole('button', { name: 'Remove' })).toBeVisible()
  await expect(mobileTables.nth(1).getByRole('button', { name: 'Disable' })).toBeVisible()
  await expect(mobileTables.nth(1).getByRole('button', { name: 'Test' })).toBeVisible()
  await expectNoDocumentOverflow(page)
  await expectOnlyLoopbackRequests(page)
})

test('desktop table column controls stay compact without changing their accessible names', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await installLoopbackFixture(page)

  for (const view of [
    { path: '/articles', label: 'Visible article columns' },
    { path: '/accounts', label: 'Visible account columns' },
    { path: '/albums', label: 'Visible album columns' },
    { path: '/jobs', label: 'Visible job columns' },
    { path: '/saved-queries', label: 'Visible saved-query columns' }
  ]) {
    await page.goto(view.path)
    const surface = page.locator('.presentation-data-table-surface')
    const toolbar = surface.locator('.presentation-data-table-toolbar')
    const selector = page.getByRole('combobox', { name: view.label, exact: true })
    await expect(selector).toBeVisible()
    await expect.poll(async () => {
      const [fieldWidth, surfaceWidth] = await Promise.all([
        selector.evaluate((element) => element.closest("[data-slot='field']")?.getBoundingClientRect().width),
        surface.evaluate((element) => element.getBoundingClientRect().width)
      ])
      if (fieldWidth === undefined) throw new Error('Table column selector is missing its field wrapper.')
      return fieldWidth < surfaceWidth
    }).toBe(true)
    await expect(toolbar).toBeVisible()
    await expect(surface.locator('.presentation-data-table-desktop')).toBeVisible()
  }
  await expectOnlyLoopbackRequests(page)
})

test('200% page zoom keeps the staged export primary flow usable without whole-document overflow', async ({ page }) => {
  await installLoopbackFixture(page)
  const session = await page.context().newCDPSession(page)
  await page.goto('/exports')
  await session.send('Emulation.setPageScaleFactor', { pageScaleFactor: 2 })
  await expect.poll(() => page.evaluate(() => window.visualViewport?.scale ?? 1)).toBe(2)

  await selectRemoteSelectorOption(page, 'Selected articles', 'Sanitized article one')
  await clickButton(page, 'Continue to format')
  await expect(page.getByRole('heading', { name: 'Format and options' })).toBeVisible()
  await clickButton(page, 'Continue to destination')
  await expect(page.getByRole('heading', { name: 'Destination and confirmation' })).toBeVisible()
  await page.getByRole('button', { name: 'Authorize default directory' }).click()
  await expect(page.getByRole('button', { name: 'Queue export' })).toBeEnabled()
  await expectNoDocumentOverflow(page)
  await expectOnlyLoopbackRequests(page)
})

async function toggleCheckbox(checkbox: Locator) {
  await checkbox.evaluate((element) => (element as HTMLInputElement).click())
}

async function clickButton(page: Page, name: string) {
  await page.getByRole('button', { name, exact: true }).evaluate((element) => (element as HTMLButtonElement).click())
}

async function focusWithKeyboard(page: Page, target: Locator) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (await target.evaluate((element) => document.activeElement === element)) return
    await page.keyboard.press('Tab')
  }
  throw new Error(`Could not reach ${await target.ariaSnapshot()} with Tab navigation.`)
}

async function expectNoDocumentOverflow(page: Page) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}
