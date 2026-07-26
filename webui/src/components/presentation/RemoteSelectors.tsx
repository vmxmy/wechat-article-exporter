import { Combobox } from '@base-ui/react/combobox'
import { useCallback, useEffect, useId, useMemo, useReducer, useRef, useState } from 'react'
import { ControlField } from '@/components/controls/field-context'
import { SelectorClearButton, SelectorOptionContent, type SelectorCopy, type SelectorDisplayOption } from '@/components/controls/selector-parts'
import { selectorOptionClassName, selectorPopupClassName, selectorTriggerClassName } from '@/components/controls/selector-styles'
import { Icons } from '@/components/icons'
import { useDebounce } from '@/hooks/use-debounce'
import type { AccountOption, AlbumOption, ArticleOption } from '../../lib/api'
import { getAccountSelectorPage, getAlbumSelectorPage, getArticleSelectorPage } from '../../lib/api'

const selectorPageSize = 25
const debounceMs = 150

type RemoteSelectorCopy = Pick<SelectorCopy, 'clear' | 'loading' | 'noResults' | 'unavailable' | 'retry' | 'selected' | 'duplicate'>

function dedupeLabels(items: readonly SelectorDisplayOption[], copy: Pick<RemoteSelectorCopy, 'duplicate'>): readonly SelectorDisplayOption[] {
  const totals = new Map<string, number>()
  for (const item of items) totals.set(item.label, (totals.get(item.label) ?? 0) + 1)
  const positions = new Map<string, number>()
  return items.map((item) => {
    const total = totals.get(item.label) ?? 1
    const position = (positions.get(item.label) ?? 0) + 1
    positions.set(item.label, position)
    return total > 1 && !item.description ? { ...item, description: copy.duplicate(position, total) } : item
  })
}

interface RemoteSearchState {
  readonly items: readonly SelectorDisplayOption[]
  readonly isLoading: boolean
  readonly isError: boolean
  readonly retryGeneration: number
}

type RemoteSearchAction =
  | { readonly type: 'request' }
  | { readonly type: 'success'; readonly items: readonly SelectorDisplayOption[] }
  | { readonly type: 'failure' }
  | { readonly type: 'clear' }
  | { readonly type: 'reset' }
  | { readonly type: 'retry' }

function remoteSearchReducer(state: RemoteSearchState, action: RemoteSearchAction): RemoteSearchState {
  switch (action.type) {
    case 'request': return { ...state, isLoading: true, isError: false }
    case 'success': return { ...state, items: action.items, isLoading: false, isError: false }
    case 'failure': return { ...state, items: [], isLoading: false, isError: true }
    case 'clear': return { ...state, items: [], isLoading: false, isError: false }
    case 'reset': return { ...state, items: [], isLoading: false, isError: false, retryGeneration: state.retryGeneration + 1 }
    case 'retry': return { ...state, retryGeneration: state.retryGeneration + 1 }
  }
}

function useRemoteSearch<T extends { readonly id: string }>({ loadPage, toItem, copy, scope }: {
  readonly loadPage: (search: string, signal: AbortSignal) => Promise<readonly T[]>
  readonly toItem: (option: T) => SelectorDisplayOption
  readonly copy: Pick<RemoteSelectorCopy, 'duplicate'>
  readonly scope: string
}) {
  const controller = useRef<AbortController | null>(null)
  const trackedByID = useRef(new Map<string, T>())
  const [search, setSearch] = useState('')
  const [state, dispatch] = useReducer(remoteSearchReducer, { items: [], isLoading: false, isError: false, retryGeneration: 0 })
  const request = useMemo(() => ({ scope, search }), [scope, search])
  const debouncedRequest = useDebounce(request, debounceMs)

  useEffect(() => {
    if (debouncedRequest.scope !== scope) return

    controller.current?.abort()
    const next = new AbortController()
    controller.current = next
    dispatch({ type: 'request' })
    void loadPage(debouncedRequest.search, next.signal).then((loaded) => {
      if (controller.current !== next || next.signal.aborted) return
      for (const option of loaded) trackedByID.current.set(option.id, option)
      dispatch({ type: 'success', items: dedupeLabels(loaded.map(toItem), copy).slice(0, selectorPageSize) })
    }).catch((error: unknown) => {
      if (controller.current !== next || next.signal.aborted || (error instanceof DOMException && error.name === 'AbortError')) return
      dispatch({ type: 'failure' })
    })
  }, [copy, debouncedRequest, loadPage, scope, state.retryGeneration, toItem])

  useEffect(() => () => controller.current?.abort(), [])
  const clear = () => {
    controller.current?.abort()
    trackedByID.current.clear()
    dispatch({ type: 'clear' })
  }
  const reset = () => {
    clear()
    setSearch('')
    dispatch({ type: 'reset' })
  }
  return { search, setSearch, ...state, trackedByID, clear, reset, retry: () => dispatch({ type: 'retry' }) }
}

