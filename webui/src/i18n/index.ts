import { useCallback, useEffect, useState } from 'react'
import { en } from './messages.en'
import { zhCN } from './messages.zh-CN'

export type Locale = 'en' | 'zh-CN'

export const messages = { en, 'zh-CN': zhCN } as const

export type MessageCatalog = {
  readonly product: { readonly name: string; readonly local: string; readonly privacy: string; readonly beta: string; readonly readOnly: string }
  readonly navigation: Record<'workspace' | 'library' | 'operations' | 'overview' | 'accounts' | 'articles' | 'albums' | 'savedQueries' | 'jobs' | 'settings', string>
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
  }
  readonly resources: {
    readonly accounts: ResourceMessages
    readonly albums: ResourceMessages
    readonly jobs: ResourceMessages
    readonly savedQueries: ResourceMessages
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
