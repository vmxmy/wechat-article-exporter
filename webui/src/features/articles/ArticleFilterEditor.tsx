import { Collapsible } from '@/components/controls/Collapsible'
import { DateTimeInput, type ISODateTimeString } from '@/components/controls/DateTimeInput'
import { MultiSelector } from '@/components/controls/MultiSelector'
import { NumberInput } from '@/components/controls/NumberInput'
import { Selector } from '@/components/controls/Selector'
import { TextInput } from '@/components/controls/TextInput'
import { AccountRemoteSelector, AlbumRemoteSelector, FieldHint, FormGrid, FormGridFullSpan } from '../../components/presentation'
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
      <FormGrid columns={2}>
        <FormGridFullSpan>
          <TextInput
            label={messages.articles.search}
            value={value.keyword ?? ''}
            placeholder={messages.articles.searchPlaceholder}
            htmlName="article-keyword"
            hasClear
            onChange={(next) => update('keyword', next.trim() || undefined)}
          />
        </FormGridFullSpan>
        <AccountRemoteSelector
          label={messages.articles.columns.account}
          description={copy.accountDescription}
          value={value.accountId}
          selectedLabel={selectedAccountLabel}
          onChange={(next, option) => {
            onChange({ ...value, accountId: next, albumId: next === value.accountId ? value.albumId : undefined })
            onAccountOptionChange?.(option)
            if (next !== value.accountId) onAlbumOptionChange?.(undefined)
          }}
          placeholder={messages.articles.filters.any}
          copy={messages.selectors}
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
          htmlName="article-state"
          placeholder={messages.articles.filters.any}
          hasClear
          clearLabel={messages.selectors.clear(messages.articles.filters.state)}
        />
      </FormGrid>
      <Collapsible trigger={`${copy.moreFilters}${advancedCount > 0 ? ` (${advancedCount})` : ''}`} defaultIsOpen={advancedCount > 0}>
        <div className="article-filter-more">
          <FieldHint>{copy.moreFilterDescription}</FieldHint>
          <FormGrid columns={2}>
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
              copy={messages.selectors}
              testID={`${idPrefix}-album`}
            />
            <TextInput label={messages.articles.filters.author} value={value.author ?? ''} htmlName="article-author" hasClear onChange={(next) => update('author', next.trim() || undefined)} />
            <MultiSelector
              label={messages.articles.filters.messageTypes.replace(/\s*\([^)]*\)/, '')}
              options={Object.entries(copy.messageTypes).map(([value, label]) => ({ value, label }))}
              value={(value.messageTypes ?? []).map(String)}
              onChange={(next) => update('messageTypes', next.map(Number).filter((type) => Number.isInteger(type) && type >= 0))}
              placeholder={copy.messageTypePlaceholder}
              copy={messages.selectors}
              triggerDisplay="labels"
              hasClear
            />
            <BooleanFilter label={messages.articles.filters.hasContent} value={value.hasContent} onChange={(next) => update('hasContent', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.hasComments} value={value.hasComments} onChange={(next) => update('hasComments', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.deleted} value={value.deleted} onChange={(next) => update('deleted', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.original} value={value.original} onChange={(next) => update('original', next)} messages={messages} />
            <BooleanFilter label={messages.articles.filters.paid} value={value.paid} onChange={(next) => update('paid', next)} messages={messages} />
            {rangePairs.map((pair) => (
              <RangeFilter
                key={pair.min}
                label={messages.articles.filters[pair.rangeLabel]}
                rangeFromLabel={messages.articles.filters.rangeFrom}
                rangeToLabel={messages.articles.filters.rangeTo}
                minValue={value[pair.min]}
                maxValue={value[pair.max]}
                onMinChange={(next) => update(pair.min, next)}
                onMaxChange={(next) => update(pair.max, next)}
                minField={pair.min}
                maxField={pair.max}
              />
            ))}
          </FormGrid>
        </div>
      </Collapsible>
    </div>
  )
}

function BooleanFilter({ label, value, onChange, messages }: { readonly label: string; readonly value: boolean | undefined; readonly onChange: (value: boolean | undefined) => void; readonly messages: MessageCatalog }) {
  return <Selector label={label} options={[{ value: 'true', label: messages.articles.filters.yes }, { value: 'false', label: messages.articles.filters.no }]} value={value === undefined ? null : String(value)} onChange={(next) => onChange(next === null ? undefined : next === 'true')} placeholder={messages.articles.filters.any} hasClear clearLabel={messages.selectors.clear(label)} />
}

function RangeFilter({ label, rangeFromLabel, rangeToLabel, minValue, maxValue, onMinChange, onMaxChange, minField, maxField }: {
  readonly label: string
  readonly rangeFromLabel: string
  readonly rangeToLabel: string
  readonly minValue: number | undefined
  readonly maxValue: number | undefined
  readonly onMinChange: (next: number | undefined) => void
  readonly onMaxChange: (next: number | undefined) => void
  readonly minField: Extract<keyof ArticleQuery, string>
  readonly maxField: Extract<keyof ArticleQuery, string>
}) {
  return (
    <fieldset className="article-filter-range">
      <legend>{label}</legend>
      <div className="article-filter-range-controls">
        <NumberInput label={rangeFromLabel} value={minValue} onChange={(next) => onMinChange(next ?? undefined)} htmlName={`article-${minField}`} autoComplete="off" min={0} step={1} isIntegerOnly hasClear />
        <NumberInput label={rangeToLabel} value={maxValue} onChange={(next) => onMaxChange(next ?? undefined)} htmlName={`article-${maxField}`} autoComplete="off" min={0} step={1} isIntegerOnly hasClear />
      </div>
    </fieldset>
  )
}

function countAdvancedFilters(query: ArticleQuery): number {
  const fields: ReadonlyArray<keyof ArticleQuery> = ['albumId', 'author', 'messageTypes', 'hasContent', 'hasComments', 'deleted', 'original', 'paid', ...rangePairs.flatMap((pair) => [pair.min, pair.max])]
  return fields.filter((field) => {
    const value = query[field]
    return Array.isArray(value) ? value.length > 0 : value !== undefined && value !== ''
  }).length
}

const rangePairs = [
  { min: 'readMin', max: 'readMax', rangeLabel: 'rangeReads' },
  { min: 'oldLikeMin', max: 'oldLikeMax', rangeLabel: 'rangeOldLikes' },
  { min: 'likeMin', max: 'likeMax', rangeLabel: 'rangeLikes' },
  { min: 'shareMin', max: 'shareMax', rangeLabel: 'rangeShares' },
  { min: 'commentMin', max: 'commentMax', rangeLabel: 'rangeComments' },
  { min: 'weCoinMin', max: 'weCoinMax', rangeLabel: 'rangeWeCoin' },
  { min: 'mediaSecondsMin', max: 'mediaSecondsMax', rangeLabel: 'rangeMediaSeconds' }
] as const satisfies ReadonlyArray<{ readonly min: Extract<keyof ArticleQuery, string>; readonly max: Extract<keyof ArticleQuery, string>; readonly rangeLabel: keyof MessageCatalog['articles']['filters'] }>
