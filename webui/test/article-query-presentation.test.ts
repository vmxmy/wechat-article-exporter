import { describe, expect, it } from 'vitest'
import { en } from '../src/i18n/messages.en'
import { zhCN } from '../src/i18n/messages.zh-CN'
import { describeArticleQuery, getArticleQuerySummaryParts, hasArticleQueryFilters } from '../src/features/articles/articleQueryPresentation'

describe('article query presentation', () => {
  it('uses readable names, localized state, dates, and metrics in an active-filter summary', () => {
    const accounts = new Map([['account-1', 'Fixture Account']])
    const albums = new Map([['album-1', 'Fixture collection']])
    const parts = getArticleQuerySummaryParts({
      keyword: 'sanitized',
      accountId: 'account-1',
      albumId: 'album-1',
      state: 'ready',
      publishedFrom: '2026-07-24T10:00:00Z',
      readMin: 120,
      messageTypes: [6]
    }, 'en', en, { accounts, albums })

    expect(parts.map((part) => part.label)).toEqual(expect.arrayContaining([
      'Search articles: sanitized',
      'Account: Fixture Account',
      'Album: Fixture collection',
      'State: Ready',
      'Minimum reads: 120',
      'Message types: Article'
    ]))
    expect(describeArticleQuery({ accountId: 'account-1' }, 'en', en, { accounts })).toBe('Account: Fixture Account')
  })

  it('recognizes filters without treating sorts as an active condition', () => {
    expect(hasArticleQueryFilters({ sorts: [{ field: 'publishedAt', direction: 'desc' }] })).toBe(false)
    expect(hasArticleQueryFilters({ hasContent: false })).toBe(true)
  })

  it('uses the active typed catalog for bilingual selector names and message types', () => {
    const parts = getArticleQuerySummaryParts({ accountId: 'account-1', albumId: 'album-1', messageTypes: [1, 6] }, 'zh-CN', zhCN, {
      accounts: new Map([['account-1', '示例账号']]),
      albums: new Map([['album-1', '示例专辑']])
    })

    expect(parts.map((part) => part.label)).toEqual([
      '账号: 示例账号',
      '专辑: 示例专辑',
      '消息类型（逗号分隔）: 文字消息, 图文文章'
    ])
  })
})
