import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

// The workspace cookie and the WeChat session are different sessions behind the
// same 401. Reporting one as the other is what makes the UI claim it is signed
// in while every task insists a sign-in is required.
test('an expired workspace session is reported as itself, not as a lost WeChat login', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/session', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ state: 'authenticated', accountId: 'account-fixture', accountName: 'Fixture Account' })
  }))
  await page.goto('/accounts')
  await expect(page.getByRole('button', { name: /Fixture Account/ })).toBeVisible()

  await page.route('**/api/v1/**', (route) => route.fulfill({
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify({ error: { code: 'authentication_required', message: 'workspace session is required' } })
  }))
  await page.getByRole('button', { name: 'Retry' }).click()

  await expect(page.getByRole('alert').filter({ hasText: 'Local workspace session expired' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Workspace session expired/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /Fixture Account/ })).toHaveCount(0)
  await expectOnlyLoopbackRequests(page)
})

test('a rejected WeChat session keeps the workspace usable and asks only for a WeChat sign-in', async ({ page }) => {
  await installLoopbackFixture(page)
  // Reported as signed in, while discovery is what WeChat rejects.
  await page.route('**/api/v1/session', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ state: 'authenticated', accountId: 'account-fixture', accountName: 'Fixture Account' })
  }))
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  await page.getByRole('textbox', { name: 'Search discovery' }).fill('fixture')
  await page.getByRole('button', { name: 'Discover account' }).click()

  await expect(page.getByText('The WeChat session may have expired. Sign in again to search for accounts.')).toBeVisible()
  await expect(page.getByRole('alert').filter({ hasText: 'Local workspace session expired' })).toHaveCount(0)
  await expectOnlyLoopbackRequests(page)
})
