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
