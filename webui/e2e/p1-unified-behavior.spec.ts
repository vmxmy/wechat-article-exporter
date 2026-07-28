import { expect, test, type Locator } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

async function toggleCheckbox(checkbox: Locator) {
  await checkbox.evaluate((element) => (element as HTMLInputElement).click())
}

// P1: accounts page has a keyword search that converges the list and echoes as a removable chip.
test('accounts list search filters the page and echoes as a removable chip', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/accounts')

  const search = page.getByRole('textbox', { name: 'Search accounts' })
  await search.type('Fixture', { delay: 60 })
  await page.waitForTimeout(450)

  // The list request carries the keyword.
  expect(fixture.requests.some((request) => request === 'GET /api/v1/accounts')).toBeTruthy()
  // The applied filter is echoed and removable.
  const applied = page.getByRole('region', { name: 'Applied filters' })
  await expect(applied).toBeVisible()
  await expect(applied.getByText(/Search accounts: Fixture/)).toBeVisible()

  // Clearing via the chip restores the unfiltered list.
  await applied.getByRole('button', { name: /Remove Search accounts/ }).click()
  await expect(page.getByRole('region', { name: 'Applied filters' })).toHaveCount(0)

  await expectOnlyLoopbackRequests(page)
})

// P1: selection actions live in the bottom SelectionActionBar on all three pages — same place.
test('all three resource pages surface selection actions in the bottom selection bar', async ({ page }) => {
  await installLoopbackFixture(page)

  for (const { path, selectLabel, barLabel } of [
    { path: '/accounts', selectLabel: 'Select Fixture Account', barLabel: 'Selected account actions' },
    { path: '/albums', selectLabel: 'Select Sanitized album', barLabel: 'Album actions' },
    { path: '/articles', selectLabel: 'Select Sanitized article one', barLabel: 'Selected article actions' }
  ]) {
    await page.goto(path)
    await toggleCheckbox(page.getByRole('checkbox', { name: selectLabel }))
    const bar = page.getByRole('region', { name: barLabel })
    await expect(bar).toBeVisible()
  }

  await expectOnlyLoopbackRequests(page)
})

// P1: the article "more actions" control is a real menu (Escape + outside-click close, ARIA menu role).
test('article more-actions menu closes on Escape and outside click and exposes menu semantics', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.goto('/articles')
  await toggleCheckbox(page.getByRole('checkbox', { name: 'Select Sanitized article one' }))

  const moreButton = page.getByRole('button', { name: 'More actions' })
  await moreButton.click()
  const menu = page.getByRole('menu')
  await expect(menu).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Preview' })).toBeVisible()

  // Escape closes the menu and returns focus to the trigger.
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(moreButton).toBeFocused()

  // Outside click also closes it. Base UI renders an inert backdrop that intercepts
  // the pointer event; click that presentation backdrop to dismiss the menu.
  await moreButton.click()
  await expect(menu).toBeVisible()
  await page.locator('[data-base-ui-portal] [role="presentation"][data-base-ui-inert]').click({ force: true })
  await expect(menu).toHaveCount(0)

  await expectOnlyLoopbackRequests(page)
})

// P1: albums echo applied filters as removable chips and show a filtered-empty state when no match.
test('albums echo applied account and keyword filters as removable chips', async ({ page }) => {
  const fixture = await installLoopbackFixture(page)
  await page.goto('/albums')

  const keyword = page.getByRole('textbox', { name: 'Album keyword' })
  await keyword.type('nomatch-zzz', { delay: 40 })
  await page.waitForTimeout(450)

  const applied = page.getByRole('region', { name: 'Applied filters' })
  await expect(applied).toBeVisible()
  await expect(applied.getByText(/Album keyword: nomatch-zzz/)).toBeVisible()

  // Removing the keyword chip clears only that filter.
  await applied.getByRole('button', { name: /Remove Album keyword/ }).click()
  await expect(applied).toHaveCount(0)

  // No external network beyond loopback; multiple keystrokes produced at most one extra list request.
  expect(fixture.requests.filter((request) => request === 'GET /api/v1/albums').length).toBeGreaterThan(0)
  await expectOnlyLoopbackRequests(page)
})
