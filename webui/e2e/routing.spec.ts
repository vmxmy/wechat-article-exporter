import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('an unknown route renders a dedicated not-found page that keeps the URL and links home', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/does-not-exist')

  await expect(page).toHaveURL(/\/does-not-exist$/)
  await expect(page.getByRole('heading', { name: 'Page not found', level: 1 })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Back to Home' })).toBeVisible()
  await expect(page).toHaveTitle('Page not found')
  await expectOnlyLoopbackRequests(page)
})

test('following the home link from a not-found page returns to the overview', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/does-not-exist')

  await page.getByRole('link', { name: 'Back to Home' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expectOnlyLoopbackRequests(page)
})

test('a known route updates the document title to its localized segment', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')

  await expect(page).toHaveURL(/\/accounts$/)
  await expect(page).toHaveTitle(/Accounts/)
  await expectOnlyLoopbackRequests(page)
})

test('a paged resource view restores its page from the URL after a reload', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts?page=2')

  await expect(page.getByText('Page 2 of', { exact: false })).toBeVisible()
  await page.reload()
  await expect(page.getByText('Page 2 of', { exact: false })).toBeVisible()
  await expect(page).toHaveURL(/page=2/)
  await expectOnlyLoopbackRequests(page)
})
