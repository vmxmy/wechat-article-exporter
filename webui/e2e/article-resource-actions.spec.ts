import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('article resource actions remain available for serial requests', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('checkbox', { name: 'Select Sanitized article one' }).check()
  await page.locator('.article-selection-more > summary').click()

  const completeResources = page.getByRole('button', { name: 'Complete missing resources' })
  const redownloadResources = page.getByRole('button', { name: 'Re-download resources' })
  await completeResources.click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([{ articleIds: ['article-fixture-1'], force: false }])
  await expect(redownloadResources).toBeEnabled()

  await redownloadResources.click()
  await expect.poll(() => fixture.resourceDownloads).toEqual([
    { articleIds: ['article-fixture-1'], force: false },
    { articleIds: ['article-fixture-1'], force: true }
  ])
  await expect(completeResources).toBeEnabled()
  await expectOnlyLoopbackRequests(page)
})
