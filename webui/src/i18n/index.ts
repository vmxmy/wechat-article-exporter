import { useCallback, useEffect, useRef, useState } from 'react'
import { getPreferences } from '../lib/api'
import { en } from './messages.en'
import { zhCN } from './messages.zh-CN'

export type Locale = 'en' | 'zh-CN'

export const messages = { en, 'zh-CN': zhCN } as const
const localeStorageKey = 'wechat-article.display.language'
const localeChangeEvent = 'wechat-article:locale-change'

export function isLocale(value: unknown): value is Locale {
  return value === 'en' || value === 'zh-CN'
}

export function persistLocale(locale: Locale) {
  try {
    window.localStorage.setItem(localeStorageKey, locale)
  } catch {
    // A privacy-restricted browser can still use the selected in-memory locale.
  }
  window.dispatchEvent(new CustomEvent<Locale>(localeChangeEvent, { detail: locale }))
}

export type MessageCatalog = {
  readonly product: { readonly name: string; readonly local: string; readonly privacy: string; readonly localOnly: string }
  readonly navigation: Record<'home' | 'content' | 'work' | 'system' | 'tasks' | 'workspace' | 'library' | 'operations' | 'overview' | 'login' | 'import' | 'importAction' | 'accounts' | 'articles' | 'albums' | 'savedQueries' | 'jobs' | 'exports' | 'settings', string>
  readonly a11y: { readonly skip: string; readonly navigation: string; readonly openNavigation: string; readonly closeNavigation: string; readonly closeDialog: string; readonly currentPage: (page: string) => string; readonly copied: string; readonly copyUnavailable: string }
  readonly selectors: { readonly clear: (label: string) => string; readonly search: (label: string) => string; readonly noResults: string; readonly loading: string; readonly unavailable: string; readonly retry: string; readonly selectAll: string; readonly selected: (count: number) => string; readonly duplicate: (position: number, total: number) => string }
  readonly localeSwitch: string
  readonly connection: { readonly connected: string; readonly unavailable: string; readonly checking: string }
  readonly notFound: { readonly title: string; readonly description: string; readonly home: string }
  readonly documentTitle: { readonly appName: string; readonly notFound: string; readonly page: (segment: string) => string }
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
    readonly primaryActionTitle: string
    readonly secondaryActionsTitle: string
    readonly quickEntries: { readonly browseArticles: string; readonly startExport: string; readonly manageAccounts: string; readonly reviewTasks: string }
    readonly recentJobsTitle: string
    readonly recentJobsEmpty: string
    readonly actions: {
      readonly signInTitle: string
      readonly signInDescription: string
      readonly signIn: string
      readonly addAccountTitle: string
      readonly addAccountDescription: string
      readonly addAccount: string
      readonly syncTitle: string
      readonly syncDescription: string
      readonly sync: string
      readonly browseTitle: string
      readonly browseDescription: string
      readonly browse: string
      readonly export: string
      readonly failedJobsTitle: (count: number) => string
      readonly failedJobsDescription: string
      readonly failedJobs: string
    }
  }
  readonly unavailableActions: { readonly confirmationTitle: string; readonly confirmationDescription: string; readonly apiUnavailable: string }
  readonly login: { readonly title: string; readonly description: string; readonly legacyDescription: string; readonly manageGlobally: string; readonly sessionTitle: string; readonly sessionMenu: string; readonly signedOut: string; readonly accountUnavailable: string; readonly manageSession: string; readonly account: string; readonly state: string; readonly unknownState: string; readonly checking: string; readonly unavailable: string; readonly qrTitle: string; readonly qrDescription: string; readonly start: string; readonly poll: string; readonly complete: string; readonly logout: string; readonly logoutComplete: string; readonly logoutUnavailable: string; readonly logoutConfirmTitle: string; readonly logoutConfirmDescription: string; readonly logoutConfirmCancel: string; readonly retry: string; readonly switchTitle: string; readonly switchAccount: string; readonly switchChecking: string; readonly switchEmpty: string; readonly switchUnavailable: string; readonly switching: string; readonly switchComplete: (name: string) => string; readonly states: Readonly<Record<string, string>> }
  readonly import: { readonly title: string; readonly description: string; readonly url: string; readonly placeholder: string; readonly submit: string; readonly force: string; readonly queued: (reference: string) => string; readonly failed: string; readonly technicalDetails: string; readonly jobID: string; readonly copyJobID: string; readonly note: string }
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
    readonly filters: { readonly title: string; readonly advanced: string; readonly advancedHint: string; readonly invalid: string; readonly apply: string; readonly reset: string; readonly accountId: string; readonly albumId: string; readonly author: string; readonly state: string; readonly messageTypes: string; readonly hasContent: string; readonly hasComments: string; readonly deleted: string; readonly original: string; readonly paid: string; readonly any: string; readonly yes: string; readonly no: string; readonly publishedFrom: string; readonly publishedTo: string; readonly readMin: string; readonly readMax: string; readonly oldLikeMin: string; readonly oldLikeMax: string; readonly shareMin: string; readonly shareMax: string; readonly likeMin: string; readonly likeMax: string; readonly commentMin: string; readonly commentMax: string; readonly weCoinMin: string; readonly weCoinMax: string; readonly mediaSecondsMin: string; readonly mediaSecondsMax: string }
    readonly ux: {
      readonly moreFilters: string
      readonly appliedFilters: string
      readonly clearFilters: string
      readonly removeFilter: (label: string) => string
      readonly savedViews: string
      readonly savedViewsToolbar: string
      readonly savedViewsPlaceholder: string
      readonly saveView: string
      readonly openDetails: (title: string) => string
      readonly details: string
      readonly closeDetails: string
      readonly selectedCount: (count: number) => string
      readonly selectedCountWithLimit: (count: number, limit: number) => string
      readonly selectionLimit: (limit: number, current: number) => string
      readonly selectionActions: string
      readonly moreActions: string
      readonly openOriginal: string
      readonly firstUseTitle: string
      readonly firstUseDescription: string
      readonly firstUseAction: string
      readonly filteredEmptyTitle: string
      readonly filteredEmptyDescription: string
      readonly filteredEmptyAction: string
      readonly accountUnavailable: string
      readonly accountNameUnavailable: string
      readonly albumUnavailable: string
      readonly accountDescription: string
      readonly albumDescription: string
      readonly selectorNoResults: string
      readonly duplicateSelection: (position: number, total: number) => string
      readonly dateFrom: string
      readonly dateTo: string
      readonly messageTypePlaceholder: string
      readonly messageTypes: Readonly<Record<string, string>>
      readonly moreFilterDescription: string
      readonly technicalDetails: string
      readonly copyArticleID: string
      readonly metrics: { readonly reads: string; readonly oldLikes: string; readonly likes: string; readonly shares: string; readonly comments: string; readonly captured: string }
      readonly savedQuery: { readonly visualEditor: string; readonly visualDescription: string; readonly technicalMode: string; readonly technicalDescription: string; readonly rawJSON: string; readonly applyTechnical: string; readonly invalidTechnical: string; readonly savedSummary: string; readonly editingVisual: string }
    }
    readonly actions: { readonly title: string; readonly description: string; readonly preview: string; readonly previewUnavailable: string; readonly previewBlocked: string; readonly download: string; readonly metadata: string; readonly comments: string; readonly resources: string; readonly forceResources: string; readonly resourcesSummaryTitle: string; readonly resourcesSummary: (total: number, available: number, missing: number) => string; readonly resourcesComplete: string; readonly resourcesLoading: string; readonly resourcesUnavailable: string; readonly detailTitle: string; readonly detailLoading: string; readonly detailUnavailable: string; readonly metricsTitle: string; readonly metricsSummary: (reads: number, oldLikes: number, likes: number, shares: number, comments: number, capturedAt: string) => string; readonly metricsUnavailable: string; readonly resourceDetailsTitle: string; readonly resourceDetail: (role: string, ordinal: number, state: string) => string; readonly resourceAvailable: string; readonly resourceMissing: string; readonly resourceDetailsEmpty: string; readonly resourceDetailsLimited: (shown: number, total: number) => string; readonly commentsTitle: string; readonly commentsLoading: string; readonly commentsUnavailable: string; readonly commentsEmpty: string; readonly commentsPartial: (count: number) => string; readonly commentsPagination: string; readonly commentsPage: (current: number, total: number) => string; readonly expandReplies: (count: number) => string; readonly repliesPending: string; readonly repliesLoading: string; readonly repliesUnavailable: string; readonly repliesEmpty: string; readonly repliesPagination: string; readonly unknownAuthor: string; readonly commentStats: (likes: number, replies: number) => string; readonly replyLikes: (likes: number) => string; readonly failed: string; readonly saveQuery: string; readonly exportSelected: string; readonly exportMatching: string }
  }
  readonly resources: {
    readonly accounts: AccountResourceMessages
    readonly albums: ResourceMessages & { readonly filters: { readonly title: string; readonly description: string; readonly accountId: string; readonly keyword: string; readonly appliedFilters: string; readonly clearFilters: string; readonly removeFilter: (label: string) => string }; readonly emptyState: { readonly firstUseTitle: string; readonly firstUseDescription: string; readonly firstUseAction: string; readonly filteredEmptyTitle: string; readonly filteredEmptyDescription: string; readonly filteredEmptyAction: string }; readonly actions: { readonly title: string; readonly description: string; readonly order: string; readonly forward: string; readonly reverse: string; readonly traverse: string; readonly download: string; readonly export: string; readonly selectOne: string; readonly selectAtLeastOne: string; readonly selectedCountWithLimit: (count: number, limit: number) => string; readonly selectionLimit: (limit: number, current: number) => string; readonly queued: string; readonly failed: string } }
    readonly jobs: JobResourceMessages
    readonly savedQueries: SavedQueryResourceMessages
  }
}

