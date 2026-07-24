import { Typeahead, TypeaheadItem, type SearchSource, type SearchableItem } from '@astryxdesign/core/Typeahead'
import { Tokenizer, type TokenizerChange } from '@astryxdesign/core/Tokenizer'
import { useCallback, useMemo, useRef, useState } from 'react'
import type { AccountOption, AlbumOption, ArticleOption } from '../../lib/api'
import { getAccountSelectorPage, getAlbumSelectorPage, getArticleSelectorPage } from '../../lib/api'

const selectorPageSize = 25

interface NamedSelectorItem extends SearchableItem<ArticleOption> {
  readonly description?: string
}

interface RemoteSelectorCopy {
  readonly unavailable: string
  readonly noResults: string
  readonly duplicate: (position: number, total: number) => string
}

interface RemoteSelectorProps<T extends AccountOption | AlbumOption> {
  readonly label: string
  readonly description?: string
  readonly placeholder: string
  readonly value?: string
  readonly selectedLabel?: string
  readonly onChange: (id: string | undefined, option?: T) => void
  readonly copy: RemoteSelectorCopy
  readonly isDisabled?: boolean
  readonly testID?: string
  readonly loadPage: (search: string, signal: AbortSignal) => Promise<readonly T[]>
  readonly toItem: (option: T, copy: RemoteSelectorCopy) => NamedSelectorItem
}

/**
 * Server-backed selector for locally stored resources.
 *
 * Astryx Selector only filters its already-rendered option list. This adapter
 * instead uses Astryx Typeahead's debounced, cancellable SearchSource so a
 * saved account or album outside the first bounded API page is still
 * discoverable. The opaque stable ID remains internal to the submitted value.
 */
function RemoteSelector<T extends AccountOption | AlbumOption>({
  label,
  description,
  placeholder,
  value,
  selectedLabel,
  onChange,
  copy,
  isDisabled,
  testID,
  loadPage,
  toItem
}: RemoteSelectorProps<T>) {
  const controller = useRef<AbortController | null>(null)
  const optionsByID = useRef(new Map<string, T>())
  const fallback = useCallback((id: string): NamedSelectorItem => ({ id, label: selectedLabel ?? copy.unavailable }), [copy.unavailable, selectedLabel])
  const [selected, setSelected] = useState<NamedSelectorItem | null>(() => value ? fallback(value) : null)
  const selectedValue = value && selected?.id === value ? selected : value ? fallback(value) : null

  const toItems = useCallback((options: readonly T[]) => {
    for (const option of options) optionsByID.current.set(option.id, option)
    const items = options.map((option) => ({ option, item: toItem(option, copy) }))
    const totals = new Map<string, number>()
    for (const { item } of items) totals.set(item.label, (totals.get(item.label) ?? 0) + 1)
    const positions = new Map<string, number>()
    return items.map(({ item }) => {
      const total = totals.get(item.label) ?? 1
      const position = (positions.get(item.label) ?? 0) + 1
      positions.set(item.label, position)
      return total > 1 && !item.description
        ? { ...item, description: copy.duplicate(position, total) }
        : item
    })
  }, [copy, toItem])

  const searchSource = useMemo<SearchSource<NamedSelectorItem>>(() => ({
    cancel: () => controller.current?.abort(),
    bootstrap: async () => {
      controller.current?.abort()
      const next = new AbortController()
      controller.current = next
      return toItems(await loadPage('', next.signal))
    },
    search: async (search) => {
      controller.current?.abort()
      const next = new AbortController()
      controller.current = next
      return toItems(await loadPage(search, next.signal))
    }
  }), [loadPage, toItems])

  const select = useCallback((item: NamedSelectorItem | null) => {
    setSelected(item)
    if (!item) {
      onChange(undefined)
      return
    }
    onChange(item.id, optionsByID.current.get(item.id) ?? optionFromSelectorItem<T>(item))
  }, [onChange])

  return (
    <Typeahead
      label={label}
      description={description}
      placeholder={placeholder}
      value={selectedValue}
      onChange={select}
      searchSource={searchSource}
      renderItem={(item) => <TypeaheadItem item={item} description={item.description} />}
      hasEntriesOnFocus
      maxMenuItems={selectorPageSize}
      debounceMs={150}
      emptySearchResultsText={copy.noResults}
      isDisabled={isDisabled}
      data-testid={testID}
    />
  )
}

