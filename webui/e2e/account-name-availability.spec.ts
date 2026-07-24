import { expect, test } from '@playwright/test'
import { expectOnlyLoopbackRequests, installLoopbackFixture } from './fixtures/loopback-api'

test('article and album lists explain unavailable account names without exposing opaque account IDs', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/articles?**', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      data: [{
        id: 'article-with-unavailable-account-name',
        title: 'Article with unavailable account name',
        accountId: 'opaque-article-account-id',
        accountNameAvailable: false,
        publishedAt: '2026-07-24T09:30:00.000Z',
        state: 'ready'
      }],
      pagination: { page: 1, pageSize: 25, total: 1 }
    })
  }))
  await page.route('**/api/v1/albums?**', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      data: [{
        id: 'album-with-unavailable-account-name',
        accountId: 'opaque-album-account-id',
        accountNameAvailable: false,
        name: 'Album with unavailable account name',
        articleCount: 1,
        paid: false
      }],
      pagination: { page: 1, pageSize: 25, total: 1 }
    })
  }))

  await page.goto('/articles')
  const articleTable = page.getByRole('table')
  await expect(articleTable).toContainText('Account name unavailable')
  await expect(articleTable).not.toContainText('opaque-article-account-id')

  await page.goto('/albums')
  const albumTable = page.getByRole('table')
  await expect(albumTable).toContainText('Account name unavailable')
  await expect(albumTable).not.toContainText('opaque-album-account-id')
  await expectOnlyLoopbackRequests(page)
})

test('article and album lists localize unavailable account names in Chinese', async ({ page }) => {
  await installLoopbackFixture(page)
  await page.route('**/api/v1/articles?**', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      data: [{
        id: 'article-with-unavailable-account-name',
        title: 'Article with unavailable account name',
        accountId: 'opaque-article-account-id',
        accountNameAvailable: false,
        publishedAt: '2026-07-24T09:30:00.000Z',
        state: 'ready'
      }],
      pagination: { page: 1, pageSize: 25, total: 1 }
    })
  }))
  await page.route('**/api/v1/albums?**', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      data: [{
        id: 'album-with-unavailable-account-name',
        accountId: 'opaque-album-account-id',
        accountNameAvailable: false,
        name: 'Album with unavailable account name',
        articleCount: 1,
        paid: false
      }],
      pagination: { page: 1, pageSize: 25, total: 1 }
    })
  }))

  await page.goto('/articles')
  await page.getByRole('button', { name: '切换至简体中文' }).click()
  await expect(page.getByRole('table')).toContainText('账号名称暂不可用')
  await expect(page.locator('body')).not.toContainText('opaque-article-account-id')

  await page.route('**/api/v1/settings/preferences', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify(preferencesWithLanguage('zh-CN'))
  }))
  await page.reload()
  await page.goto('/albums')
  await expect(page.getByRole('table')).toContainText('账号名称暂不可用')
  await expect(page.locator('body')).not.toContainText('opaque-album-account-id')
  await expectOnlyLoopbackRequests(page)
})

function preferencesWithLanguage(language: 'en' | 'zh-CN') {
  return {
    sync: { range: 'all', pageDelay: 1, jitter: 0, pageSize: 20, incremental: true, unsafePacingSaved: false },
    download: { concurrency: 2, forceContent: false, metadataOverridesContent: false },
    export: { namingTemplate: '{title}', maximumNameBytes: 180, collisionPolicy: 'suffix', excelIncludeContent: true, jsonIncludeContent: true, jsonIncludeComments: false, htmlIncludeComments: false },
    display: { noColor: false, ascii: false, plain: false, hideDeleted: false, language },
    proxy: { directFirst: true, fallbackEnabled: false }
  }
}