type ExportMessages = {
  readonly title: string; readonly description: string; readonly setupTitle: string; readonly setupDescription: string; readonly authorize: string; readonly authorized: (label: string) => string; readonly directoryToken: string; readonly createDirectory: string; readonly childName: string; readonly childPlaceholder: string; readonly create: string; readonly selectionTitle: string; readonly selection: { readonly title: string; readonly accountId: string; readonly albumId: string; readonly albumIds: string; readonly albumIdsHint: string; readonly account: string; readonly album: string; readonly savedQuery: string; readonly savedQueryPlaceholder: string; readonly matchingQuery: string; readonly matching: string; readonly active: (label: string) => string; readonly explicit: (count: number) => string; readonly accountLabel: (id: string) => string; readonly albumLabel: (id: string) => string; readonly albumsLabel: (count: number) => string; readonly savedQueryLabel: (name: string) => string; readonly matchingLabel: string }; readonly workflow: { readonly stages: string; readonly scope: string; readonly scopeDescription: string; readonly scopeType: string; readonly selectedArticles: string; readonly selectedArticlesDescription: string; readonly selectedArticlesEmpty: string; readonly oneAccount: string; readonly oneAccountDescription: string; readonly oneAlbum: string; readonly oneAlbumDescription: string; readonly savedQuery: string; readonly savedQueryDescription: string; readonly matching: string; readonly matchingDescription: string; readonly chooseAccount: string; readonly chooseAlbum: string; readonly useCurrentResults: string; readonly format: string; readonly formatDescription: string; readonly destination: string; readonly destinationDescription: string; readonly authorizeDefaultDescription: string; readonly destinationReady: string; readonly optionalDestination: string; readonly continueToFormat: string; readonly continueToDestination: string; readonly back: string; readonly currentAction: string; readonly technicalDetails: string; readonly copyValue: string; readonly savedAccountFallback: string; readonly savedAlbumFallback: string; readonly selectedArticlesLabel: (count: number) => string; readonly matchingSummary: string; readonly noMatchingFilters: string; readonly recordLabel: (format: string) => string; readonly queued: (jobLabel: string, reference: string) => string; readonly jobLabel: string; readonly jobID: string; readonly exportID: string; readonly provenanceGeneration: string }; readonly articleIds: string; readonly articleIdsHint: string; readonly format: string; readonly subdirectory: string; readonly subdirectoryHint: string; readonly options: string; readonly formatOptions: (format: string) => string; readonly namingTemplate: string; readonly maximumNameBytes: string; readonly collision: string; readonly collisionFail: string; readonly collisionSkip: string; readonly collisionReplace: string; readonly collisionSuffix: string; readonly includeContent: string; readonly includeMetadata: string; readonly includeComments: string; readonly htmlOptions: string; readonly resourcePolicy: string; readonly resourceBestEffort: string; readonly resourceStrict: string; readonly batchArchive: string; readonly batchArchiveHint: string; readonly confirmation: string; readonly confirmationHint: string; readonly start: string; readonly queued: (jobId: string) => string; readonly queuedHint: string; readonly invalidSelection: string; readonly invalidDirectory: string; readonly actionFailed: string; readonly recordsTitle: string; readonly recordsDescription: string; readonly loading: string; readonly unavailable: string; readonly empty: string; readonly retry: string; readonly previous: string; readonly next: string; readonly page: (current: number, total: number) => string; readonly pagination: string; readonly selected: string; readonly selectAll: string; readonly selectRow: (id: string) => string; readonly visibleColumns: string; readonly visibleFileColumns: string; readonly columns: { readonly id: string; readonly format: string; readonly state: string; readonly created: string; readonly provenance: string }; readonly detailTitle: string; readonly detailDescription: string; readonly selectOne: string; readonly loadManifest: string; readonly verify: string; readonly verifyConfirmation: (id: string) => string; readonly manifestLoading: string; readonly manifestUnavailable: string; readonly manifestSummary: (count: number) => string; readonly files: string; readonly noFiles: string; readonly fileColumns: { readonly path: string; readonly size: string; readonly status: string; readonly checksum: string; readonly download: string }; readonly downloadArtifact: string; readonly verificationTitle: string; readonly verificationValid: (count: number) => string; readonly verificationInvalid: (count: number) => string; readonly verificationIssues: string; readonly verificationIssue: (position: number) => string; readonly artifactTitle: string; readonly artifactDescription: string; readonly openAction: string; readonly openConfirmationLabel: string; readonly openConfirmation: (id: string) => string; readonly openConfirmationHint: string; readonly openConfirmationInput: string; readonly outputOpened: string
}

