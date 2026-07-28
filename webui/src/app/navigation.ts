import { navigationGuard } from '../lib/navigationGuard'

export const navigationEvent = 'wechat-article:navigate'

const historyIndexKey = 'wechatArticleNavigationIndex'
let currentHistoryIndex = 0
let navigationHistoryInitialized = false
let restoringHistory = false
let acceptedHistoryIndex: number | undefined

export const navigationGroups = ['home', 'content', 'tasks', 'settings'] as const

export const navigationItems = [
  { group: 'home', href: '/', key: 'overview' },
  { group: 'content', href: '/accounts', key: 'accounts' },
  { group: 'content', href: '/articles', key: 'articles' },
  { group: 'content', href: '/albums', key: 'albums' },
  { group: 'tasks', href: '/jobs', key: 'jobs' },
  { group: 'tasks', href: '/exports', key: 'exports' },
  { group: 'settings', href: '/settings', key: 'settings' }
] as const

export type NavigationGroup = typeof navigationGroups[number]
export type NavigationItem = typeof navigationItems[number]

export function navigateTo(href: string) {
  const target = parseClientNavigationURL(href)
  if (!target || currentLocation() === clientLocation(target)) return

  navigationGuard.request(() => {
    initializeNavigationHistory()
    currentHistoryIndex += 1
    window.history.pushState(withHistoryIndex(window.history.state, currentHistoryIndex), '', clientLocation(target))
    window.dispatchEvent(new Event(navigationEvent))
  })
}

export function replaceLocation(nextLocation: string) {
  if (nextLocation === currentLocation()) return
  initializeNavigationHistory()
  window.history.replaceState(
    withHistoryIndex(window.history.state, currentHistoryIndex),
    '',
    nextLocation,
  )
  window.dispatchEvent(new Event(navigationEvent))
}

export function listenForNavigation(onNavigate: () => void) {
  initializeNavigationHistory()
  const handleNavigation = () => onNavigate()
  const handlePopState = (event: PopStateEvent) => {
    const targetIndex = readHistoryIndex(event.state)
    if (targetIndex === undefined) {
      onNavigate()
      return
    }

    if (restoringHistory) {
      restoringHistory = false
      currentHistoryIndex = targetIndex
      return
    }

    if (acceptedHistoryIndex === targetIndex) {
      acceptedHistoryIndex = undefined
      currentHistoryIndex = targetIndex
      onNavigate()
      return
    }

    const delta = targetIndex - currentHistoryIndex
    let targetWasRestored = false
    const acceptNavigation = () => {
      if (!targetWasRestored) {
        currentHistoryIndex = targetIndex
        onNavigate()
        return
      }
      acceptedHistoryIndex = targetIndex
      window.history.go(delta)
    }

    if (navigationGuard.request(acceptNavigation)) return

    targetWasRestored = true
    restoringHistory = true
    window.history.go(-delta)
  }

  window.addEventListener('popstate', handlePopState)
  window.addEventListener(navigationEvent, handleNavigation)
  return () => {
    window.removeEventListener('popstate', handlePopState)
    window.removeEventListener(navigationEvent, handleNavigation)
  }
}

export function getClientNavigationHref(anchor: HTMLAnchorElement, event: MouseEvent): string | undefined {
  if (
    event.defaultPrevented
    || event.button !== 0
    || event.metaKey
    || event.ctrlKey
    || event.shiftKey
    || event.altKey
    || (anchor.target && anchor.target !== '_self')
    || anchor.hasAttribute('download')
  ) {
    return
  }

  const target = parseClientNavigationURL(anchor.href)
  if (!target || !isSPANavigable(target.pathname)) return
  if (target.pathname === window.location.pathname && target.search === window.location.search) return
  return clientLocation(target)
}

function initializeNavigationHistory() {
  if (navigationHistoryInitialized) return
  const existingIndex = readHistoryIndex(window.history.state)
  currentHistoryIndex = existingIndex ?? 0
  if (existingIndex === undefined) {
    window.history.replaceState(withHistoryIndex(window.history.state, currentHistoryIndex), '', currentLocation())
  }
  navigationHistoryInitialized = true
}

function parseClientNavigationURL(href: string): URL | undefined {
  if (!href || href.startsWith('//')) return
  let target: URL
  try {
    target = new URL(href, window.location.href)
  } catch {
    return
  }
  if (target.origin !== window.location.origin) return
  return target
}

function isSPANavigable(pathname: string): boolean {
  if (pathname === '/api' || pathname.startsWith('/api/')) return false
  if (pathname === '/assets' || pathname.startsWith('/assets/')) return false
  return true
}

function readHistoryIndex(state: unknown): number | undefined {
  if (!state || typeof state !== 'object') return
  const index = (state as Record<string, unknown>)[historyIndexKey]
  return typeof index === 'number' ? index : undefined
}

function withHistoryIndex(state: unknown, index: number) {
  const existing = state && typeof state === 'object' ? state : {}
  return { ...existing, [historyIndexKey]: index }
}

function currentLocation() {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`
}

function clientLocation(url: URL) {
  return `${url.pathname}${url.search}${url.hash}`
}