interface RemoteSelectorBaseProps<T extends AccountOption | AlbumOption> {
  readonly label: string
  readonly description?: string
  readonly placeholder: string
  readonly value?: string
  readonly selectedLabel?: string
  readonly onChange: (id: string | undefined, option?: T) => void
  readonly copy: RemoteSelectorCopy
  readonly isDisabled?: boolean
  readonly testID?: string
}

type AccountSelectorProps = RemoteSelectorBaseProps<AccountOption>

export function AccountRemoteSelector(props: AccountSelectorProps) {
  const loadPage = useCallback(async (search: string, signal: AbortSignal) => (await getAccountSelectorPage({ page: 1, pageSize: selectorPageSize, search }, signal)).data, [])
  const toItem = useCallback((option: AccountOption, copy: RemoteSelectorCopy): NamedSelectorItem => ({
    id: option.id,
    label: option.displayName?.trim() || copy.unavailable,
    description: option.alias?.trim() || undefined
  }), [])
  return <RemoteSelector {...props} loadPage={loadPage} toItem={toItem} />
}

interface AlbumSelectorProps extends RemoteSelectorBaseProps<AlbumOption> {
  readonly accountID?: string
}

export function AlbumRemoteSelector({ accountID, ...props }: AlbumSelectorProps) {
  const loadPage = useCallback(async (search: string, signal: AbortSignal) => (await getAlbumSelectorPage({ page: 1, pageSize: selectorPageSize, search, accountId: accountID }, signal)).data, [accountID])
  const toItem = useCallback((option: AlbumOption, copy: RemoteSelectorCopy): NamedSelectorItem => ({
    id: option.id,
    label: option.displayName?.trim() || copy.unavailable,
    description: option.accountName?.trim() || undefined
  }), [])
  return <RemoteSelector key={props.value ?? 'empty'} {...props} loadPage={loadPage} toItem={toItem} />
}

interface ArticleSelectorProps {
  readonly label: string
  readonly description?: string
  readonly placeholder: string
  readonly selected: readonly ArticleOption[]
  readonly onChange: (items: readonly ArticleOption[]) => void
  readonly copy: RemoteSelectorCopy
}

export function ArticleRemoteMultiSelector({ label, description, placeholder, selected, onChange, copy }: ArticleSelectorProps) {
  const controller = useRef<AbortController | null>(null)
  const source = useMemo<SearchSource<NamedSelectorItem>>(() => ({
    cancel: () => controller.current?.abort(),
    bootstrap: async () => {
      controller.current?.abort()
      const next = new AbortController()
      controller.current = next
      return toArticleItems((await getArticleSelectorPage({ page: 1, pageSize: selectorPageSize }, next.signal)).data)
    },
    search: async (search) => {
      controller.current?.abort()
      const next = new AbortController()
      controller.current = next
      return toArticleItems((await getArticleSelectorPage({ page: 1, pageSize: selectorPageSize, search }, next.signal)).data)
    }
  }), [])
  const selectedItems = selected.map(articleToItem)
  const change = (items: NamedSelectorItem[], detail: TokenizerChange<NamedSelectorItem>) => {
    void detail
    onChange(items.map(itemToArticleOption))
  }
  return (
    <Tokenizer
      label={label}
      description={description}
      placeholder={placeholder}
      value={selectedItems}
      onChange={change}
      searchSource={source}
      renderItem={(item) => <TypeaheadItem item={item} description={item.description} />}
      hasEntriesOnFocus
      maxMenuItems={selectorPageSize}
      debounceMs={150}
      emptySearchResultsText={copy.noResults}
    />
  )
}

function articleToItem(article: ArticleOption): NamedSelectorItem { return { id: article.id, label: article.title, description: article.accountName?.trim() || undefined, auxiliaryData: article } }
function optionFromSelectorItem<T extends AccountOption | AlbumOption>(item: NamedSelectorItem): T {
  return { id: item.id, displayName: item.label, displayNameAvailable: item.label.trim() !== '' } as T
}
function itemToArticleOption(item: NamedSelectorItem): ArticleOption {
  const article = item.auxiliaryData as ArticleOption | undefined
  return article ?? { id: item.id, title: item.label, accountNameAvailable: Boolean(item.description), ...(item.description ? { accountName: item.description } : {}) }
}
function toArticleItems(articles: readonly ArticleOption[]) { return articles.map(articleToItem) }
