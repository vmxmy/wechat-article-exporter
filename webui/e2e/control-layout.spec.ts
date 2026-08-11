import { expect, test, type Locator, type Page } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'
import { selectRemoteSelectorOption } from './fixtures/selectors'

type Box = { readonly x: number; readonly y: number; readonly width: number; readonly height: number }

const viewports = [
  { name: 'wide', width: 1280, height: 720 },
  { name: 'medium', width: 720, height: 900 },
  { name: 'compact', width: 640, height: 900 },
  { name: 'mobile', width: 390, height: 844 }
] as const

for (const viewport of viewports) {
  test(`article toolbar stays bounded and follows its field policy at ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await installLoopbackFixture(page)
    await page.goto('/articles')

    const toolbar = page.getByRole('toolbar', { name: 'Article library tools', exact: true })
    const savedViewsField = toolbar.getByRole('combobox', { name: 'Saved views', exact: true }).locator('xpath=ancestor::*[@data-slot="field"][1]')

    await expect(toolbar).toHaveAttribute('data-stack-at', 'medium')
    await expect(savedViewsField).toHaveAttribute('data-control-layout', 'inline')
    await expect(savedViewsField).toHaveAttribute('data-control-size', 'lg')
    await expectNoHorizontalOverflow(page, toolbar)

    await expectOnlyLoopbackRequests(page)
  })
}

test('jobs toolbar exposes the task filter strip with the shared layout', async ({ page }) => {
  await installLoopbackFixture(page)
  for (const viewport of [{ width: 1280, height: 720 }, { width: 720, height: 900 }]) {
    await page.setViewportSize(viewport)
    await page.goto('/jobs')

    const toolbar = page.getByRole('toolbar', { name: 'Task filters and display', exact: true })
    await expect(toolbar).toHaveAttribute('data-stack-at', 'medium')
    const tabs = toolbar.getByRole('tablist')
    await expect(tabs.getByRole('tab', { name: /All/ })).toBeVisible()
    await expect(tabs.getByRole('tab', { name: /Needs attention/ })).toBeVisible()
    await expectNoHorizontalOverflow(page, toolbar)
  }
  await expectOnlyLoopbackRequests(page)
})

for (const viewport of [{ width: 720, height: 900 }, { width: 640, height: 900 }, { width: 390, height: 844 }]) {
  test(`account selection bar keeps actions bounded at ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await installLoopbackFixture(page)
    await page.goto('/accounts')
    await page.getByRole('checkbox', { name: 'Select Fixture Account' }).check()

    // Selection-dependent actions now live in the bottom SelectionActionBar (unified with
    // albums/articles), not in the in-table toolbar. The toolbar keeps only Add/Import.
    const selectionBar = page.getByRole('region', { name: 'Selected account actions' })
    const syncButton = selectionBar.getByRole('button', { name: /Sync/ })
    const deleteButton = selectionBar.getByRole('button', { name: /Delete/ })

    await expect(syncButton).toBeEnabled()
    await expect(deleteButton).toBeEnabled()
    await expect(selectionBar.getByRole('combobox', { name: 'Synchronization mode' })).toBeVisible()
    await expectNoHorizontalOverflow(page, selectionBar)

    const toolbar = page.getByRole('toolbar', { name: 'Account library tools', exact: true })
    await expect(toolbar).toHaveAttribute('data-stack-at', 'medium')
    await expectNoHorizontalOverflow(page, toolbar)

    await expectOnlyLoopbackRequests(page)
  })
}

test('form grids cap tracks while horizontal export fields remain the documented exception', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await installLoopbackFixture(page)
  await page.goto('/settings')
  await page.getByRole('tab', { name: 'Network/Proxy' }).click()
  await expect(page.locator('#settings-network')).toBeVisible()

  const proxyGrid = page.locator('.settings-proxy-form')
  await expect(proxyGrid).toHaveAttribute('data-columns', '2')
  expect(await distinctRowColumnCount(proxyGrid)).toBeLessThanOrEqual(2)
  await expectNoHorizontalOverflow(page, proxyGrid)

  const proxyClassField = proxyGrid.getByRole('checkbox', { name: 'Public article content' }).locator('xpath=ancestor::*[@data-slot="field"][1]')
  const proxyClassControl = proxyGrid.getByRole('checkbox', { name: 'Public article content' })
  await expect(proxyClassField).toHaveAttribute('data-control-layout', 'compact')
  expect((await box(proxyClassField)).width).toBeGreaterThanOrEqual((await box(proxyClassControl)).width)

  await page.goto('/exports')
  await selectRemoteSelectorOption(page, 'Selected articles', 'Sanitized article one')
  await page.getByRole('button', { name: 'Continue to format' }).evaluate((element) => (element as HTMLButtonElement).click())
  const horizontalGrid = page.locator('.presentation-form-grid-horizontal')
  await expect(horizontalGrid).toHaveAttribute('data-direction', 'horizontal')
  await expect(horizontalGrid).not.toHaveAttribute('data-columns')
  await expectNoHorizontalOverflow(page, horizontalGrid)
  await expectOnlyLoopbackRequests(page)
})

async function box(locator: Locator): Promise<Box> {
  return locator.evaluate((element) => {
    const bounds = element.getBoundingClientRect()
    return { x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height }
  })
}

async function distinctRowColumnCount(grid: Locator): Promise<number> {
  return grid.evaluate((element) => {
    const children = Array.from(element.children).filter((child) => !child.classList.contains('presentation-form-grid-full-span'))
    const firstRowTop = children[0]?.getBoundingClientRect().top
    if (firstRowTop === undefined) return 0
    return children.filter((child) => Math.abs(child.getBoundingClientRect().top - firstRowTop) < 2).length
  })
}

async function expectNoHorizontalOverflow(page: Page, container: Locator): Promise<void> {
  expect(await page.locator('html').evaluate((element) => element.scrollWidth <= element.clientWidth)).toBeTruthy()
  expect(await container.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBeTruthy()
}