interface RemoteSelectorBaseProps {
  readonly label: string
  readonly description?: string
  readonly placeholder: string
  readonly value?: string
  readonly selectedLabel?: string
  readonly isDisabled?: boolean
  readonly testID?: string
}

function SearchStatus<T extends { readonly id: string }>({ search, copy }: { readonly search: ReturnType<typeof useRemoteSearch<T>>; readonly copy: RemoteSelectorCopy }) {
  return <>
    {search.isLoading && search.items.length === 0 ? <p className="px-2 py-3 text-sm text-muted-foreground" role="status">{copy.loading}</p> : null}
    {search.isError ? <div className="px-2 py-3 text-sm" role="alert"><p>{copy.unavailable}</p><button type="button" className="mt-2 text-sm font-medium underline" onClick={search.retry}>{copy.retry}</button></div> : null}
    {!search.isLoading && !search.isError && search.items.length === 0 ? <Combobox.Empty className="px-2 py-6 text-center text-sm text-muted-foreground">{copy.noResults}</Combobox.Empty> : null}
  </>
}

type RemoteSelectorProps<T extends AccountOption | AlbumOption> = RemoteSelectorBaseProps & {
  readonly onChange: (id: string | undefined, option?: T) => void
  readonly copy: RemoteSelectorCopy
  readonly loadPage: (search: string, signal: AbortSignal) => Promise<readonly T[]>
  readonly toItem: (option: T) => SelectorDisplayOption
  readonly scope: string
}

function RemoteSelector<T extends AccountOption | AlbumOption>(props: RemoteSelectorProps<T>) {
  return <ScopedRemoteSelector key={props.scope} {...props} />
}

function ScopedRemoteSelector<T extends AccountOption | AlbumOption>({ label, description, placeholder, value, selectedLabel, onChange, copy, isDisabled, testID, loadPage, toItem, scope }: RemoteSelectorProps<T>) {
  const search = useRemoteSearch<T>({ loadPage, toItem, copy, scope })
  const labelId = useId()
  const descriptionId = description ? `${labelId}-description` : undefined
  const inputRef = useRef<HTMLInputElement>(null)
  const [typedQuery, setTypedQuery] = useState('')
  const [isOpen, setIsOpen] = useState(false)
  const inputValue = isOpen ? typedQuery : value ? selectedLabel ?? copy.unavailable : ''

  return <ControlField label={label} description={description} labelId={labelId} descriptionId={descriptionId}><div className="flex items-center gap-2">
    <Combobox.Root value={value ?? null} onValueChange={(next) => { if (next === null) onChange(undefined); else { const option = search.trackedByID.current.get(next); if (option) { onChange(next, option); setIsOpen(false) } } }} inputValue={inputValue} onInputValueChange={(next) => { search.clear(); setTypedQuery(next); search.setSearch(next) }} open={isOpen} onOpenChange={(open) => { setIsOpen(open); if (open) { setTypedQuery(''); search.reset() } }} disabled={isDisabled}>
      <div className="relative w-full"><Combobox.Input ref={inputRef} aria-labelledby={labelId} aria-describedby={descriptionId} data-testid={testID} placeholder={placeholder} className={`${selectorTriggerClassName} w-full pr-9 placeholder:text-muted-foreground`} />{search.isLoading ? <Icons.spinner className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-muted-foreground" aria-hidden="true" /> : <Icons.chevronsUpDown className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 opacity-50" aria-hidden="true" />}</div>
      <Combobox.Portal><Combobox.Positioner align="start" className="z-50"><Combobox.Popup aria-busy={search.isLoading || undefined} className={`${selectorPopupClassName} w-(--anchor-width) min-w-72 p-1 outline-none data-[side=bottom]:translate-y-1`}><SearchStatus search={search} copy={copy} /><Combobox.List>{search.items.map((item) => <Combobox.Item key={item.value} value={item.value} disabled={search.isLoading} className={selectorOptionClassName}><span className="absolute right-2 flex size-3.5 items-center justify-center"><Combobox.ItemIndicator><Icons.check className="size-4" /></Combobox.ItemIndicator></span><SelectorOptionContent option={item} /></Combobox.Item>)}</Combobox.List></Combobox.Popup></Combobox.Positioner></Combobox.Portal>
    </Combobox.Root>
    {value ? <SelectorClearButton label={label} copy={copy} isDisabled={isDisabled} onClear={() => onChange(undefined)} restoreFocusRef={inputRef} /> : null}
  </div></ControlField>
}