type SettingsMessages = {
  readonly title: string; readonly description: string; readonly loading: string; readonly unavailable: string; readonly retry: string; readonly actionFailed: string
  readonly navigation: { readonly label: string; readonly general: string; readonly downloadExport: string; readonly credentials: string; readonly network: string; readonly storage: string; readonly diagnostics: string; readonly danger: string }
  readonly unsaved: { readonly title: string; readonly description: string; readonly stay: string; readonly discard: string }
  readonly credentials: { readonly title: string; readonly description: string; readonly importTitle: string; readonly listTitle: string; readonly accountUnavailable: string; readonly technicalDetails: string; readonly accountId: string; readonly copyAccountId: string; readonly empty: string; readonly import: string; readonly validate: string; readonly validating: string; readonly validationPassed: string; readonly validationFailed: string; readonly validationHint: string; readonly remove: string; readonly removed: string; readonly removeConfirmation: (id: string) => string; readonly removeConfirmationLabel: string; readonly removeConfirmationHint: string; readonly confirmRemove: string; readonly cancelRemove: string; readonly nickname: string; readonly biz: string; readonly uin: string; readonly key: string; readonly passTicket: string; readonly wapSid2: string; readonly appMsgToken: string; readonly cookie: string; readonly optional: string; readonly imported: string; readonly file: string; readonly fileHint: string; readonly fileImported: string; readonly visibleColumns: string; readonly columns: { readonly account: string; readonly kind: string; readonly status: string; readonly updated: string } }
  readonly proxies: { readonly title: string; readonly description: string; readonly addTitle: string; readonly listTitle: string; readonly endpointPlaceholder: string; readonly empty: string; readonly add: string; readonly added: string; readonly remove: string; readonly removed: string; readonly removeConfirmation: (id: string) => string; readonly removeConfirmationLabel: string; readonly removeConfirmationHint: string; readonly confirmRemove: string; readonly cancelRemove: string; readonly enable: string; readonly enabledNotice: string; readonly disable: string; readonly disabledNotice: string; readonly test: string; readonly tested: string; readonly name: string; readonly endpoint: string; readonly authorization: string; readonly trust: string; readonly publicOnly: string; readonly credentialTrusted: string; readonly publicOnlyExplanation: string; readonly credentialTrustedExplanation: string; readonly priority: string; readonly classes: string; readonly disclosure: string; readonly disclosureRequired: string; readonly confirmation: string; readonly confirmationHint: string; readonly health: string; readonly probe: string; readonly enabled: string; readonly disabled: string; readonly visibleColumns: string; readonly columns: { readonly name: string; readonly endpoint: string; readonly trust: string; readonly priority: string; readonly health: string; readonly state: string; readonly actions: string } }
  readonly preferences: { readonly title: string; readonly description: string; readonly downloadDefaults: string; readonly exportDefaults: string; readonly save: string; readonly saved: string; readonly downloadConcurrency: string; readonly forceContent: string; readonly metadataOverrides: string; readonly directFirst: string; readonly fallbackEnabled: string; readonly language: string; readonly languageEnglish: string; readonly languageChinese: string; readonly namingTemplate: string; readonly maximumNameBytes: string; readonly collisionPolicy: string; readonly collisionFail: string; readonly collisionSkip: string; readonly collisionReplace: string; readonly collisionSuffix: string; readonly excelIncludeContent: string; readonly jsonIncludeContent: string; readonly jsonIncludeComments: string; readonly htmlIncludeComments: string; readonly exportDefaultsHint: string }
  readonly storage: { readonly description: string; readonly dangerEyebrow: string; readonly dangerTitle: string; readonly dangerDescription: string }
  readonly backups: { readonly title: string; readonly description: string; readonly create: string; readonly created: string; readonly download: string; readonly verify: string; readonly backupId: string; readonly valid: string; readonly invalid: string; readonly summaryCreated: string; readonly summarySize: string; readonly summaryObjects: string; readonly technicalDetails: string; readonly checksum: string; readonly copyChecksum: string; readonly restoreTitle: string; readonly restoreDescription: string; readonly archive: string; readonly policy: string; readonly refuse: string; readonly rename: string; readonly stage: string; readonly staging: string; readonly confirmation: string; readonly confirmationHint: string; readonly commit: string; readonly destructiveWarning: string; readonly terminalTitle: string; readonly terminalMessage: string }
  readonly integrity: { readonly title: string; readonly description: string; readonly checked: string; readonly issues: string; readonly noIssues: string; readonly visibleColumns: string; readonly columns: { readonly kind: string; readonly message: string; readonly repairable: string; readonly recommendation: string } }
  readonly gc: { readonly title: string; readonly description: string; readonly plan: string; readonly apply: string; readonly planned: string; readonly planExpired: string; readonly generated: string; readonly expires: string; readonly confirmation: string; readonly totals: string; readonly quantity: (count: string, bytes: string) => string; readonly result: string; readonly categories: { readonly objects: string; readonly temporary: string; readonly debug: string; readonly logs: string } }
  readonly diagnostics: { readonly title: string; readonly description: string; readonly collected: string; readonly empty: string; readonly visibleColumns: string; readonly createBundle: string; readonly creatingBundle: string; readonly bundleReady: string; readonly bundleDescription: string; readonly summaryCreated: string; readonly summarySize: string; readonly technicalDetails: string; readonly checksum: string; readonly copyChecksum: string; readonly downloadBundle: string; readonly bundleExpires: string; readonly bundleChecksum: string; readonly columns: { readonly check: string; readonly status: string; readonly summary: string } }
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
  readonly rangeSummary: (start: number, end: number, total: number | undefined) => string
  readonly updating: string
  readonly selected: string
  readonly selectAll: string
  readonly selectRow: (row: string) => string
  readonly visibleColumns: string
  readonly columns: Readonly<Record<string, string>>
}

