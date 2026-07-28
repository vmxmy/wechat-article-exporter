import { expect, test, type Locator } from '@playwright/test'
import { bulkArticleKeyword, expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

async function toggleCheckbox(checkbox: Locator) {
  await checkbox.evaluate((element) => (element as HTMLInputElement).click())
}

// P0 defect #1: the clear-draft button must be labeled honestly, not as "re-login".
test('account add drawer shows an honest clear button and an explicit cancel after picking a candidate', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/login')
  await page.getByRole('button', { name: 'Start QR login' }).click()
  await page.getByRole('button', { name: 'Poll login status' }).click()
  await page.getByRole('button', { name: 'Complete login' }).click()
  await expect(page.getByText('Authenticated', { exact: true })).toBeVisible()
  await page.goto('/accounts')

  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  await page.getByRole('textbox', { name: 'Search discovery' }).fill('fixture')
  await page.getByRole('button', { name: 'Discover account' }).click()
  await page.getByRole('region', { name: 'Discovery results' }).getByRole('button', { name: 'Use candidate' }).click()

  const dialog = page.getByRole('dialog', { name: 'Add account' })
  await expect(dialog.getByRole('button', { name: 'Clear and re-select' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeVisible()
  // The misleading "re-login" label must not appear as a destructive clear action.
  await expect(dialog.getByRole('button', { name: 'Sign in again' })).toHaveCount(0)

  // Clearing drops the picked candidate but keeps the drawer open for a new selection.
  await dialog.getByRole('button', { name: 'Clear and re-select' }).click()
  await expect(page.getByRole('textbox', { name: 'Account name' })).toHaveValue('')

  // Cancel closes the drawer without saving.
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByRole('dialog', { name: 'Add account' })).toHaveCount(0)

  await expectOnlyLoopbackRequests(page)
})

// P0 defect #3: edit mode has a distinct title and hides the discovery flow.
test('account edit drawer has an edit title, edit description, and hides discovery', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/accounts')
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Fixture Account' }))

  await page.getByRole('button', { name: 'More account actions' }).click()
  await page.getByRole('menuitem', { name: 'Update selected account' }).click()

  const editDialog = page.getByRole('dialog', { name: 'Edit account' })
  await expect(editDialog).toBeVisible()
  // Edit describes only what the drawer offers — no "start sync" promise, no discovery.
  await expect(editDialog).toContainText('Update the selected account record')
  await expect(editDialog.getByText('Search discovery')).toHaveCount(0)
  await expect(editDialog.getByRole('textbox', { name: 'Search discovery' })).toHaveCount(0)
  // The technical/fakeid input stays available in edit mode.
  await expect(editDialog.getByRole('button', { name: 'Advanced technical details' })).toBeVisible()

  // Add mode keeps the discovery flow and the create title/description.
  await editDialog.getByRole('button', { name: 'Cancel' }).click()
  await page.getByRole('button', { name: 'Add account', exact: true }).click()
  const addDialog = page.getByRole('dialog', { name: 'Add account' })
  await expect(addDialog).toBeVisible()
  await expect(addDialog.getByText('Search discovery')).toBeVisible()

  await expectOnlyLoopbackRequests(page)
})

// P0 defect #2: exceeding the album selection limit surfaces a visible notice.
test('exceeding the album selection limit shows a notice and keeps the count bounded', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/albums')

  const selectAll = page.getByRole('checkbox', { name: 'Select all rows on this page' })
  // Page 1 + page 2 = 50 rows, exactly the limit.
  await toggleCheckbox(selectAll)
  await expect(page.getByText('25 / 50 selected', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Next page' }).click()
  await toggleCheckbox(selectAll)
  await expect(page.getByText('50 / 50 selected', { exact: false })).toBeVisible()

  // Page 3 holds unselected rows; attempting the 51st selection is refused with a notice.
  await page.getByRole('button', { name: 'Next page' }).click()
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Synthetic album 51' }).first())
  await expect(page.getByText(/Up to 50 albums can be selected at once/)).toBeVisible()
  // The count did not silently grow past the limit.
  await expect(page.getByText('50 / 50 selected', { exact: false })).toBeVisible()

  await expectOnlyLoopbackRequests(page)
})

// P0 defect #2 (articles): same contract for the article limit.
test('exceeding the article selection limit shows a notice and keeps the count bounded', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto(`/articles?keyword=${bulkArticleKeyword}`)

  const selectAll = page.getByRole('checkbox', { name: 'Select all visible articles' })
  // Ten full pages of 25 reach exactly the 250 limit.
  for (let pageNumber = 1; pageNumber <= 10; pageNumber += 1) {
    await expect(page.getByRole('cell', { name: `Bulk article ${(pageNumber - 1) * 25 + 1}`, exact: true })).toBeVisible()
    await toggleCheckbox(selectAll)
    await expect(page.getByText(`${pageNumber * 25} / 250 selected`, { exact: false })).toBeVisible()
    await page.getByRole('button', { name: 'Next page' }).click()
  }

  // Page 11 holds the single remaining row; selecting it would be the 251st.
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Bulk article 251' }))
  await expect(page.getByText(/Up to 250 articles can be selected at once/)).toBeVisible()
  // The count did not silently grow past the limit.
  await expect(page.getByText('250 / 250 selected', { exact: false })).toBeVisible()

  await expectOnlyLoopbackRequests(page)
})

// P0 defect #4: the album keyword is debounced into a single list request.
test('album keyword collapses a typing burst into one list request', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')
  await expect(page.getByRole('table').getByText('Sanitized album', { exact: true })).toBeVisible()

  const before = fixture.requests.filter((request) => request === 'GET /api/v1/albums').length
  // Six keystrokes: undebounced this is six requests, debounced it is exactly one.
  await page.getByRole('textbox', { name: 'Album keyword' }).pressSequentially('Synthe', { delay: 40 })
  await expect(page.getByRole('table').getByText('Synthetic album 3', { exact: true })).toBeVisible()
  await page.waitForTimeout(500)

  const after = fixture.requests.filter((request) => request === 'GET /api/v1/albums').length
  // Exactly one — `toBeLessThanOrEqual(1)` would also pass if the query stopped updating entirely.
  expect(after - before).toBe(1)
  await expectOnlyLoopbackRequests(page)
})

// Blocker found in review: a selection must never survive into a result set that no
// longer contains it, or the user queues a job against an album they cannot see.
test('album selection is dropped once the committed keyword changes the result set', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/albums')

  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized album' }))
  const selectionBar = page.getByRole('region', { name: 'Album actions' })
  await expect(selectionBar).toContainText('1 / 50 selected')

  // A keyword that excludes the selected row commits after the debounce window.
  await page.getByRole('textbox', { name: 'Album keyword' }).pressSequentially('Synthe', { delay: 40 })
  await expect(page.getByRole('table').getByText('Synthetic album 3', { exact: true })).toBeVisible()
  await expect(page.getByRole('table').getByText('Sanitized album', { exact: true })).toHaveCount(0)

  // No stale count, and therefore no traversal target the user cannot see.
  await expect(selectionBar).toHaveCount(0)
  await expectOnlyLoopbackRequests(page)
})
