import type { Locale, MessageCatalog } from '../../i18n'
import { formatCount, formatDateTime, formatStatus } from '../../lib/presentation'
import type { ArticleQuery } from '../../lib/api'

export interface ArticleQuerySummaryPart {
  readonly id: Exclude<keyof ArticleQuery, 'sorts'>
  readonly label: string
}

export interface ArticleQueryNames {
  readonly accounts?: ReadonlyMap<string, string>
  readonly albums?: ReadonlyMap<string, string>
}

export function getArticleQuerySummaryParts(query: ArticleQuery, locale: Locale, messages: MessageCatalog, names: ArticleQueryNames = {}): readonly ArticleQuerySummaryPart[] {
  const copy = messages.articles.ux
  const label = messages.articles.filters
  const parts: ArticleQuerySummaryPart[] = []
  const addText = (id: Extract<keyof ArticleQuery, string>, title: string, value: string | undefined) => {
    if (value?.trim()) parts.push({ id: id as ArticleQuerySummaryPart['id'], label: `${title}: ${value.trim()}` })
  }
  if (query.keyword?.trim()) parts.push({ id: 'keyword', label: `${messages.articles.search}: ${query.keyword.trim()}` })
  if (query.accountId) parts.push({ id: 'accountId', label: `${messages.articles.columns.account}: ${names.accounts?.get(query.accountId) ?? copy.accountUnavailable}` })
  if (query.albumId) parts.push({ id: 'albumId', label: `${label.albumId.replace(/\s*ID$/i, '')}: ${names.albums?.get(query.albumId) ?? copy.albumUnavailable}` })
  addText('author', label.author, query.author)
  if (query.state) parts.push({ id: 'state', label: `${label.state}: ${formatStatus(query.state, locale).label}` })
  if (query.publishedFrom) parts.push({ id: 'publishedFrom', label: `${copy.dateFrom}: ${formatDateTime(query.publishedFrom, locale)}` })
  if (query.publishedTo) parts.push({ id: 'publishedTo', label: `${copy.dateTo}: ${formatDateTime(query.publishedTo, locale)}` })

  const messageTypes = query.messageTypes?.map(String) ?? []
  if (messageTypes.length > 0) {
    parts.push({ id: 'messageTypes', label: `${label.messageTypes.replace(/\s*\([^)]*\)/, '')}: ${messageTypes.map((value) => copy.messageTypes[value] ?? value).join(', ')}` })
  }

  const booleanLabels: ReadonlyArray<[Extract<keyof ArticleQuery, 'deleted' | 'hasContent' | 'hasComments' | 'original' | 'paid'>, string]> = [
    ['hasContent', label.hasContent], ['hasComments', label.hasComments], ['deleted', label.deleted], ['original', label.original], ['paid', label.paid]
  ]
  for (const [id, title] of booleanLabels) if (query[id] !== undefined) parts.push({ id, label: `${title}: ${query[id] ? label.yes : label.no}` })

  const numberLabels: ReadonlyArray<[Extract<keyof ArticleQuery, 'readMin' | 'readMax' | 'oldLikeMin' | 'oldLikeMax' | 'shareMin' | 'shareMax' | 'likeMin' | 'likeMax' | 'commentMin' | 'commentMax' | 'weCoinMin' | 'weCoinMax' | 'mediaSecondsMin' | 'mediaSecondsMax'>, string]> = [
    ['readMin', label.readMin], ['readMax', label.readMax], ['oldLikeMin', label.oldLikeMin], ['oldLikeMax', label.oldLikeMax],
    ['shareMin', label.shareMin], ['shareMax', label.shareMax], ['likeMin', label.likeMin], ['likeMax', label.likeMax],
    ['commentMin', label.commentMin], ['commentMax', label.commentMax], ['weCoinMin', label.weCoinMin], ['weCoinMax', label.weCoinMax],
    ['mediaSecondsMin', label.mediaSecondsMin], ['mediaSecondsMax', label.mediaSecondsMax]
  ]
  for (const [id, title] of numberLabels) if (typeof query[id] === 'number') parts.push({ id, label: `${title}: ${formatCount(query[id], locale)}` })
  return parts
}

export function describeArticleQuery(query: ArticleQuery, locale: Locale, messages: MessageCatalog, names?: ArticleQueryNames): string {
  const parts = getArticleQuerySummaryParts(query, locale, messages, names)
  return parts.length > 0 ? parts.map((part) => part.label).join(' · ') : messages.articles.filters.any
}

export function hasArticleQueryFilters(query: ArticleQuery): boolean {
  return Object.entries(query).some(([field, value]) => field !== 'sorts' && (Array.isArray(value) ? value.length > 0 : value !== undefined && value !== ''))
}