type AccountResourceMessages = ResourceMessages & {
  readonly filters: { readonly title: string; readonly description: string; readonly keyword: string; readonly appliedFilters: string; readonly clearFilters: string; readonly removeFilter: (label: string) => string }
  readonly emptyState: { readonly title: string; readonly description: string; readonly addAccount: string; readonly importManifest: string; readonly filteredEmptyTitle: string; readonly filteredEmptyDescription: string; readonly filteredEmptyAction: string }
  readonly actions: { readonly createTitle: string; readonly editTitle: string; readonly createDescription: string; readonly editDescription: string; readonly toolbarLabel: string; readonly addAccount: string; readonly discoveryTitle: string; readonly discoveryDescription: string; readonly search: string; readonly searchPlaceholder: string; readonly discoveryRequiresSession: string; readonly discoverySignIn: string; readonly discoveryCheckingSession: string; readonly discoveryFailedTitle: string; readonly discoveryFailedAuth: string; readonly discoveryFailedGeneric: string; readonly discoveryRetry: string; readonly discoveryReAuth: string; readonly clearCandidate: string; readonly cancel: string; readonly fakeid: string; readonly fakeidHint: string; readonly technicalDetails: string; readonly name: string; readonly alias: string; readonly discover: string; readonly discoveryResults: string; readonly discoveryEmpty: string; readonly articleLinkTitle: string; readonly articleLinkDescription: string; readonly articleLinkLabel: string; readonly articleLinkPlaceholder: string; readonly articleLinkResolve: string; readonly articleLinkResolved: (name: string) => string; readonly articleLinkFailed: string; readonly useCandidate: string; readonly candidateSelected: (name: string) => string; readonly add: string; readonly saved: (name: string) => string; readonly edit: string; readonly remove: string; readonly sync: string; readonly syncMode: string; readonly incremental: string; readonly full: string; readonly incrementalHint: string; readonly fullHint: string; readonly downloadManifest: string; readonly importManifest: string; readonly manifestHint: string; readonly manifestImported: (added: number, merged: number, unchanged: number) => string; readonly manifestFailed: string; readonly selectOne: string; readonly deleteTitle: string; readonly deleteConfirm: string; readonly deleteConfirmation: (ids: readonly string[]) => string; readonly deleteConfirmationLabel: string; readonly deleteConfirmationHint: string; readonly confirmDelete: string; readonly cancelDelete: string; readonly actionFailed: string; readonly more: string; readonly selectionActions: string }
}

