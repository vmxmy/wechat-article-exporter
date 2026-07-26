import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'
import { selectStaticSelectorOption } from './fixtures/selectors'

test('settings categories provide current-section navigation and preserve preference feedback', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  const navigation = page.getByRole('navigation', { name: 'Settings sections' })
  await expect(navigation.getByRole('link', { name: 'General/Preferences' })).toHaveAttribute('aria-current', 'location')

  await navigation.getByRole('link', { name: 'Download/Export Defaults' }).click()
  await expect(page.locator('#settings-download-export')).toBeInViewport()
  await expect(navigation.getByRole('link', { name: 'Download/Export Defaults' })).toHaveAttribute('aria-current', 'location')

  await selectStaticSelectorOption(page, 'Collision policy', 'Replace existing output')
  await page.getByRole('button', { name: 'Save preferences' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Preferences saved.' })).toBeVisible()
  expect(fixture.preferencePatches).toHaveLength(1)
  await expectOnlyLoopbackRequests(page)
})

test('settings use the readable credential account name and explain credential-trusted proxy scope', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/settings/credentials', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify([{ id: 'credential-fixture', accountId: 'opaque-account-id', accountName: 'Readable Fixture Account', accountNameAvailable: true, kind: 'cookie', status: 'valid', createdAt: '2026-07-24T09:30:00.000Z', updatedAt: '2026-07-24T09:30:00.000Z' }])
  }))
  await page.goto('/settings')

  await expect(page.getByText('Readable Fixture Account', { exact: true })).toBeVisible()
  await expect(page.getByText('opaque-account-id', { exact: true })).not.toBeVisible()
  await page.getByRole('button', { name: 'Credential technical details' }).click()
  await expect(page.getByText('opaque-account-id', { exact: true })).toBeVisible()

  await expect(page.getByText('Public-only routes never receive credential-bearing requests.', { exact: true }).first()).toBeVisible()
  await expectOnlyLoopbackRequests(page)
})

test('settings localize category navigation and use Astryx numeric controls', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/settings')

  await selectStaticSelectorOption(page, 'Display language', 'Chinese (Simplified)')
  await page.getByRole('button', { name: 'Save preferences' }).click()

  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  const navigation = page.getByRole('navigation', { name: '设置分区' })
  await expect(navigation.getByRole('link', { name: '下载与导出默认值' })).toBeVisible()
  await expect(page.getByRole('spinbutton', { name: '下载并发数' })).toHaveValue('2')
  await expect(page.locator('#settings-download-export').getByRole('combobox', { name: '冲突策略', exact: true })).toBeVisible()
  expect(fixture.preferencePatches).toHaveLength(1)
  await expectOnlyLoopbackRequests(page)
})

test('storage maintenance isolates restore and garbage collection while preserving exact confirmation contracts', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/settings')

  const dangerZone = page.locator('.settings-danger-zone')
  await expect(dangerZone).toContainText('Destructive recovery and cleanup')
  await expect(dangerZone.getByRole('alert')).toContainText('Destructive action')
  await expect(dangerZone.getByRole('button', { name: 'Stage archive for restore' })).toBeDisabled()

  await dangerZone.getByRole('button', { name: 'Generate GC plan' }).click()
  await expect(dangerZone.getByText('apply-gc-fixture', { exact: true })).toBeVisible()
  const apply = dangerZone.getByRole('button', { name: 'Apply this plan once' })
  await expect(apply).toBeDisabled()
  await dangerZone.getByRole('textbox', { name: 'One-time exact confirmation' }).fill('apply-gc-fixture')
  await expect(apply).toBeEnabled()
  await expectOnlyLoopbackRequests(page)
})

test('settings protects unsaved preferences and clears the guard after save', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/settings')

  await selectStaticSelectorOption(page, 'Collision policy', 'Replace existing output')
  const articlesLink = page.getByRole('link', { name: 'Articles', exact: true })
  await articlesLink.click()
  const guard = page.getByRole('alertdialog', { name: 'Unsaved preference changes' })
  await expect(guard).toBeVisible()
  const discardChanges = guard.getByRole('button', { name: 'Discard changes' })
  const stayOnSettings = guard.getByRole('button', { name: 'Stay on settings' })
  await expect(discardChanges).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(guard.getByRole('button', { name: 'Close dialog' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(discardChanges).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(stayOnSettings).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(guard.getByRole('button', { name: 'Close dialog' })).toBeFocused()
  await stayOnSettings.click()
  await expect(page).toHaveURL(/\/settings$/)
  await expect(articlesLink).toBeFocused()

  await articlesLink.click()
  await guard.getByRole('button', { name: 'Discard changes' }).click()
  await expect(page).toHaveURL(/\/articles$/)

  await page.goto('/settings')
  await selectStaticSelectorOption(page, 'Collision policy', 'Replace existing output')
  await page.getByRole('button', { name: 'Save preferences' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Preferences saved.' })).toBeVisible()
  await page.getByRole('link', { name: 'Articles', exact: true }).click()
  await expect(page).toHaveURL(/\/articles$/)
  await expect(guard).toBeHidden()
  await expectOnlyLoopbackRequests(page)
})

test('diagnostic bundle summary humanizes size, shortens the checksum, and keeps the opaque download handle', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/settings')

  await page.getByRole('button', { name: 'Create diagnostic bundle' }).click()
  await expect(page.getByText('128 B', { exact: true })).toBeVisible()
  await expect(page.getByText('dddddddd…dddddddd', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Download diagnostic bundle' })).toHaveAttribute('href', '/api/v1/maintenance/diagnostic-bundles/diagnostic_sanitized-handle')
  await expectOnlyLoopbackRequests(page)
})
