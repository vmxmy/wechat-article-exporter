import { expect, test, type Locator, type Page } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('keyboard-only navigation preserves skip focus and live login status', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')

  const skip = page.getByRole('link', { name: 'Skip to content' })
  await focusWithKeyboard(page, skip)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('main')).toBeFocused()

  await focusWithKeyboard(page, page.getByRole('button', { name: 'Start QR login' }))
  await page.keyboard.press('Enter')
  await expect(page.getByRole('img', { name: 'QR-code login' })).toBeVisible()

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
  await expect(page).toHaveURL(/\/exports$/)
  await expect(page.getByRole('heading', { name: 'Export articles', level: 1 })).toBeFocused()

  await page.goto('/albums')
  await page.getByRole('checkbox', { name: 'Select album-fixture-1' }).check()
  await page.getByRole('button', { name: 'Export selected album' }).click()
  await expect(page).toHaveURL(/\/exports$/)
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
  await page.getByRole('checkbox', { name: 'Select job-fixture-1' }).check()

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

test('narrow viewport keeps the sanitized export workspace usable without page overflow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installLoopbackFixture(page)
  await page.goto('/exports')

  await expect(page.getByRole('heading', { name: 'Export articles' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Authorize default directory' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await expectOnlyLoopbackRequests(page)
})

async function focusWithKeyboard(page: Page, target: Locator) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (await target.evaluate((element) => document.activeElement === element)) return
    await page.keyboard.press('Tab')
  }
  throw new Error(`Could not reach ${await target.ariaSnapshot()} with Tab navigation.`)
}