type JobResourceMessages = ResourceMessages & {
  readonly filterTabs: { readonly all: string; readonly active: string; readonly done: string; readonly attention: string; readonly count: (count: number) => string }
  readonly filterToolbarLabel: string
  readonly actions: { readonly title: string; readonly description: string; readonly start: string; readonly pause: string; readonly resume: string; readonly retry: string; readonly cancel: string; readonly selectOne: string; readonly pauseTitle: string; readonly retryTitle: string; readonly cancelTitle: string; readonly confirmPause: string; readonly confirmRetry: string; readonly confirmCancel: string; readonly pauseConfirmation: (id: string) => string; readonly retryConfirmation: (id: string) => string; readonly cancelConfirmationProof: (id: string) => string; readonly confirmationLabel: string; readonly confirmationHint: string; readonly cancelConfirmation: string; readonly actionFailed: string }
  readonly detail: { readonly title: string; readonly description: string; readonly refresh: string; readonly refreshing: string; readonly loading: string; readonly unavailable: string; readonly items: string; readonly itemsLimited: (shown: number, total: number) => string; readonly noItems: string; readonly logs: string; readonly noLogs: string; readonly lease: string; readonly leaseActive: string; readonly leaseInactive: string; readonly expires: string; readonly attempts: string; readonly errorClass: string; readonly refreshed: string; readonly attention: string; readonly reason: string; readonly impact: string; readonly nextAction: string; readonly retryAction: string; readonly refreshAction: string; readonly networkReason: string; readonly unknownReason: string; readonly itemNotCompleted: string; readonly technicalDetails: string; readonly jobID: string; readonly profile: string; readonly copyID: string; readonly copyValue: string }
}

