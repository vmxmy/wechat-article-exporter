import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('article resource actions remain available for serial requests', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('checkbox', { name: 'Select Sanitized article one' }).check()

  const completeResources = page.getByRole('menuitem', { name: 'Complete missing resources' })
  const redownloadResources = page.getByRole('menuitem', { name: 'Re-download resources' })
  await page.getByRole('button', { name: 'More actions' }).click()
  await completeResources.click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([{ articleIds: ['article-fixture-1'], force: false }])

  await page.getByRole('button', { name: 'More actions' }).click()
  await expect(redownloadResources).toBeVisible()
  await redownloadResources.click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([
    { articleIds: ['article-fixture-1'], force: false },
    { articleIds: ['article-fixture-1'], force: true }
  ])
  await expectOnlyLoopbackRequests(page)
})
