import { Collapsible } from '@astryxdesign/core/Collapsible'
import { DateTimeInput, type ISODateTimeString } from '@astryxdesign/core/DateTimeInput'
import { MultiSelector } from '@astryxdesign/core/MultiSelector'
import { NumberInput } from '@astryxdesign/core/NumberInput'
import { Selector } from '@astryxdesign/core/Selector'
import { TextInput } from '@astryxdesign/core/TextInput'
import { AccountRemoteSelector, AlbumRemoteSelector } from '../../components/presentation'
import type { Locale, MessageCatalog } from '../../i18n'
import type { AccountOption, AlbumOption, ArticleQuery } from '../../lib/api'
import { formatStatus } from '../../lib/presentation'
import './ArticleTable.css'

interface ArticleFilterEditorProps {
  readonly locale: Locale
  readonly messages: MessageCatalog
  readonly value: ArticleQuery
  readonly onChange: (query: ArticleQuery) => void
  readonly onAccountOptionChange?: (option: AccountOption | undefined) => void
  readonly onAlbumOptionChange?: (option: AlbumOption | undefined) => void
  readonly selectedAccountLabel?: string
  readonly selectedAlbumLabel?: string
  readonly idPrefix?: string
}

export function ArticleFilterEditor({ locale, messages, value, onChange, onAccountOptionChange, onAlbumOptionChange, selectedAccountLabel, selectedAlbumLabel, idPrefix = 'article-filter' }: ArticleFilterEditorProps) {
  const copy = messages.articles.ux
  const update = <Key extends keyof ArticleQuery>(field: Key, next: ArticleQuery[Key] | undefined) => onChange({ ...value, [field]: next })
  const advancedCount = countAdvancedFilters(value)
  const stateOptions = ['ready', 'queued', 'failed'].map((state) => ({ value: state, label: formatStatus(state, locale).label }))

  return (
    <div className="article-filter-editor" data-testid={`${idPrefix}-editor`}>
      <div className="article-filter-defaults">
        <TextInput
          label={messages.articles.search}
          value={value.keyword ?? ''}
          placeholder={messages.articles.searchPlaceholder}
          hasClear
          onChange={(next) => update('keyword', next.trim() || undefined)}
        />
        <AccountRemoteSelector
          label={messages.articles.columns.account}
          description={copy.accountDescription}
          value={value.accountId}
          selectedLabel={selectedAccountLabel}
          onChange={(next, option) => {
            update('accountId', next)
            onAccountOptionChange?.(option)
          }}
          placeholder={messages.articles.filters.any}
          copy={{ unavailable: copy.accountUnavailable, noResults: copy.selectorNoResults, duplicate: copy.duplicateSelection }}
          testID={`${idPrefix}-account`}
        />
        <DateTimeInput
          label={copy.dateFrom}
          value={value.publishedFrom as ISODateTimeString | undefined}
          onChange={(next) => update('publishedFrom', next)}
          hasClear
          hourFormat="24h"
          timeIncrement={5}
        />
        <DateTimeInput
          label={copy.dateTo}
          value={value.publishedTo as ISODateTimeString | undefined}
          onChange={(next) => update('publishedTo', next)}
          hasClear
          hourFormat="24h"
          timeIncrement={5}
        />
        <Selector
          label={messages.articles.filters.state}
          options={stateOptions}
          value={value.state ?? null}
          onChange={(next) => update('state', next || undefined)}
          placeholder={messages.articles.filters.any}
          hasClear
        />
      </div>
      <Collapsible trigger={`${copy.moreFilters}${advancedCount > 0 ? ` (${advancedCount})` : ''}`} defaultIsOpen={advancedCount > 0}>
        <div className="article-filter-more">
          <p className="field-hint">{copy.moreFilterDescription}</p>
          <div className="article-filter-advanced-grid">
            <AlbumRemoteSelector
              label={messages.articles.filters.albumId.replace(/\s*ID$/i, '')}
              description={copy.albumDescription}
              value={value.albumId}
              selectedLabel={selectedAlbumLabel}
              accountID={value.accountId}
              onChange={(next, option) => {
                update('albumId', next)
                onAlbumOptionChange?.(option)
              }}
              placeholder={messages.articles.filters.any}
              copy={{ unavailable: copy.albumUnavailable, noResults: copy.selectorNoResults, duplicate: copy.duplicateSelection }}
              testID={`${idPrefix}-album`}
            />
            <TextInput label={messages.articles.filters.author} value={value.author ?? ''} hasClear onChange={(next) => update('author', next.trim() || undefined)} />
            <MultiSelector
              label={messages.articles.filters.messageTypes.replace(/\s*\([^)]*\)/, '')}
              options={Object.entries(copy.messageTypes).map(([value, label]) => ({ value, label }))}
              value={(value.messageTypes ?? []).map(String)}
              onChange={(next) => update('messageTypes', next.map(Number).filter((type) => Number.isInteger(type) && type >= 0))}
              placeholder={copy.messageTypePlaceholder}
              triggerDisplay="labels"
              hasClear
            />
            <BooleanFilter label={messages.articles.filters.hasContent} value={value.hasContent} onChange={(next) => update('hasContent', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.hasComments} value={value.hasComments} onChange={(next) => update('hasComments', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.deleted} value={value.deleted} onChange={(next) => update('deleted', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.original} value={value.original} onChange={(next) => update('original', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.paid} value={value.paid} onChange={(next) => update('paid', next)} messages={messages} />
            {numberFilters.map(({ field, label }) => (
              <NumberInput
                key={field}
                label={messages.articles.filters[label]}
                value={value[field]}
                onChange={(next) => update(field, next ?? undefined)}
                min={0}
                step={1}
                isIntegerOnly
                hasClear
              />
            ))}
          </div>
        </div>
      </Collapsible>
    </div>
  )
}

function BooleanFilter({ label, value, onChange, messages }: { readonly label: string; readonly value: boolean | undefined; readonly onChange: (value: boolean | undefined) => void; readonly messages: MessageCatalog }) {
  return <Selector label={label} options={[{ value: 'true', label: messages.articles.filters.yes }, { value: 'false', label: messages.articles.filters.no }]} value={value === undefined ? null : String(value)} onChange={(next) => onChange(next === null ? undefined : next === 'true')} placeholder={messages.articles.filters.any} hasClear />
}

function countAdvancedFilters(query: ArticleQuery): number {
  const fields: ReadonlyArray<keyof ArticleQuery> = ['albumId', 'author', 'messageTypes', 'hasContent', 'hasComments', 'deleted', 'original', 'paid', ...numberFilters.map((filter) => filter.field)]
  return fields.filter((field) => {
    const value = query[field]
    return Array.isArray(value) ? value.length > 0 : value !== undefined && value !== ''
  }).length
}

const numberFilters = [
  { field: 'readMin', label: 'readMin' }, { field: 'readMax', label: 'readMax' }, { field: 'oldLikeMin', label: 'oldLikeMin' }, { field: 'oldLikeMax', label: 'oldLikeMax' },
  { field: 'shareMin', label: 'shareMin' }, { field: 'shareMax', label: 'shareMax' }, { field: 'likeMin', label: 'likeMin' }, { field: 'likeMax', label: 'likeMax' },
  { field: 'commentMin', label: 'commentMin' }, { field: 'commentMax', label: 'commentMax' }, { field: 'weCoinMin', label: 'weCoinMin' }, { field: 'weCoinMax', label: 'weCoinMax' },
  { field: 'mediaSecondsMin', label: 'mediaSecondsMin' }, { field: 'mediaSecondsMax', label: 'mediaSecondsMax' }
] as const satisfies ReadonlyArray<{ readonly field: Extract<keyof ArticleQuery, string>; readonly label: keyof MessageCatalog['articles']['filters'] }>
