import { Button } from '@/components/controls/Button'
import { Collapsible } from '@/components/controls/Collapsible'
import { DateTimeInput, type ISODateTimeString } from '@/components/controls/DateTimeInput'
import { MultiSelector } from '@/components/controls/MultiSelector'
import { NumberInput } from '@/components/controls/NumberInput'
import { Selector } from '@/components/controls/Selector'
import { TextInput } from '@/components/controls/TextInput'
import { useControlId } from '@/components/controls/field-context'
import { useState, type ReactNode } from 'react'
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
  /** `inline` collapses the common filters into one toolbar row; `stacked` keeps the form grid. */
  readonly layout?: 'stacked' | 'inline'
  /** Apply/reset controls rendered at the end of the inline row. */
  readonly actions?: ReactNode
}

export function ArticleFilterEditor({ locale, messages, value, onChange, onAccountOptionChange, onAlbumOptionChange, selectedAccountLabel, selectedAlbumLabel, idPrefix = 'article-filter', layout = 'stacked', actions }: ArticleFilterEditorProps) {
  const copy = messages.articles.ux
  const isInline = layout === 'inline'
  const update = <Key extends keyof ArticleQuery>(field: Key, next: ArticleQuery[Key] | undefined) => onChange({ ...value, [field]: next })
  const advancedCount = countAdvancedFilters(value, isInline)
  const stateOptions = ['ready', 'queued', 'failed'].map((state) => ({ value: state, label: formatStatus(state, locale).label }))
  const moreLabel = `${copy.moreFilters}${advancedCount > 0 ? ` (${advancedCount})` : ''}`
  const moreID = useControlId('article-filter-more')
  const [isMoreOpen, setMoreOpen] = useState(advancedCount > 0)

  const keywordInput = <TextInput
    label={messages.articles.search}
    value={value.keyword ?? ''}
    placeholder={messages.articles.searchPlaceholder}
    htmlName="article-keyword"
    isLabelHidden={isInline}
    hasClear
    onChange={(next) => update('keyword', next.trim() || undefined)}
  />
  const accountSelector = <AccountRemoteSelector
    label={messages.articles.columns.account}
    description={isInline ? undefined : copy.accountDescription}
    value={value.accountId}
    selectedLabel={selectedAccountLabel}
    onChange={(next, option) => {
      onChange({ ...value, accountId: next, albumId: next === value.accountId ? value.albumId : undefined })
      onAccountOptionChange?.(option)
      if (next !== value.accountId) onAlbumOptionChange?.(undefined)
    }}
    placeholder={isInline ? messages.articles.columns.account : messages.articles.filters.any}
    isLabelHidden={isInline}
    copy={messages.selectors}
    testID={`${idPrefix}-account`}
  />
  const stateSelector = <Selector
    label={messages.articles.filters.state}
    options={stateOptions}
    value={value.state ?? null}
    onChange={(next) => update('state', next || undefined)}
    htmlName="article-state"
    placeholder={isInline ? messages.articles.filters.state : messages.articles.filters.any}
    isLabelHidden={isInline}
    hasClear
    clearLabel={messages.selectors.clear(messages.articles.filters.state)}
  />
  const dateInputs = <>
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
  </>

  const advancedFilters = (
    <div className="article-filter-more">
      <FieldHint>{copy.moreFilterDescription}</FieldHint>
      <FormGrid columns={2}>
        {isInline ? dateInputs : null}
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
  )

  if (isInline) {
    return <>
      <div className="article-filter-bar" data-testid={`${idPrefix}-editor`}>
        <div className="article-filter-bar-search">{keywordInput}</div>
        <div className="article-filter-bar-item">{accountSelector}</div>
        <div className="article-filter-bar-item article-filter-bar-item-narrow">{stateSelector}</div>
        <Button
          label={moreLabel}
          variant="secondary"
          size="sm"
          aria-expanded={isMoreOpen}
          aria-controls={moreID}
          onClick={() => setMoreOpen((current) => !current)}
        />
        {actions ? <div className="article-filter-bar-actions">{actions}</div> : null}
      </div>
      {isMoreOpen ? <div className="article-filter-panel" id={moreID}>{advancedFilters}</div> : null}
    </>
  }

  return (
    <div className="article-filter-editor" data-testid={`${idPrefix}-editor`}>
      <FormGrid columns={2}>
        <FormGridFullSpan>{keywordInput}</FormGridFullSpan>
        {accountSelector}
        {dateInputs}
        {stateSelector}
      </FormGrid>
      <Collapsible trigger={moreLabel} defaultIsOpen={advancedCount > 0}>{advancedFilters}</Collapsible>
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

function countAdvancedFilters(query: ArticleQuery, includesDates: boolean): number {
  const fields: ReadonlyArray<keyof ArticleQuery> = [
    ...(includesDates ? (['publishedFrom', 'publishedTo'] as const) : []),
    'albumId', 'author', 'messageTypes', 'hasContent', 'hasComments', 'deleted', 'original', 'paid',
    ...rangePairs.flatMap((pair) => [pair.min, pair.max])
  ]
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