export function AccountRemoteSelector(props: RemoteSelectorBaseProps & { readonly onChange: (id: string | undefined, option?: AccountOption) => void; readonly copy: RemoteSelectorCopy }) {
  const loadPage = useCallback((search: string, signal: AbortSignal) => getAccountSelectorPage({ page: 1, pageSize: selectorPageSize, search }, signal).then((page) => page.data), [])
  const toItem = useCallback((option: AccountOption): SelectorDisplayOption => ({ value: option.id, label: option.displayName?.trim() || props.copy.unavailable, description: option.alias?.trim() || undefined }), [props.copy.unavailable])
  return <RemoteSelector<AccountOption> {...props} loadPage={loadPage} toItem={toItem} scope="accounts" />
}

export function AlbumRemoteSelector({ accountID, ...props }: RemoteSelectorBaseProps & { readonly accountID?: string; readonly onChange: (id: string | undefined, option?: AlbumOption) => void; readonly copy: RemoteSelectorCopy }) {
  const loadPage = useCallback((search: string, signal: AbortSignal) => getAlbumSelectorPage({ page: 1, pageSize: selectorPageSize, search, accountId: accountID }, signal).then((page) => page.data), [accountID])
  const toItem = useCallback((option: AlbumOption): SelectorDisplayOption => ({ value: option.id, label: option.displayName?.trim() || props.copy.unavailable, description: option.accountName?.trim() || undefined }), [props.copy.unavailable])
  return <RemoteSelector<AlbumOption> {...props} loadPage={loadPage} toItem={toItem} scope={`albums:${accountID ?? ''}`} />
}

export function ArticleRemoteMultiSelector({ label, description, placeholder, selected, onChange, copy }: { readonly label: string; readonly description?: string; readonly placeholder: string; readonly selected: readonly ArticleOption[]; readonly onChange: (items: readonly ArticleOption[]) => void; readonly copy: RemoteSelectorCopy }) {
  const labelId = useId()
  const descriptionId = description ? `${labelId}-description` : undefined
  const inputRef = useRef<HTMLInputElement>(null)
  const loadPage = useCallback((search: string, signal: AbortSignal) => getArticleSelectorPage({ page: 1, pageSize: selectorPageSize, search }, signal).then((page) => page.data), [])
  const toItem = useCallback((option: ArticleOption): SelectorDisplayOption => ({ value: option.id, label: option.title, description: option.accountName?.trim() || undefined }), [])
  const search = useRemoteSearch<ArticleOption>({ loadPage, toItem, copy, scope: 'articles' })
  const [inputValue, setInputValue] = useState('')
  const [isOpen, setIsOpen] = useState(false)

  return <ControlField label={label} description={description} labelId={labelId} descriptionId={descriptionId}><div className="flex items-center gap-2">
    <Combobox.Root multiple value={selected.map((option) => option.id)} onValueChange={(ids) => { const existing = new Map(selected.map((option) => [option.id, option])); const next = ids.map((id) => existing.get(id) ?? search.trackedByID.current.get(id)).filter((option): option is ArticleOption => option !== undefined); if (next.length === ids.length) onChange(next) }} inputValue={inputValue} onInputValueChange={(next) => { search.clear(); setInputValue(next); search.setSearch(next) }} open={isOpen} onOpenChange={(open) => { setIsOpen(open); search.reset() }}>
      <div className="relative w-full"><Combobox.Input ref={inputRef} aria-labelledby={labelId} aria-describedby={descriptionId} placeholder={selected.length > 0 ? copy.selected(selected.length) : placeholder} className={`${selectorTriggerClassName} w-full pr-9 placeholder:text-muted-foreground`} />{search.isLoading ? <Icons.spinner className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 animate-spin text-muted-foreground" aria-hidden="true" /> : <Icons.chevronsUpDown className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 opacity-50" aria-hidden="true" />}</div>
      <Combobox.Portal><Combobox.Positioner align="start" className="z-50"><Combobox.Popup aria-busy={search.isLoading || undefined} className={`${selectorPopupClassName} w-(--anchor-width) min-w-72 p-1 outline-none data-[side=bottom]:translate-y-1`}><SearchStatus search={search} copy={copy} /><Combobox.List>{search.items.map((item) => <Combobox.Item key={item.value} value={item.value} disabled={search.isLoading} className={selectorOptionClassName}><span className="absolute right-2 flex size-3.5 items-center justify-center"><Combobox.ItemIndicator><Icons.check className="size-4" /></Combobox.ItemIndicator></span><SelectorOptionContent option={item} /></Combobox.Item>)}</Combobox.List></Combobox.Popup></Combobox.Positioner></Combobox.Portal>
    </Combobox.Root>
    {selected.length > 0 ? <SelectorClearButton label={label} copy={copy} onClear={() => onChange([])} restoreFocusRef={inputRef} /> : null}
  </div></ControlField>
}
