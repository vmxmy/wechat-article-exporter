import { expect, test } from '@playwright/test'
import { startLoopbackWorkspace, type LoopbackWorkspace } from './loopback-server'

let workspace: LoopbackWorkspace

test.beforeEach(async () => {
  workspace = await startLoopbackWorkspace()
})

test.afterEach(async () => {
  if (workspace) await workspace.close()
})

test('real Go loopback workspace bootstraps a local browser session and serves the embedded SPA', async ({ context, page }) => {
  const bootstrap = new URL(workspace.bootstrapURL)
  const requests: URL[] = []
  context.on('request', (request) => requests.push(new URL(request.url())))

  const bootstrapResponse = await page.goto(workspace.bootstrapURL)
  expect(bootstrapResponse?.status()).toBe(200)
  await expect(page).toHaveURL(`${bootstrap.origin}/`)
  expect(new URL(page.url()).searchParams.has('token')).toBe(false)

  const headers = bootstrapResponse?.headers() ?? {}
  expect(headers['cache-control']).toContain('no-store')
  expect(headers['content-security-policy']).toContain("default-src 'self'")
  expect(headers['content-security-policy']).toContain("connect-src 'self'")
  expect(headers['content-security-policy']).toContain("frame-ancestors 'none'")
  await expect(page.locator('#root')).toBeAttached()

  const cookies = await context.cookies(bootstrap.origin)
  expect(cookies).toEqual(expect.arrayContaining([
    expect.objectContaining({ name: 'wechat_article_session', httpOnly: true, sameSite: 'Strict' }),
    expect.objectContaining({ name: 'wechat_article_csrf', httpOnly: false, sameSite: 'Strict' })
  ]))

  const deepRoute = await page.goto(`${bootstrap.origin}/settings`)
  expect(deepRoute?.status()).toBe(200)
  expect(deepRoute?.headers()['cache-control']).toContain('no-store')
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()

  const preferencePatch = page.waitForResponse((response) => response.request().method() === 'PATCH' && response.url() === `${bootstrap.origin}/api/v1/settings/preferences`)
  await page.getByRole('button', { name: 'Save preferences' }).click()
  expect((await preferencePatch).status()).toBe(200)
  await expect(page.getByRole('status').filter({ hasText: 'Preferences saved.' })).toBeVisible()

  expect(requests).not.toHaveLength(0)
  expect(requests.every((request) => request.protocol === 'http:' && request.hostname === bootstrap.hostname && request.port === bootstrap.port)).toBe(true)
})
