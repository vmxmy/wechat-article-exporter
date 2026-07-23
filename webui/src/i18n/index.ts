import { useCallback, useEffect, useState } from 'react'
import { en } from './messages.en'
import { zhCN } from './messages.zh-CN'

export type Locale = 'en' | 'zh-CN'

export const messages = { en, 'zh-CN': zhCN } as const

export type MessageCatalog = {
  readonly product: { readonly name: string; readonly local: string; readonly privacy: string; readonly beta: string; readonly readOnly: string }
  readonly navigation: Record<'workspace' | 'library' | 'operations' | 'overview' | 'login' | 'import' | 'accounts' | 'articles' | 'albums' | 'savedQueries' | 'jobs' | 'settings', string>
  readonly a11y: { readonly skip: string }
  readonly localeSwitch: string
  readonly connection: { readonly connected: string; readonly unavailable: string; readonly checking: string }
  readonly overview: {
    readonly title: string
    readonly description: string
    readonly profileTitle: string
    readonly profileDescription: string
    readonly nextTitle: string
    readonly nextDescription: string
    readonly runtimeTitle: string
    readonly sessionTitle: string
    readonly storageTitle: string
    readonly unavailable: string
    readonly sessionAccount: string
    readonly sessionState: string
    readonly runtimeProfile: string
    readonly runtimeVersion: string
    readonly storageCounts: (accounts: number, articles: number, albums: number, jobs: number) => string
  }
  readonly unavailableActions: { readonly confirmationTitle: string; readonly confirmationDescription: string; readonly apiUnavailable: string }
  readonly login: { readonly title: string; readonly description: string; readonly sessionTitle: string; readonly account: string; readonly state: string; readonly checking: string; readonly unavailable: string; readonly qrTitle: string; readonly qrDescription: string; readonly start: string; readonly poll: string; readonly complete: string; readonly states: Readonly<Record<string, string>> }
  readonly import: { readonly title: string; readonly description: string; readonly url: string; readonly placeholder: string; readonly submit: string; readonly force: string; readonly queued: (id: string) => string; readonly failed: string; readonly note: string }
  readonly articles: {
    readonly title: string
    readonly description: string
    readonly search: string
    readonly searchPlaceholder: string
    readonly selected: string
    readonly empty: string
    readonly loading: string
    readonly unavailable: string
    readonly retry: string
    readonly previous: string
    readonly next: string
    readonly page: (current: number, total: number) => string
    readonly pagination: string
    readonly visibleColumns: string
    readonly selectAll: string
    readonly selectRow: (title: string) => string
    readonly columns: { readonly title: string; readonly account: string; readonly published: string; readonly status: string }
    readonly actions: { readonly title: string; readonly description: string; readonly preview: string; readonly download: string; readonly saveQuery: string }
  }
  readonly resources: {
    readonly accounts: AccountResourceMessages
    readonly albums: ResourceMessages
    readonly jobs: JobResourceMessages
    readonly savedQueries: SavedQueryResourceMessages
  }
}

type ResourceMessages = {
  readonly title: string
  readonly description: string
  readonly loading: string
  readonly unavailable: string
  readonly empty: string
  readonly retry: string
  readonly previous: string
  readonly next: string
  readonly page: (current: number, total: number) => string
  readonly pagination: string
  readonly selected: string
  readonly selectAll: string
  readonly selectRow: (row: string) => string
  readonly visibleColumns: string
  readonly columns: Readonly<Record<string, string>>
}

type AccountResourceMessages = ResourceMessages & {
  readonly actions: { readonly title: string; readonly description: string; readonly search: string; readonly fakeid: string; readonly name: string; readonly alias: string; readonly discover: string; readonly add: string; readonly edit: string; readonly remove: string; readonly sync: string; readonly selectOne: string; readonly deleteConfirm: string; readonly actionFailed: string }
}

type JobResourceMessages = ResourceMessages & {
  readonly actions: { readonly title: string; readonly description: string; readonly start: string; readonly pause: string; readonly resume: string; readonly retry: string; readonly cancel: string; readonly selectOne: string; readonly confirmPause: string; readonly confirmRetry: string; readonly confirmCancel: string; readonly actionFailed: string }
}

type SavedQueryResourceMessages = ResourceMessages & {
  readonly actions: { readonly title: string; readonly description: string; readonly create: string; readonly edit: string; readonly remove: string }
}

export function useMessages(locale: Locale): MessageCatalog {
  return messages[locale]
}

export function useLocale(): readonly [Locale, (locale: Locale) => void] {
  const [locale, setLocaleState] = useState<Locale>(readInitialLocale)
  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])
  const setLocale = useCallback((nextLocale: Locale) => {
    try {
      window.localStorage.setItem('wechat-article.display.language', nextLocale)
    } catch {
      // A privacy-restricted browser can still use the selected in-memory locale.
    }
    setLocaleState(nextLocale)
  }, [])

  return [locale, setLocale]
}

function readInitialLocale(): Locale {
  let persisted: string | null = null
  try {
    persisted = window.localStorage.getItem('wechat-article.display.language')
  } catch {
    // Fall through to the browser preference when persistence is unavailable.
  }
  if (persisted === 'en' || persisted === 'zh-CN') {
    return persisted
  }

  return navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}
