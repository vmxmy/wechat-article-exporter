import { describe, expect, it } from 'vitest'
import {
  clearExportBrowserDraft,
  createExportWorkflowID,
  earliestValidExportStage,
  loadExportBrowserDraft,
  parseArticleBrowserView,
  parseExportBrowserView,
  saveExportBrowserDraft,
  serializeArticleBrowserView,
  serializeExportBrowserView,
  type ExportBrowserDraft
} from '../src/lib/browserViewState'

describe('article browser view state', () => {
  it('parses the applied query, sort, and page and serializes one canonical URL', () => {
    const parsed = parseArticleBrowserView('?keyword=%20release%20&deleted=false&messageType=5&messageType=1&sort=title%3Aasc&page=3&from=other')

    expect(parsed.state).toEqual({
      query: { keyword: 'release', deleted: false, messageTypes: [1, 5] },
      sort: { field: 'title', direction: 'asc' },
      page: 3
    })
    expect(parsed.canonicalSearch).toBe('?from=other&keyword=release&deleted=false&messageType=1&messageType=5&sort=title%3Aasc&page=3')
    expect(parsed.needsReplace).toBe(true)
  })

  it('omits defaults and discards invalid owned parameters without touching foreign parameters', () => {
    const parsed = parseArticleBrowserView('?embed=compact&page=0&sort=secret%3Asideways&hasContent=maybe&readMin=20&readMax=10&likeMin=5&publishedFrom=bad')

    expect(parsed.state).toEqual({ query: { likeMin: 5 }, sort: { field: 'publishedAt', direction: 'desc' }, page: 1 })
    expect(parsed.canonicalSearch).toBe('?embed=compact&likeMin=5')
    expect(parsed.needsReplace).toBe(true)
    expect(serializeArticleBrowserView(parsed.state, '?embed=compact&keyword=stale')).toBe('?embed=compact&likeMin=5')
  })

  it('keeps draft-only state out of the serialized applied view', () => {
    expect(serializeArticleBrowserView({
      query: { accountId: 'account / fixture', keyword: 'visible' },
      sort: { field: 'publishedAt', direction: 'desc' },
      page: 1
    }, '?dialog=article-1&selection=article-2')).toBe('?accountId=account+%2F+fixture&keyword=visible')
  })
})

describe('export browser view state', () => {
  it('allows only stage, scope type, and format in the canonical URL', () => {
    const parsed = parseExportBrowserView('?stage=destination&scope=savedQuery&format=pdf&directoryToken=secret&path=%2Ftmp%2Fprivate&confirm=exact')

    expect(parsed.state).toEqual({ stage: 'destination', scope: 'savedQuery', format: 'pdf' })
    expect(parsed.specified).toEqual({ stage: true, scope: true, format: true })
    expect(parsed.canonicalSearch).toBe('?stage=destination&scope=savedQuery&format=pdf')
    expect(serializeExportBrowserView(parsed.state, '?directoryToken=secret&path=%2Ftmp%2Fprivate&confirm=exact')).toBe('?stage=destination&scope=savedQuery&format=pdf')
  })

  it('falls back invalid values to compact defaults', () => {
    const parsed = parseExportBrowserView('?stage=done&scope=filesystem&format=zip')

    expect(parsed.state).toEqual({ stage: 'scope', scope: 'articles', format: 'markdown' })
    expect(parsed.canonicalSearch).toBe('')
    expect(parsed.needsReplace).toBe(true)
  })

  it('distinguishes an explicit default scope from an omitted scope', () => {
    expect(parseExportBrowserView('?scope=articles').specified.scope).toBe(true)
    expect(parseExportBrowserView('').specified.scope).toBe(false)
  })

  it('round-trips only a bounded opaque workflow key', () => {
    const workflow = createExportWorkflowID(() => '12345678-1234-1234-1234-1234567890ab')
    expect(workflow).toBe('123456781234123412341234567890ab')
    expect(parseExportBrowserView(`?flow=${workflow}`).state.workflow).toBe(workflow)
    expect(parseExportBrowserView('?flow=article-fixture-1').state.workflow).toBeUndefined()
  })

  it('falls back to the earliest stage whose prerequisites are available', () => {
    expect(earliestValidExportStage('destination', false, true)).toBe('scope')
    expect(earliestValidExportStage('destination', true, false)).toBe('format')
    expect(earliestValidExportStage('destination', true, true)).toBe('destination')
  })
})

