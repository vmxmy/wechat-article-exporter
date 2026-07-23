import { useCallback, useEffect, useState } from 'react'
import { en } from './messages.en'
import { zhCN } from './messages.zh-CN'

export type Locale = 'en' | 'zh-CN'

export const messages = { en, 'zh-CN': zhCN } as const

export type MessageCatalog = {
  readonly product: { readonly name: string; readonly local: string; readonly privacy: string; readonly beta: string; readonly readOnly: string }
  readonly navigation: Record<'workspace' | 'library' | 'operations' | 'overview' | 'login' | 'import' | 'accounts' | 'articles' | 'albums' | 'savedQueries' | 'jobs' | 'exports' | 'settings', string>
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
  readonly exports: ExportMessages
  readonly settings: SettingsMessages
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
  readonly actions: { readonly title: string; readonly description: string; readonly preview: string; readonly previewUnavailable: string; readonly previewBlocked: string; readonly download: string; readonly metadata: string; readonly comments: string; readonly resources: string; readonly failed: string; readonly saveQuery: string }
  }
  readonly resources: {
    readonly accounts: AccountResourceMessages
    readonly albums: ResourceMessages & { readonly actions: { readonly title: string; readonly description: string; readonly traverse: string; readonly download: string; readonly selectOne: string; readonly queued: (id: string) => string; readonly failed: string } }
    readonly jobs: JobResourceMessages
    readonly savedQueries: SavedQueryResourceMessages
  }
}

type ExportMessages = {
  readonly title: string; readonly description: string; readonly setupTitle: string; readonly setupDescription: string; readonly authorize: string; readonly authorized: (label: string) => string; readonly directoryToken: string; readonly createDirectory: string; readonly childName: string; readonly childPlaceholder: string; readonly create: string; readonly selectionTitle: string; readonly articleIds: string; readonly articleIdsHint: string; readonly format: string; readonly subdirectory: string; readonly subdirectoryHint: string; readonly options: string; readonly namingTemplate: string; readonly maximumNameBytes: string; readonly collision: string; readonly collisionFail: string; readonly collisionSkip: string; readonly collisionReplace: string; readonly collisionSuffix: string; readonly includeContent: string; readonly includeMetadata: string; readonly includeComments: string; readonly htmlOptions: string; readonly resourcePolicy: string; readonly resourceBestEffort: string; readonly resourceStrict: string; readonly batchArchive: string; readonly batchArchiveHint: string; readonly confirmation: string; readonly confirmationHint: string; readonly start: string; readonly queued: (jobId: string) => string; readonly queuedHint: string; readonly invalidSelection: string; readonly invalidDirectory: string; readonly actionFailed: string; readonly recordsTitle: string; readonly recordsDescription: string; readonly loading: string; readonly unavailable: string; readonly empty: string; readonly retry: string; readonly previous: string; readonly next: string; readonly page: (current: number, total: number) => string; readonly pagination: string; readonly selected: string; readonly selectAll: string; readonly selectRow: (id: string) => string; readonly visibleColumns: string; readonly columns: { readonly id: string; readonly format: string; readonly state: string; readonly created: string; readonly provenance: string }; readonly detailTitle: string; readonly detailDescription: string; readonly selectOne: string; readonly loadManifest: string; readonly verify: string; readonly verifyConfirmation: (id: string) => string; readonly manifestLoading: string; readonly manifestUnavailable: string; readonly manifestSummary: (count: number) => string; readonly files: string; readonly noFiles: string; readonly fileColumns: { readonly path: string; readonly size: string; readonly status: string; readonly checksum: string; readonly download: string }; readonly downloadArtifact: string; readonly verificationTitle: string; readonly verificationValid: (count: number) => string; readonly verificationInvalid: (count: number) => string; readonly verificationIssues: string; readonly artifactTitle: string; readonly artifactDescription: string; readonly openAction: string; readonly openConfirmationLabel: string; readonly openConfirmation: (id: string) => string; readonly openConfirmationHint: string; readonly openConfirmationInput: string; readonly outputOpened: string
}

