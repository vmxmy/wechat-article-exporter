import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('article details expose bounded stored comments and keyboard-expandable paginated replies', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')
  await page.getByRole('checkbox', { name: 'Select Sanitized article one' }).check()

  await expect(page.getByRole('heading', { name: 'Stored comments' })).toBeVisible()
  await expect(page.getByText('Sanitized stored comment')).toBeVisible()
  await expect(page.getByText('1 reply thread is still pending locally.')).toBeVisible()
  const replies = page.getByRole('button', { name: 'Show 2 replies' })
  await replies.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByText('Sanitized stored reply')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('sensitive-continuation')
  await expect(page.locator('body')).not.toContainText('sensitive-reply-buffer')
  await expectOnlyLoopbackRequests(page)
})
