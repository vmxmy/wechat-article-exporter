import { expect, test, type Page } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('desktop navigation is grouped by user task and session actions are global', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/')

  const navigation = page.getByRole('navigation').filter({ has: page.getByRole('link', { name: 'Home overview' }) }).first()
  await expect(navigation.getByRole('button', { name: 'Home', exact: true })).toHaveAttribute('aria-expanded', 'true')
  await expect(navigation.getByRole('button', { name: 'Content', exact: true })).toHaveAttribute('aria-expanded', 'true')
  await expect(navigation.getByRole('button', { name: 'Work', exact: true })).toHaveAttribute('aria-expanded', 'true')
  await expect(navigation.getByRole('button', { name: 'System', exact: true })).toHaveAttribute('aria-expanded', 'true')
  await expect(navigation.getByRole('link', { name: 'Login', exact: true })).toHaveCount(0)
  await expect(navigation.getByRole('link', { name: 'Saved queries', exact: true })).toHaveCount(0)

  const contentGroup = navigation.getByRole('button', { name: 'Content', exact: true })
  await contentGroup.click()
  await expect(contentGroup).toHaveAttribute('aria-expanded', 'false')
  await expect(navigation.getByRole('link', { name: 'Articles', exact: true })).toBeHidden()
  await contentGroup.click()
  await expect(contentGroup).toHaveAttribute('aria-expanded', 'true')
  await expect(navigation.getByRole('link', { name: 'Articles', exact: true })).toBeVisible()
  await expect(page.getByText('Runs only on this device', { exact: true })).toBeVisible()
  await expect(page.locator('body')).not.toContainText('Read-only')

  await page.getByRole('button', { name: 'Session · Not signed in' }).click()
  await page.getByRole('menuitem', { name: 'Manage login' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await page.getByRole('button', { name: 'Complete login' }).click()

  await page.getByRole('link', { name: 'Articles', exact: true }).click()
  await page.getByRole('button', { name: 'Fixture Account' }).click()
  await page.getByRole('menuitem', { name: 'Second Fixture Account' }).click()
  await expect.poll(() => fixture.accountSwitches).toEqual(['account-fixture-2'])
  await expect(page.getByRole('status').filter({ hasText: 'Switched to Second Fixture Account.' })).toBeAttached()

  await page.getByRole('button', { name: /Fixture Account/ }).click()
  await page.getByRole('menuitem', { name: 'Log out' }).click()
  await expect(page.getByRole('button', { name: 'Session · Not signed in' })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('home recommends sign-in, account setup, sync, browsing, and failed-job recovery from live state', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Sign in to WeChat' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login')

  await useHomeSnapshot(page, snapshot({ session: 'authenticated', accounts: 0, articles: 0 }))
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Add your first account' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Add account' })).toHaveAttribute('href', '/accounts')

  await useHomeSnapshot(page, snapshot({ session: 'authenticated', accounts: 1, articles: 0 }))
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Sync article records' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Choose an account to sync' })).toHaveAttribute('href', '/accounts')

  await useHomeSnapshot(page, snapshot({ session: 'authenticated', accounts: 1, articles: 4 }))
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Continue with your articles' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Browse articles' })).toHaveAttribute('href', '/articles')
  await expect(page.getByRole('link', { name: 'Export articles' })).toHaveAttribute('href', '/exports')

  await useHomeSnapshot(page, snapshot({ session: 'authenticated', accounts: 1, articles: 4, failedJobs: 2 }))
  await page.reload()
  await expect(page.getByRole('heading', { name: '2 failed jobs need attention' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Review failed jobs' })).toHaveAttribute('href', '/jobs')
  await expectOnlyLoopbackRequests(page)
})

test('mobile drawer is labelled, exposes the current page, closes on navigation, and restores focus', async ({ page }) => {
  const pageErrors: Error[] = []
  page.on('pageerror', (error) => pageErrors.push(error))
  await page.setViewportSize({ width: 390, height: 844 })
  await installLoopbackFixture(page)
  await page.goto('/articles')

  const toggle = page.getByRole('button', { name: /Open navigation|Open workspace navigation/ })
  await expect(toggle).toBeVisible()
  await toggle.focus()
  await page.keyboard.press('Enter')

  const drawer = page.getByRole('dialog', { name: 'Workspace navigation' })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByText('Current page: Articles', { exact: true })).toBeVisible()
  await expect(drawer.getByRole('link', { name: 'Articles', exact: true })).toHaveAttribute('aria-current', 'page')

  await drawer.getByRole('link', { name: 'Jobs', exact: true }).click()
  await expect(page).toHaveURL(/\/jobs$/)
  await expect(drawer).toBeHidden()
  await expect(page.getByRole('heading', { name: 'Jobs', level: 1 })).toBeFocused()

  await toggle.focus()
  await page.keyboard.press('Enter')
  await expect(drawer).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(drawer).toBeHidden()
  await expect(toggle).toBeFocused()
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect(pageErrors).toEqual([])
})

test('legacy login and saved-query deep links remain compatible', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'WeChat login' })).toBeVisible()
  await expect(page.getByText('This legacy deep link remains available.', { exact: false })).toBeVisible()

  await page.goto('/saved-queries')
  await expect(page.getByRole('heading', { name: 'Saved queries', level: 1 })).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

async function useHomeSnapshot(page: Page, body: ReturnType<typeof snapshot>) {
  await page.unroute('**/api/v1/events/snapshot')
  await page.route('**/api/v1/events/snapshot', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ apiVersion: 'v1', data: body }) }))
}

function snapshot({ session, accounts, articles, failedJobs = 0 }: { readonly session: string; readonly accounts: number; readonly articles: number; readonly failedJobs?: number }) {
  return {
    runtime: { version: 'e2e-sanitized', profileId: 'fixture-profile' },
    session: { state: session, accountId: session === 'authenticated' ? 'account-fixture' : undefined, accountName: session === 'authenticated' ? 'Fixture Account' : undefined },
    storage: { databaseAvailable: true, objectStoreReady: true, accounts, articles, albums: 0, jobs: failedJobs, objects: articles, objectBytes: articles * 42 },
    jobs: { data: Array.from({ length: failedJobs }, (_, index) => ({ id: `failed-job-${index}`, kind: 'export', state: 'failed', createdAt: '2026-07-24T09:30:00.000Z', updatedAt: '2026-07-24T09:30:00.000Z', permittedActions: ['retry'] })), pagination: { page: 1, pageSize: 25, total: failedJobs } },
    observedAt: '2026-07-24T09:30:00.000Z',
    revision: 1
  }
}