type SettingsMessages = {
  readonly title: string; readonly description: string; readonly loading: string; readonly unavailable: string; readonly retry: string; readonly actionFailed: string
  readonly credentials: { readonly title: string; readonly description: string; readonly empty: string; readonly import: string; readonly remove: string; readonly nickname: string; readonly cookie: string; readonly optional: string; readonly imported: string; readonly columns: { readonly account: string; readonly kind: string; readonly status: string; readonly updated: string } }
  readonly proxies: { readonly title: string; readonly description: string; readonly empty: string; readonly add: string; readonly remove: string; readonly enable: string; readonly disable: string; readonly test: string; readonly name: string; readonly endpoint: string; readonly authorization: string; readonly trust: string; readonly publicOnly: string; readonly credentialTrusted: string; readonly priority: string; readonly classes: string; readonly disclosure: string; readonly disclosureRequired: string; readonly confirmation: string; readonly confirmationHint: string; readonly health: string; readonly probe: string; readonly enabled: string; readonly disabled: string; readonly columns: { readonly name: string; readonly endpoint: string; readonly trust: string; readonly priority: string; readonly health: string; readonly state: string; readonly actions: string } }
  readonly preferences: { readonly title: string; readonly description: string; readonly save: string; readonly saved: string; readonly downloadConcurrency: string; readonly forceContent: string; readonly metadataOverrides: string; readonly directFirst: string; readonly fallbackEnabled: string; readonly language: string; readonly namingTemplate: string; readonly maximumNameBytes: string; readonly collisionPolicy: string }
  readonly backups: { readonly title: string; readonly description: string; readonly create: string; readonly created: string; readonly verify: string; readonly backupId: string; readonly valid: string; readonly invalid: string; readonly restoreTitle: string; readonly restoreDescription: string; readonly archive: string; readonly policy: string; readonly refuse: string; readonly rename: string; readonly stage: string; readonly staging: string; readonly confirmation: string; readonly confirmationHint: string; readonly commit: string; readonly destructiveWarning: string; readonly terminalTitle: string; readonly terminalMessage: string }
  readonly integrity: { readonly title: string; readonly description: string; readonly checked: string; readonly issues: string; readonly noIssues: string; readonly columns: { readonly kind: string; readonly message: string; readonly repairable: string; readonly recommendation: string } }
  readonly gc: { readonly title: string; readonly description: string; readonly plan: string; readonly apply: string; readonly planned: string; readonly planExpired: string; readonly generated: string; readonly expires: string; readonly confirmation: string; readonly totals: string; readonly result: string; readonly categories: { readonly objects: string; readonly temporary: string; readonly debug: string; readonly logs: string } }
  readonly diagnostics: { readonly title: string; readonly description: string; readonly collected: string; readonly empty: string; readonly columns: { readonly check: string; readonly status: string; readonly summary: string } }
  readonly common: { readonly yes: string; readonly no: string; readonly bytes: (count: number) => string; readonly countBytes: (count: number, bytes: number) => string }
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
  readonly detail: { readonly title: string; readonly description: string; readonly refresh: string; readonly refreshing: string; readonly loading: string; readonly unavailable: string; readonly items: string; readonly itemsLimited: (shown: number, total: number) => string; readonly noItems: string; readonly logs: string; readonly noLogs: string; readonly lease: string; readonly leaseActive: string; readonly leaseInactive: string; readonly expires: string; readonly attempts: string; readonly errorClass: string; readonly refreshed: string }
}

type SavedQueryResourceMessages = ResourceMessages & {
  readonly actions: { readonly title: string; readonly description: string; readonly name: string; readonly query: string; readonly create: string; readonly edit: string; readonly remove: string; readonly selectOne: string; readonly invalidQuery: string; readonly deleteConfirm: (name: string) => string; readonly saved: (name: string) => string; readonly editing: (name: string) => string; readonly deleted: (name: string) => string; readonly actionFailed: string }
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