type SavedQueryResourceMessages = ResourceMessages & {
  readonly actions: { readonly title: string; readonly description: string; readonly name: string; readonly query: string; readonly create: string; readonly edit: string; readonly remove: string; readonly selectOne: string; readonly invalidQuery: string; readonly deleteTitle: string; readonly deleteConfirm: (name: string) => string; readonly deleteConfirmation: (name: string) => string; readonly deleteConfirmationLabel: string; readonly deleteConfirmationHint: string; readonly confirmDelete: string; readonly cancelDelete: string; readonly saved: (name: string) => string; readonly editing: (name: string) => string; readonly deleted: (name: string) => string; readonly actionFailed: string }
}

export function useMessages(locale: Locale): MessageCatalog {
  return messages[locale]
}

export function useLocale(): readonly [Locale, (locale: Locale) => void] {
  const [locale, setLocaleState] = useState<Locale>(readInitialLocale)
  const localeGeneration = useRef(0)
  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])
  useEffect(() => {
    const controller = new AbortController()
    const requestGeneration = localeGeneration.current
    void getPreferences(controller.signal).then((preferences) => {
      if (requestGeneration === localeGeneration.current && isLocale(preferences.display.language)) {
        persistLocale(preferences.display.language)
      }
    }).catch(() => {
      // The local profile is unavailable; retain the persisted or browser locale.
    })
    return () => controller.abort()
  }, [])
  useEffect(() => {
    const updateLocale = (event: Event) => {
      const nextLocale = (event as CustomEvent<Locale>).detail
      if (isLocale(nextLocale)) {
        localeGeneration.current += 1
        setLocaleState(nextLocale)
      }
    }
    window.addEventListener(localeChangeEvent, updateLocale)
    return () => window.removeEventListener(localeChangeEvent, updateLocale)
  }, [])
  const setLocale = useCallback((nextLocale: Locale) => {
    persistLocale(nextLocale)
  }, [])

  return [locale, setLocale]
}

function readInitialLocale(): Locale {
  let persisted: string | null = null
  try {
    persisted = window.localStorage.getItem(localeStorageKey)
  } catch {
    // Fall through to the browser preference when persistence is unavailable.
  }
  if (isLocale(persisted)) {
    return persisted
  }

  return navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}
