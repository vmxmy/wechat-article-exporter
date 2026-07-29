import { expect, test } from '@playwright/test'
import { installLoopbackFixture } from './fixtures/loopback-api'

/**
 * A table handed a fresh data array on every render makes TanStack auto-reset
 * the page index, and that reset re-renders the page, so the export workspace
 * used to spin at 100% CPU until the tab died. Sample the renderer instead of
 * the DOM, because the render loop yields between scheduler tasks and so still
 * answers evaluate() while it burns the main thread.
 */
test('the export workspace does not spin the renderer', async ({ page, context }) => {
  await installLoopbackFixture(page)
  const client = await context.newCDPSession(page)
  await client.send('Profiler.enable')
  await client.send('Profiler.setSamplingInterval', { interval: 200 })

  await page.goto('/exports')
  await expect(page.getByRole('heading', { name: /export articles|导出文章/i })).toBeVisible()
  await page.waitForTimeout(2_000)

  await client.send('Profiler.start')
  await page.waitForTimeout(3_000)
  const { profile } = await client.send('Profiler.stop')

  const total = profile.nodes.reduce((sum, node) => sum + (node.hitCount ?? 0), 0)
  const idle = profile.nodes.reduce((sum, node) => sum + (node.callFrame.functionName === '(idle)' ? (node.hitCount ?? 0) : 0), 0)
  const busyRatio = total === 0 ? 0 : (total - idle) / total

  expect(total, 'profiler produced no samples').toBeGreaterThan(0)
  expect(busyRatio, `renderer was busy ${(busyRatio * 100).toFixed(1)}% of an idle 3s window`).toBeLessThan(0.5)
})