describe('bounded export session draft', () => {
  it('round-trips recoverable selections and non-sensitive options', () => {
    const storage = memoryStorage()
    const workflow = '123456781234123412341234567890ab'
    const draft: ExportBrowserDraft = {
      selection: { kind: 'explicit_ids', articleIds: ['article-1', 'article-2'] },
      selectionLabel: '2 selected articles',
      selectedArticles: [
        { id: 'article-1', title: 'One', accountNameAvailable: true, accountName: 'Fixture' },
        { id: 'article-2', title: 'Two', accountNameAvailable: false }
      ],
      options: {
        namingTemplate: '{published}-{title}', maximumNameBytes: 180, collisionPolicy: 'suffix',
        includeContent: true, includeMetadata: false, includeComments: true, htmlResourcePolicy: 'strict'
      }
    }

    saveExportBrowserDraft(workflow, draft, storage)
    expect(loadExportBrowserDraft(workflow, storage)).toEqual(draft)
    expect(storage.value()).not.toContain('directoryToken')
    expect(storage.value()).not.toContain('confirmation')
    expect(storage.value()).not.toContain('subdirectory')

    clearExportBrowserDraft(workflow, storage)
    expect(loadExportBrowserDraft(workflow, storage)).toBeUndefined()
  })

  it('preserves a validated matching-query sort in the session draft', () => {
    const storage = memoryStorage()
    const workflow = '123456781234123412341234567890ab'
    saveExportBrowserDraft(workflow, {
      selection: { kind: 'all_matching', query: { keyword: 'fixture', sorts: [{ field: 'title', direction: 'asc' }] } },
      selectionLabel: 'Current results'
    }, storage)

    expect(loadExportBrowserDraft(workflow, storage)?.selection).toEqual({
      kind: 'all_matching', query: { keyword: 'fixture', sorts: [{ field: 'title', direction: 'asc' }] }
    })
  })

  it('drops path-like or oversized strings and rejects stale malformed selections', () => {
    const storage = memoryStorage()
    const workflow = '123456781234123412341234567890ab'
    storage.setItem(`wechat-article.export-browser-draft.v2.${workflow}`, JSON.stringify({
      version: 1,
      selection: { kind: 'account', accountId: '' },
      selectionLabel: '/Users/private/output',
      options: {
        namingTemplate: '/Users/private/{title}', maximumNameBytes: 0, collisionPolicy: 'replace',
        includeContent: true, includeMetadata: true, includeComments: false, htmlResourcePolicy: 'best-effort',
        directoryToken: 'secret', confirmation: 'exact', articleContent: 'private body'
      }
    }))

    expect(loadExportBrowserDraft(workflow, storage)).toEqual({
      options: {
        namingTemplate: '{published}-{title}', maximumNameBytes: 180, collisionPolicy: 'replace',
        includeContent: true, includeMetadata: true, includeComments: false, htmlResourcePolicy: 'best-effort'
      }
    })
  })

  it('does not load a workflow draft for an independent bare visit', () => {
    const storage = memoryStorage()
    const workflow = '123456781234123412341234567890ab'
    saveExportBrowserDraft(workflow, { selection: { kind: 'account', accountId: 'account-1' } }, storage)

    expect(loadExportBrowserDraft(undefined, storage)).toBeUndefined()
    expect(loadExportBrowserDraft('abcdefabcdefabcdefabcdefabcdefab', storage)).toBeUndefined()
  })
})

function memoryStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
    value: () => [...values.values()].join('')
  }
}
